// Package archive builds and reads the backup artifact.
//
// The pipeline is strictly streaming: tar -> compressor -> cipher -> file.
// Nothing intermediate is ever written to disk in the clear, which is a
// security property rather than a performance one, and memory stays flat no
// matter how large the tree is.
package archive

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/manifest"
	"github.com/hamsa/arca/internal/walk"
)

// PartialSuffix marks an archive still being written. The final name appears
// only after every layer has been closed successfully, so a file with the real
// name is always a complete archive.
const PartialSuffix = ".partial"

// Spec describes the pipeline to build.
type Spec struct {
	Compressor codec.Compressor
	Compress   codec.CompressOpts
	Cipher     codec.Cipher
	Keyring    *codec.Keyring
}

// Writer assembles the archive. Layers are closed in reverse order and every
// close is checked: skipping the cipher's Close would drop the final
// authenticated chunk and produce a truncated file that still looks plausible.
type Writer struct {
	finalPath   string
	partialPath string

	file    *os.File
	counter *countingWriter
	cipherW io.WriteCloser
	compW   io.WriteCloser
	tar     *tar.Writer

	warnings []walk.Warning
	closed   bool
}

// Create opens a new archive. It refuses to touch an existing archive and
// refuses to resume over a leftover partial, because either would mean
// destroying a backup nobody asked it to destroy.
func Create(finalPath string, spec Spec) (*Writer, error) {
	if err := spec.Cipher.Validate(spec.Keyring); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(finalPath); err == nil {
		return nil, fmt.Errorf("%s already exists: refusing to overwrite an existing backup", finalPath)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("checking output path: %w", err)
	}

	partialPath := finalPath + PartialSuffix
	// O_EXCL so a concurrent run, or a crashed one, is reported rather than
	// silently overwritten.
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf(
				"%s already exists: another backup may be running, or a previous one was interrupted. "+
					"Remove it if you are sure, then run again", partialPath)
		}
		return nil, fmt.Errorf("creating output: %w", err)
	}

	w := &Writer{finalPath: finalPath, partialPath: partialPath, file: file}

	w.counter = &countingWriter{w: file}
	w.cipherW, err = spec.Cipher.Seal(w.counter, spec.Keyring)
	if err != nil {
		w.cleanup()
		return nil, fmt.Errorf("initialising encryption: %w", err)
	}
	w.compW, err = spec.Compressor.NewWriter(w.cipherW, spec.Compress)
	if err != nil {
		w.cleanup()
		return nil, fmt.Errorf("initialising compression: %w", err)
	}
	w.tar = tar.NewWriter(w.compW)
	return w, nil
}

// BytesWritten reports ciphertext bytes flushed so far. It lags the input
// because the compressor and cipher buffer.
func (w *Writer) BytesWritten() int64 { return w.counter.n }

// Warnings lists what the writer itself decided to skip or adjust.
func (w *Writer) Warnings() []walk.Warning { return w.warnings }

func (w *Writer) warn(path, reason string) {
	w.warnings = append(w.warnings, walk.Warning{Path: path, Reason: reason})
}

