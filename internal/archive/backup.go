package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/henriquemenezes/arca/internal/codec"
	"github.com/henriquemenezes/arca/internal/config"
	"github.com/henriquemenezes/arca/internal/manifest"
	"github.com/henriquemenezes/arca/internal/walk"
)

// Progress reports how far a backup has got. It is emitted per entry; throttle
// the rendering at the call site rather than here, so tests can rely on exact
// final values.
type Progress struct {
	Member  string
	Files   int
	Dirs    int
	Bytes   int64 // content bytes read from the sources
	Written int64 // ciphertext bytes flushed to disk so far
}

type BackupOptions struct {
	Config      *config.Config
	ConfigTOML  string // stored verbatim in the manifest
	Keyring     *codec.Keyring
	OutputPath  string
	ToolVersion string
	// Planned carries the counts from a prior dry pass, used for the manifest
	// and for a percentage in the progress display. Optional.
	Planned  *manifest.Stats
	Progress func(Progress)
}

type BackupResult struct {
	Path         string
	Stats        manifest.Stats
	Warnings     []string
	ArchiveBytes int64
	Manifest     *manifest.Manifest
}

// Backup walks the configured sources and writes a single encrypted archive.
//
// Every failure path removes the partial file, so the only artifact that can
// survive under the final name is a complete archive.
func Backup(opts BackupOptions) (*BackupResult, error) {
	cfg := opts.Config
	if cfg == nil {
		return nil, errors.New("no configuration provided")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	sources, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}

	compressor, err := codec.GetCompressor(cfg.Settings.Compressor)
	if err != nil {
		return nil, err
	}
	level, err := codec.ParseLevel(cfg.Settings.Compression)
	if err != nil {
		return nil, err
	}
	cipher, err := codec.GetCipher(cfg.Settings.Cipher)
	if err != nil {
		return nil, err
	}
	// Checked before anything is created, so a bad keyring never leaves a file
	// behind and never pays for a key derivation first.
	if err := cipher.Validate(opts.Keyring); err != nil {
		return nil, err
	}

	mf, err := manifest.Build(opts.ToolVersion, cfg, opts.ConfigTOML, opts.Planned)
	if err != nil {
		return nil, err
	}
	manifestJSON, err := manifest.Marshal(mf)
	if err != nil {
		return nil, err
	}

	outPath, err := filepath.Abs(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("resolving output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	w, err := Create(outPath, Spec{
		Compressor: compressor,
		Compress:   codec.CompressOpts{Level: level, Threads: cfg.Settings.Threads},
		Cipher:     cipher,
		Keyring:    opts.Keyring,
	})
	if err != nil {
		return nil, err
	}

	if err := w.AddDocument(manifest.Name, manifestJSON, nil); err != nil {
		w.Abort()
		return nil, err
	}

	var progress Progress
	walkRes, err := walk.Walk(sources, walk.Options{
		FollowSymlinks: cfg.Settings.FollowSymlinks,
		OneFilesystem:  cfg.Settings.OneFilesystem,
		// The archive must never swallow itself.
		Skip: map[string]bool{outPath: true, outPath + PartialSuffix: true},
	}, func(e walk.Entry) error {
		n, err := w.Add(e)
		if err != nil {
			return err
		}
		switch e.Kind {
		case walk.KindRegular:
			progress.Files++
			progress.Bytes += n
		case walk.KindDir:
			progress.Dirs++
		}
		if opts.Progress != nil {
			progress.Member = e.Member
			progress.Written = w.BytesWritten()
			opts.Progress(progress)
		}
		return nil
	})
	if err != nil {
		w.Abort()
		return nil, err
	}

	stats := manifest.StatsFrom(walkRes)
	warnings := formatWarnings(append(append([]walk.Warning{}, walkRes.Warnings...), w.Warnings()...))

	if err := w.Close(manifest.Summary{
		FormatVersion: manifest.FormatVersion,
		CompletedAt:   mf.CreatedAt,
		Stats:         stats,
		Warnings:      warnings,
	}); err != nil {
		return nil, err
	}

	size := int64(0)
	if info, statErr := os.Stat(outPath); statErr == nil {
		size = info.Size()
	}
	return &BackupResult{
		Path:         outPath,
		Stats:        stats,
		Warnings:     warnings,
		ArchiveBytes: size,
		Manifest:     mf,
	}, nil
}

func formatWarnings(ws []walk.Warning) []string {
	if len(ws) == 0 {
		return nil
	}
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.String())
	}
	return out
}
