package archive

import (
	"archive/tar"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/manifest"
)

// peekSize is how much of a stream is buffered before identifying it. It only
// has to cover the longest registered magic, with room to spare.
const peekSize = 512

// Reader streams an archive back. The cipher and the compressor are identified
// from the bytes themselves, never from the file name, so a renamed archive
// still restores and the user only needs the key.
type Reader struct {
	cipherName string
	compName   string
	manifest   *manifest.Manifest

	tr      *tar.Reader
	closers []io.Closer
}

// Open reads an archive from disk.
func Open(path string, k *codec.Keyring) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening archive: %w", err)
	}
	r, err := NewReader(f, k)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.closers = append(r.closers, f)
	return r, nil
}

// NewReader reads an archive from any stream. Nothing here seeks, so a pipe
// works as well as a file.
func NewReader(src io.Reader, k *codec.Keyring) (*Reader, error) {
	r := &Reader{}

	buffered := bufio.NewReaderSize(src, peekSize)
	cipher, err := detectCipher(buffered)
	if err != nil {
		return nil, err
	}
	r.cipherName = cipher.Name()

	plain, err := cipher.Open(buffered, k)
	if err != nil {
		return nil, fmt.Errorf("decrypting with %s: %w", cipher.Name(), err)
	}
	r.closers = append(r.closers, plain)

	bufferedPlain := bufio.NewReaderSize(plain, peekSize)
	comp, err := detectCompressor(bufferedPlain)
	if err != nil {
		r.Close()
		return nil, err
	}
	r.compName = comp.Name()

	decomp, err := comp.NewReader(bufferedPlain)
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("decompressing with %s: %w", comp.Name(), err)
	}
	r.closers = append(r.closers, decomp)

	r.tr = tar.NewReader(decomp)
	if err := r.readManifest(); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

func detectCipher(br *bufio.Reader) (codec.Cipher, error) {
	head, err := peek(br)
	if err != nil {
		return nil, err
	}
	cipher, ok := codec.DetectCipher(head)
	if !ok {
		return nil, fmt.Errorf(
			"this does not look like an arca archive: no known encryption format at the start of the stream "+
				"(expected one of: %s)", strings.Join(codec.CipherNames(), ", "))
	}
	return cipher, nil
}

func detectCompressor(br *bufio.Reader) (codec.Compressor, error) {
	head, err := peek(br)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted stream: %w", err)
	}
	comp, ok := codec.DetectCompressor(head)
	if !ok {
		return nil, fmt.Errorf(
			"decrypted stream is not in a known compression format (expected one of: %s)",
			strings.Join(codec.CompressorNames(), ", "))
	}
	return comp, nil
}

func peek(br *bufio.Reader) ([]byte, error) {
	head, err := br.Peek(peekSize)
	// A short stream is not itself an error: detection only needs the magic.
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, err
	}
	if len(head) < codec.MaxMagicLen() {
		return nil, errors.New("stream is too short to be an archive")
	}
	return head, nil
}

// readManifest consumes the first member, which the format requires to be the
// manifest. Reading it eagerly is what makes `arca list` answer immediately
// instead of streaming the whole archive.
func (r *Reader) readManifest() error {
	h, err := r.tr.Next()
	if err != nil {
		return fmt.Errorf("reading archive manifest: %w", err)
	}
	if h.Name != manifest.Name {
		return fmt.Errorf(
			"first archive member is %q, expected %s: this archive was not written by arca",
			h.Name, manifest.Name)
	}
	data, err := io.ReadAll(r.tr)
	if err != nil {
		return fmt.Errorf("reading archive manifest: %w", err)
	}
	m, err := manifest.ParseManifest(data)
	if err != nil {
		return err
	}
	r.manifest = m
	return nil
}

func (r *Reader) Manifest() *manifest.Manifest { return r.manifest }
func (r *Reader) Cipher() string               { return r.cipherName }
func (r *Reader) Compressor() string           { return r.compName }

// Next advances to the following member.
func (r *Reader) Next() (*tar.Header, error) { return r.tr.Next() }

// Read reads content from the current member.
func (r *Reader) Read(p []byte) (int, error) { return r.tr.Read(p) }