// AddDocument stores a generated JSON document, used for the manifest and the
// summary.
func (w *Writer) AddDocument(name string, data []byte, modeTimeFrom *tar.Header) error {
	h := &tar.Header{
		Name:     name,
		Mode:     0o600,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if modeTimeFrom != nil {
		h.ModTime = modeTimeFrom.ModTime
	}
	if err := w.tar.WriteHeader(h); err != nil {
		return fmt.Errorf("writing %s header: %w", name, err)
	}
	if _, err := w.tar.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}

// Add writes one walked entry. It returns the number of content bytes stored,
// which is zero for everything but regular files.
func (w *Writer) Add(e walk.Entry) (int64, error) {
	// Regular files are opened before their header is written: if the open
	// fails there is still a chance to skip the entry cleanly, whereas a
	// header already committed would leave the tar stream desynchronised.
	var src *os.File
	if e.Kind == walk.KindRegular {
		f, err := os.Open(e.Path)
		if err != nil {
			w.warn(e.Path, "cannot open for reading, skipped: "+err.Error())
			return 0, nil
		}
		src = f
		defer src.Close()
	}

	h, err := w.header(e)
	if err != nil {
		w.warn(e.Path, "cannot build archive header, skipped: "+err.Error())
		return 0, nil
	}
	if err := w.tar.WriteHeader(h); err != nil {
		return 0, fmt.Errorf("writing header for %q: %w", e.Member, err)
	}
	if src == nil {
		return 0, nil
	}

	copied, grew, err := CopyExactly(w.tar, src, h.Size)
	if err != nil {
		return copied, fmt.Errorf("writing %q: %w", e.Member, err)
	}
	switch {
	case copied < h.Size:
		w.warn(e.Path, fmt.Sprintf(
			"file shrank while being read (%d of %d bytes); the archived copy is zero-padded", copied, h.Size))
	case grew:
		w.warn(e.Path, fmt.Sprintf(
			"file grew while being read; the archived copy is truncated to %d bytes", h.Size))
	}
	return copied, nil
}

func (w *Writer) header(e walk.Entry) (*tar.Header, error) {
	link := ""
	if e.Kind == walk.KindSymlink {
		link = e.LinkTarget
	}
	h, err := tar.FileInfoHeader(e.Info, link)
	if err != nil {
		return nil, err
	}

	// PAX preserves sub-second timestamps, long names and UTF-8 without the
	// silent rounding and truncation of the older tar formats.
	h.Format = tar.FormatPAX
	h.Name = e.Member
	if e.Kind == walk.KindDir {
		h.Name = e.Member + "/"
		if e.Info.Sys() == nil {
			// A synthetic parent directory has no real owner; use the caller's
			// so a root restore does not end up with a stray root-owned path.
			h.Uid, h.Gid = os.Getuid(), os.Getgid()
		}
	}
	if e.Kind == walk.KindHardlink {
		h.Typeflag = tar.TypeLink
		h.Linkname = e.HardlinkTo
		h.Size = 0
	}
	return h, nil
}

// CopyExactly writes exactly size bytes to dst, padding with zeros if src ends
// early and stopping if it has more.
//
// A tar header commits to a size before the content is read. A file that is
// appended to or truncated in that window would otherwise desynchronise the
// whole stream, corrupting every member after it.
func CopyExactly(dst io.Writer, src io.Reader, size int64) (copied int64, grew bool, err error) {
	if size > 0 {
		copied, err = io.CopyN(dst, src, size)
		if err != nil && !errors.Is(err, io.EOF) {
			return copied, false, err
		}
		err = nil
	}

	if copied < size {
		if err := padZero(dst, size-copied); err != nil {
			return copied, false, err
		}
		return copied, false, nil
	}

	// The source still having data means the file grew; the extra is dropped.
	var probe [1]byte
	if n, _ := src.Read(probe[:]); n > 0 {
		grew = true
	}
	return copied, grew, nil
}

func padZero(dst io.Writer, n int64) error {
	const chunk = 32 * 1024
	zeros := make([]byte, chunk)
	for n > 0 {
		size := int64(chunk)
		if n < size {
			size = n
		}
		written, err := dst.Write(zeros[:size])
		if err != nil {
			return err
		}
		n -= int64(written)
	}
	return nil
}

// Close finishes the archive: the summary member, then every layer in reverse
// order, then a durable rename into place.
func (w *Writer) Close(summary manifest.Summary) error {
	if w.closed {
		return errors.New("archive already closed")
	}
	w.closed = true

	data, err := manifest.Marshal(summary)
	if err != nil {
		w.cleanup()
		return fmt.Errorf("rendering %s: %w", manifest.SummaryName, err)
	}
	if err := w.AddDocument(manifest.SummaryName, data, nil); err != nil {
		w.cleanup()
		return err
	}

	// Reverse order, every error checked. The cipher only emits its final
	// authenticated chunk on Close; dropping it yields a file that decrypts
	// almost to the end and then fails.
	for _, step := range []struct {
		what  string
		close func() error
	}{
		{"tar", w.tar.Close},
		{"compressor", w.compW.Close},
		{"cipher", w.cipherW.Close},
		{"sync", w.file.Sync},
		{"file", w.file.Close},
	} {
		if err := step.close(); err != nil {
			w.removePartial()
			return fmt.Errorf("closing %s: %w", step.what, err)
		}
	}

	if err := os.Rename(w.partialPath, w.finalPath); err != nil {
		w.removePartial()
		return fmt.Errorf("finalising archive: %w", err)
	}
	return nil
}

// Abort discards a partial archive. A failed backup must never leave anything
// that could be mistaken for a usable one.
func (w *Writer) Abort() {
	if w.closed {
		return
	}
	w.closed = true
	w.cleanup()
}

func (w *Writer) cleanup() {
	if w.tar != nil {
		_ = w.tar.Close()
	}
	if w.compW != nil {
		_ = w.compW.Close()
	}
	if w.cipherW != nil {
		_ = w.cipherW.Close()
	}
	if w.file != nil {
		_ = w.file.Close()
	}
	w.removePartial()
}

func (w *Writer) removePartial() {
	if w.partialPath != "" {
		_ = os.Remove(w.partialPath)
	}
}

// countingWriter tracks how many ciphertext bytes have reached the file.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// ArchiveName builds the conventional file name for a pipeline, so the
// extensions always describe what is actually inside.
func ArchiveName(base string, comp codec.Compressor, ciph codec.Cipher) string {
	return base + ".tar." + comp.Ext() + "." + ciph.Ext()
}
