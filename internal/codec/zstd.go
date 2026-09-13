package codec

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

func init() { RegisterCompressor(zstdCompressor{}) }

// zstdCompressor uses klauspost/compress, a pure-Go implementation. That choice
// is what keeps the binary cgo-free and cross-compilable to macOS, at the cost
// of a maximum ratio closer to `zstd -11` than to `zstd -19`.
type zstdCompressor struct{}

func (zstdCompressor) Name() string { return "zstd" }
func (zstdCompressor) Ext() string  { return "zst" }

// Magic is the zstd frame magic 0xFD2FB528, stored little-endian.
func (zstdCompressor) Magic() []byte { return []byte{0x28, 0xB5, 0x2F, 0xFD} }

func (zstdCompressor) NewWriter(w io.Writer, opts CompressOpts) (io.WriteCloser, error) {
	encOpts := []zstd.EOption{zstd.WithEncoderLevel(zstdLevel(opts.Level))}
	if opts.Threads > 0 {
		encOpts = append(encOpts, zstd.WithEncoderConcurrency(opts.Threads))
	}
	return zstd.NewWriter(w, encOpts...)
}

func (zstdCompressor) NewReader(r io.Reader) (io.ReadCloser, error) {
	d, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return d.IOReadCloser(), nil
}

func zstdLevel(l Level) zstd.EncoderLevel {
	switch l {
	case LevelFastest:
		return zstd.SpeedFastest
	case LevelBetter:
		return zstd.SpeedBetterCompression
	case LevelBest:
		return zstd.SpeedBestCompression
	default:
		return zstd.SpeedDefault
	}
}