func (r *Reader) Close() error {
	var firstErr error
	for i := len(r.closers) - 1; i >= 0; i-- {
		if err := r.closers[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	r.closers = nil
	return firstErr
}

// ---------- inspect ----------

// Info is the cheap answer: what the archive says about itself, read from its
// first member alone.
type Info struct {
	Cipher     string
	Compressor string
	Manifest   *manifest.Manifest
}

func Inspect(path string, k *codec.Keyring) (*Info, error) {
	r, err := Open(path, k)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return &Info{Cipher: r.Cipher(), Compressor: r.Compressor(), Manifest: r.Manifest()}, nil
}

// ---------- list ----------

type ListEntry struct {
	Name       string
	Kind       string
	Size       int64
	Mode       fs.FileMode
	ModTime    time.Time
	LinkTarget string
}

type ListResult struct {
	Info    *Info
	Summary *manifest.Summary
	Entries []ListEntry
	Stats   manifest.Stats
}

// List enumerates every member. Unlike Inspect it streams the whole archive,
// which also authenticates it end to end.
func List(path string, k *codec.Keyring) (*ListResult, error) {
	r, err := Open(path, k)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	res := &ListResult{Info: &Info{Cipher: r.Cipher(), Compressor: r.Compressor(), Manifest: r.Manifest()}}
	err = eachMember(r, func(h *tar.Header, body io.Reader) error {
		res.Entries = append(res.Entries, ListEntry{
			Name:       memberName(h),
			Kind:       kindOf(h),
			Size:       h.Size,
			Mode:       h.FileInfo().Mode(),
			ModTime:    h.ModTime,
			LinkTarget: h.Linkname,
		})
		countMember(&res.Stats, h)
		return nil
	}, func(s *manifest.Summary) { res.Summary = s })
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ---------- verify ----------

type VerifyResult struct {
	Info     *Info
	Summary  *manifest.Summary
	Stats    manifest.Stats
	Complete bool
	Warnings []string
}

// Verify decrypts and decompresses the entire archive, discarding the content.
//
// That is a stronger check than a checksum alongside the file: the cipher
// authenticates every chunk and marks the final one, so tampering anywhere and
// truncation at the end both surface here. The presence of the summary member
// additionally proves the writer finished.
func Verify(path string, k *codec.Keyring) (*VerifyResult, error) {
	r, err := Open(path, k)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	res := &VerifyResult{Info: &Info{Cipher: r.Cipher(), Compressor: r.Compressor(), Manifest: r.Manifest()}}
	err = eachMember(r, func(h *tar.Header, body io.Reader) error {
		if _, err := io.Copy(io.Discard, body); err != nil {
			return fmt.Errorf("member %q: %w", h.Name, err)
		}
		countMember(&res.Stats, h)
		return nil
	}, func(s *manifest.Summary) {
		res.Summary = s
		res.Complete = true
	})
	if err != nil {
		return nil, err
	}

	if !res.Complete {
		return nil, errors.New(
			"archive has no completion record: it was interrupted while being written, or was altered")
	}
	if res.Summary.Stats != res.Stats {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"contents do not match the recorded summary: found %+v, recorded %+v", res.Stats, res.Summary.Stats))
	}
	return res, nil
}

// ---------- shared iteration ----------

// eachMember walks the members after the manifest, routing the summary to its
// own handler so callers never treat bookkeeping as user content.
func eachMember(r *Reader, visit func(*tar.Header, io.Reader) error, onSummary func(*manifest.Summary)) error {
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}

		if h.Name == manifest.SummaryName {
			data, err := io.ReadAll(r)
			if err != nil {
				return fmt.Errorf("reading %s: %w", manifest.SummaryName, err)
			}
			s, err := manifest.ParseSummary(data)
			if err != nil {
				return err
			}
			if onSummary != nil {
				onSummary(s)
			}
			continue
		}
		if h.Name == manifest.Name {
			continue // never expected twice, but never user content either
		}
		if err := visit(h, r); err != nil {
			return err
		}
	}
}

func memberName(h *tar.Header) string { return strings.TrimSuffix(h.Name, "/") }

func kindOf(h *tar.Header) string {
	switch h.Typeflag {
	case tar.TypeDir:
		return "dir"
	case tar.TypeSymlink:
		return "symlink"
	case tar.TypeLink:
		return "hardlink"
	case tar.TypeReg:
		return "file"
	}
	return "other"
}

func countMember(s *manifest.Stats, h *tar.Header) {
	switch h.Typeflag {
	case tar.TypeDir:
		s.Dirs++
	case tar.TypeSymlink:
		s.Symlinks++
	case tar.TypeLink:
		s.Hardlinks++
	case tar.TypeReg:
		s.Files++
		s.Bytes += h.Size
	}
}
