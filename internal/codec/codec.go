// Package codec defines the pluggable compression and encryption layers of the
// backup pipeline.
//
// The pipeline is `tar -> Compressor -> Cipher -> file`. Both stages are looked
// up through a registry and identified on the way back by their magic bytes, so
// restoring never depends on the file name or on the user remembering which
// algorithms were used. Adding gpg, openssl, xz or lz4 later means registering
// another implementation; the pipeline itself does not change.
package codec

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Level is an abstract compression effort, mapped by each Compressor onto
// whatever its own library calls these settings.
type Level int

const (
	LevelFastest Level = iota
	LevelDefault
	LevelBetter
	LevelBest
)

var levelNames = map[Level]string{
	LevelFastest: "fastest",
	LevelDefault: "default",
	LevelBetter:  "better",
	LevelBest:    "best",
}

func (l Level) String() string {
	if n, ok := levelNames[l]; ok {
		return n
	}
	return fmt.Sprintf("Level(%d)", int(l))
}

// ParseLevel resolves a configuration or flag value, case-insensitively.
func ParseLevel(s string) (Level, error) {
	for lvl, name := range levelNames {
		if strings.EqualFold(s, name) {
			return lvl, nil
		}
	}
	return 0, fmt.Errorf("unknown compression level %q (want one of: fastest, default, better, best)", s)
}

// CompressOpts carries the tunables shared by every compressor.
type CompressOpts struct {
	Level   Level
	Threads int // 0 means "decide from the machine"
}

// A Compressor is one registered compression format.
type Compressor interface {
	Name() string
	Ext() string   // file extension fragment, e.g. "zst"
	Magic() []byte // leading bytes that identify the format on disk
	NewWriter(w io.Writer, opts CompressOpts) (io.WriteCloser, error)
	NewReader(r io.Reader) (io.ReadCloser, error)
}

// Keyring is the key material handed to a Cipher. Which fields are meaningful
// depends on the cipher; age uses a passphrase OR recipients, never both.
//
// Passphrases are never read from the environment: that would expose them in
// /proc/<pid>/environ and in process listings.
type Keyring struct {
	Passphrase    []byte   // scrypt/symmetric mode
	Recipients    []string // public recipients, e.g. "age1...", "ssh-ed25519 ..."
	Identities    []string // inline private identities
	IdentityFiles []string // paths to private identity files
}

// Empty reports whether the keyring carries no usable key material at all.
func (k *Keyring) Empty() bool {
	return k == nil || (len(k.Passphrase) == 0 &&
		len(k.Recipients) == 0 &&
		len(k.Identities) == 0 &&
		len(k.IdentityFiles) == 0)
}

// A Cipher is one registered encryption format.
type Cipher interface {
	Name() string
	Ext() string
	Magic() []byte
	// Validate reports whether the keyring is usable for sealing, without
	// doing any work. Callers use it to fail before creating an output file,
	// and before paying for an expensive key derivation.
	Validate(k *Keyring) error
	Seal(w io.Writer, k *Keyring) (io.WriteCloser, error)
	Open(r io.Reader, k *Keyring) (io.ReadCloser, error)
}

var (
	compressors = map[string]Compressor{}
	ciphers     = map[string]Cipher{}
)

// RegisterCompressor adds a compressor to the registry. It panics on a
// duplicate name, which can only be a programming error at init time.
func RegisterCompressor(c Compressor) {
	if _, dup := compressors[c.Name()]; dup {
		panic("codec: duplicate compressor " + c.Name())
	}
	compressors[c.Name()] = c
}

// RegisterCipher adds a cipher to the registry.
func RegisterCipher(c Cipher) {
	if _, dup := ciphers[c.Name()]; dup {
		panic("codec: duplicate cipher " + c.Name())
	}
	ciphers[c.Name()] = c
}

func GetCompressor(name string) (Compressor, error) {
	c, ok := compressors[name]
	if !ok {
		return nil, fmt.Errorf("unknown compressor %q (available: %s)", name, strings.Join(CompressorNames(), ", "))
	}
	return c, nil
}

func GetCipher(name string) (Cipher, error) {
	c, ok := ciphers[name]
	if !ok {
		return nil, fmt.Errorf("unknown cipher %q (available: %s)", name, strings.Join(CipherNames(), ", "))
	}
	return c, nil
}

func CompressorNames() []string { return sortedKeys(compressors) }
func CipherNames() []string     { return sortedKeys(ciphers) }

func sortedKeys[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// DetectCompressor identifies a compression format from the start of a stream.
func DetectCompressor(peek []byte) (Compressor, bool) {
	for _, name := range CompressorNames() {
		if c := compressors[name]; hasMagic(peek, c.Magic()) {
			return c, true
		}
	}
	return nil, false
}

// DetectCipher identifies an encryption format from the start of a stream.
func DetectCipher(peek []byte) (Cipher, bool) {
	for _, name := range CipherNames() {
		if c := ciphers[name]; hasMagic(peek, c.Magic()) {
			return c, true
		}
	}
	return nil, false
}

func hasMagic(peek, magic []byte) bool {
	if len(magic) == 0 || len(peek) < len(magic) {
		return false
	}
	return string(peek[:len(magic)]) == string(magic)
}

// MaxMagicLen is how many bytes a caller must buffer before detection can
// succeed for any registered format.
func MaxMagicLen() int {
	max := 0
	for _, c := range compressors {
		if n := len(c.Magic()); n > max {
			max = n
		}
	}
	for _, c := range ciphers {
		if n := len(c.Magic()); n > max {
			max = n
		}
	}
	return max
}
