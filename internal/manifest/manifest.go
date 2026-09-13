// Package manifest describes an archive from the inside.
//
// Both documents live inside the encryption, so recording original paths, the
// mapping and the host is not a metadata leak: an attacker holding the file
// sees only ciphertext. That is the whole point of putting them here rather
// than in a sidecar.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"time"

	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/walk"
)

// FormatVersion guards future changes to these documents. A reader that meets a
// newer version says so plainly instead of guessing.
const FormatVersion = 1

const (
	Name        = config.ManifestName
	SummaryName = config.SummaryName
)

// Manifest is the first member of the archive: what this backup set out to do.
// Everything in it is known before a single byte of content is read, which is
// what lets `arca list` answer without streaming the whole archive.
type Manifest struct {
	Tool          string    `json:"tool"`
	ToolVersion   string    `json:"tool_version"`
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	Host          Host      `json:"host"`
	Pipeline      Pipeline  `json:"pipeline"`
	Groups        []Group   `json:"groups"`
	Planned       *Stats    `json:"planned,omitempty"`
	Config        string    `json:"config"` // the effective configuration, verbatim
}

type Host struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	User     string `json:"user"`
}

type Pipeline struct {
	Compressor     string `json:"compressor"`
	Compression    string `json:"compression"`
	Cipher         string `json:"cipher"`
	EncryptionMode string `json:"encryption_mode"` // "passphrase" or "recipients"
	Recipients     int    `json:"recipients,omitempty"`
}

type Group struct {
	Name    string   `json:"name"`
	Dest    string   `json:"dest"`
	Sources []Source `json:"sources"`
}

// Source records where content came from and where it now lives, so a restore
// can explain itself without the user holding any external notes.
type Source struct {
	Path   string `json:"path"`
	Member string `json:"member"`
}

type Stats struct {
	Files     int   `json:"files"`
	Dirs      int   `json:"dirs"`
	Symlinks  int   `json:"symlinks"`
	Hardlinks int   `json:"hardlinks"`
	Bytes     int64 `json:"bytes"`
}

func StatsFrom(r *walk.Result) Stats {
	if r == nil {
		return Stats{}
	}
	return Stats{
		Files:     r.Files,
		Dirs:      r.Dirs,
		Symlinks:  r.Symlinks,
		Hardlinks: r.Hardlinks,
		Bytes:     r.Bytes,
	}
}

// Summary is the last member of the archive: what the backup actually captured,
// including everything it decided to skip.
type Summary struct {
	FormatVersion int       `json:"format_version"`
	CompletedAt   time.Time `json:"completed_at"`
	Stats         Stats     `json:"stats"`
	Warnings      []string  `json:"warnings"`
}

// Build assembles the manifest for a run. planned may be nil when no counting
// pass was made.
func Build(toolVersion string, cfg *config.Config, cfgTOML string, planned *Stats) (*Manifest, error) {
	resolved, err := cfg.Resolve()
	if err != nil {
		return nil, err
	}
	byGroup := map[string][]Source{}
	for _, r := range resolved {
		byGroup[r.Group] = append(byGroup[r.Group], Source{Path: r.Path, Member: r.Member})
	}

	groups := make([]Group, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		groups = append(groups, Group{Name: g.Name, Dest: g.Dest, Sources: byGroup[g.Name]})
	}

	mode := "passphrase"
	if n := len(cfg.Encryption.Recipients); n > 0 {
		mode = "recipients"
	}

	return &Manifest{
		Tool:          "arca",
		ToolVersion:   toolVersion,
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC(),
		Host:          currentHost(),
		Pipeline: Pipeline{
			Compressor:     cfg.Settings.Compressor,
			Compression:    cfg.Settings.Compression,
			Cipher:         cfg.Settings.Cipher,
			EncryptionMode: mode,
			Recipients:     len(cfg.Encryption.Recipients),
		},
		Groups:  groups,
		Planned: planned,
		Config:  cfgTOML,
	}, nil
}

func currentHost() Host {
	h := Host{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if name, err := os.Hostname(); err == nil {
		h.Hostname = name
	}
	if u, err := user.Current(); err == nil {
		h.User = u.Username
	}
	return h
}

// Marshal renders a document for storage. Indented, because a human reading it
// during a recovery should not need extra tooling.
func Marshal(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Name, err)
	}
	if m.FormatVersion > FormatVersion {
		return nil, fmt.Errorf(
			"archive uses format version %d but this build understands up to %d: upgrade arca, "+
				"or restore manually with: age -d FILE | zstd -d | tar -x",
			m.FormatVersion, FormatVersion)
	}
	return &m, nil
}

func ParseSummary(data []byte) (*Summary, error) {
	var s Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", SummaryName, err)
	}
	return &s, nil
}
