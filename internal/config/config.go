// Package config loads and validates the declarative backup manifest.
//
// The central idea is the mapping: a group has a destination directory inside
// the archive, and each source contributes its basename (or an explicit alias)
// under it. That is what turns `.ssh` and `.aws` into `dotfiles/.ssh` and
// `dotfiles/.aws`, and it is applied while writing the archive, so restoring is
// a plain extraction.
//
// All three front-ends (CLI flags, this file, the TUI) converge on the same
// Config, so there is a single execution path. Precedence is
// defaults < file < flags/TUI.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/hamsa/arca/internal/codec"
)

// ManifestName is the first member of every archive: what the backup intended
// to capture. SummaryName is the last: what it actually captured. Writing the
// summary only on success makes its presence a completion marker, on top of the
// truncation detection the cipher already provides.
//
// No source may shadow either name.
const (
	ManifestName = "BACKUP-MANIFEST.json"
	SummaryName  = "BACKUP-SUMMARY.json"
)

// ReservedNames are archive-root members the tool writes itself.
var ReservedNames = []string{ManifestName, SummaryName}

// Source is one directory or file to back up. It parses from either a bare
// string ("~/.ssh") or a table ({ path = "...", as = "..." }); the alias form
// exists to resolve basename collisions such as ~/.config/nvim against
// ~/.local/share/nvim.
type Source struct {
	Path string
	As   string
}

// Group is a set of sources sharing a destination directory in the archive.
type Group struct {
	Name    string
	Dest    string
	Sources []Source
	Exclude []string
}

type Settings struct {
	Compression    string `toml:"compression"`
	Compressor     string `toml:"compressor"`
	Cipher         string `toml:"cipher"`
	Threads        int    `toml:"threads"`
	FollowSymlinks bool   `toml:"follow_symlinks"`
	OneFilesystem  bool   `toml:"one_filesystem"`
}

// Encryption holds public recipients. An empty list selects passphrase mode;
// a non-empty list selects X25519 mode. The two can never be combined, because
// the age spec forbids it.
type Encryption struct {
	Recipients []string `toml:"recipients"`
}

type Config struct {
	Settings   Settings
	Encryption Encryption
	Groups     []Group
}

// Default returns the built-in configuration, the lowest precedence layer.
func Default() *Config {
	return &Config{
		Settings: Settings{
			Compression:    "best",
			Compressor:     "zstd",
			Cipher:         "age",
			Threads:        0, // decided from the machine
			FollowSymlinks: false,
			OneFilesystem:  true,
		},
	}
}

// ---------- parsing ----------

type rawConfig struct {
	Settings   Settings   `toml:"settings"`
	Encryption Encryption `toml:"encryption"`
	Groups     []rawGroup `toml:"group"`
}

type rawGroup struct {
	Name    string   `toml:"name"`
	Dest    string   `toml:"dest"`
	Sources []any    `toml:"sources"`
	Exclude []string `toml:"exclude"`
}

// Load reads a config file from disk.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes TOML over the defaults. Unknown keys are rejected: in a backup
// tool a typo that silently disables a setting is worse than a failed run.
func Parse(data []byte) (*Config, error) {
	def := Default()
	raw := rawConfig{Settings: def.Settings, Encryption: def.Encryption}

	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse config: %s", describeTOMLError(err))
	}

	c := &Config{Settings: raw.Settings, Encryption: raw.Encryption}
	for i, rg := range raw.Groups {
		g := Group{Name: rg.Name, Dest: rg.Dest, Exclude: rg.Exclude}
		for j, rs := range rg.Sources {
			src, err := toSource(rs)
			if err != nil {
				return nil, fmt.Errorf("group %q: source #%d: %w", groupLabel(rg.Name, i), j+1, err)
			}
			g.Sources = append(g.Sources, src)
		}
		c.Groups = append(c.Groups, g)
	}
	return c, nil
}

// describeTOMLError unwraps go-toml's structured errors, whose Error() text
// omits the detail that makes a config typo fixable: which key, and where.
func describeTOMLError(err error) string {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		return "unknown key in config file\n" + strict.String()
	}
	var decErr *toml.DecodeError
	if errors.As(err, &decErr) {
		return decErr.Error() + "\n" + decErr.String()
	}
	return err.Error()
}

func groupLabel(name string, i int) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("#%d", i+1)
}

// toSource accepts the two source spellings and rejects anything else with a
// message that names the offending key, so a typo is actionable.
func toSource(v any) (Source, error) {
	switch t := v.(type) {
	case string:
		return Source{Path: t}, nil
	case map[string]any:
		var s Source
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			str, ok := t[k].(string)
			if !ok {
				return Source{}, fmt.Errorf("key %q must be a string", k)
			}
			switch k {
			case "path":
				s.Path = str
			case "as":
				s.As = str
			default:
				return Source{}, fmt.Errorf("unknown key %q (expected \"path\" and optionally \"as\")", k)
			}
		}
		if s.Path == "" {
			return Source{}, errors.New(`missing "path"`)
		}
		return s, nil
	default:
		return Source{}, fmt.Errorf(`must be a path string or a { path = "...", as = "..." } table, got %T`, v)
	}
}

// ---------- overrides ----------

// Overrides is the flag/TUI layer. Nil pointers leave the existing value alone,
// so an unset flag never clobbers a config file value.
type Overrides struct {
	Compression    *string
	Compressor     *string
	Cipher         *string
	Threads        *int
	FollowSymlinks *bool
	OneFilesystem  *bool
	Recipients     []string // non-nil replaces the configured list wholesale
	Sources        []string // ad-hoc "path[:dest]" specs, appended as groups
}

func (c *Config) Apply(o Overrides) error {
	setIf(&c.Settings.Compression, o.Compression)
	setIf(&c.Settings.Compressor, o.Compressor)
	setIf(&c.Settings.Cipher, o.Cipher)
	setIf(&c.Settings.Threads, o.Threads)
	setIf(&c.Settings.FollowSymlinks, o.FollowSymlinks)
	setIf(&c.Settings.OneFilesystem, o.OneFilesystem)
	if o.Recipients != nil {
		c.Encryption.Recipients = o.Recipients
	}
	return c.addAdHocSources(o.Sources)
}

func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// addAdHocSources turns `--source PATH[:DEST]` flags into groups, so a backup
// can run with no config file at all. Specs sharing a destination are merged
// into one group.
func (c *Config) addAdHocSources(specs []string) error {
	if len(specs) == 0 {
		return nil
	}
	byDest := map[string][]Source{}
	var order []string
	for _, spec := range specs {
		path, dest, err := ParseSourceSpec(spec)
		if err != nil {
			return err
		}
		if _, seen := byDest[dest]; !seen {
			order = append(order, dest)
		}
		byDest[dest] = append(byDest[dest], Source{Path: path})
	}
	for _, dest := range order {
		c.Groups = append(c.Groups, Group{
			Name:    adHocGroupName(dest),
			Dest:    dest,
			Sources: byDest[dest],
		})
	}
	return nil
}

func adHocGroupName(dest string) string {
	if dest == "" {
		return "cli"
	}
	return "cli:" + dest
}

// ParseSourceSpec splits a `--source` value into its path and destination. The
// split is on the last colon so that the common case, a path without colons,
// behaves predictably.
func ParseSourceSpec(spec string) (path, dest string, err error) {
	path = spec
	if i := strings.LastIndex(spec, ":"); i >= 0 {
		path, dest = spec[:i], spec[i+1:]
	}
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("source %q: missing path (expected PATH[:DEST])", spec)
	}
	return path, dest, nil
}

// ---------- validation ----------

// Validate checks everything that can be known without touching the source
// trees: structure, destination safety, and archive-path collisions. Whether a
// source actually exists is reported later, by the walk.
func (c *Config) Validate() error {
	if _, err := codec.ParseLevel(c.Settings.Compression); err != nil {
		return err
	}
	if _, err := codec.GetCompressor(c.Settings.Compressor); err != nil {
		return err
	}
	if _, err := codec.GetCipher(c.Settings.Cipher); err != nil {
		return err
	}
	if c.Settings.Threads < 0 {
		return fmt.Errorf("threads = %d: must be zero (auto) or positive", c.Settings.Threads)
	}
	if len(c.Groups) == 0 {
		return errors.New("no groups configured: nothing would be backed up")
	}

	seenGroup := map[string]bool{}
	// owner maps an archive path to the source that claimed it, so a collision
	// can name both sides.
	owner := map[string]string{}

	for _, g := range c.Groups {
		if strings.TrimSpace(g.Name) == "" {
			return errors.New(`every group needs a non-empty "name"`)
		}
		if seenGroup[g.Name] {
			return fmt.Errorf("duplicate group name %q", g.Name)
		}
		seenGroup[g.Name] = true

		if err := validArchivePath(g.Dest); err != nil {
			return fmt.Errorf("group %q: dest %q: %w", g.Name, g.Dest, err)
		}
		if len(g.Sources) == 0 {
			return fmt.Errorf("group %q has no sources", g.Name)
		}

		for _, s := range g.Sources {
			if strings.TrimSpace(s.Path) == "" {
				return fmt.Errorf("group %q: empty source path", g.Name)
			}
			if s.As != "" {
				if err := validAlias(s.As); err != nil {
					return fmt.Errorf("group %q: source %q: as %q: %w", g.Name, s.Path, s.As, err)
				}
			}

			abs, err := ExpandPath(s.Path)
			if err != nil {
				return fmt.Errorf("group %q: source %q: %w", g.Name, s.Path, err)
			}
			if !filepath.IsAbs(abs) {
				return fmt.Errorf("group %q: source %q must be an absolute path or start with ~/", g.Name, s.Path)
			}

			member := archiveMember(g.Dest, abs, s.As)
			for _, reserved := range ReservedNames {
				if member == reserved {
					return fmt.Errorf("group %q: source %q would be stored as %s, which is reserved by the archive format; give it an \"as\" alias",
						g.Name, s.Path, reserved)
				}
			}
			if prev, dup := owner[member]; dup {
				return fmt.Errorf("archive path collision at %q: both %s and %s map there; "+
					"give one of them an \"as\" alias", member, prev, abs)
			}
			owner[member] = abs
		}
	}
	return nil
}

func validArchivePath(p string) error {
	if p == "" {
		return nil // archive root
	}
	if filepath.IsAbs(p) {
		return errors.New("must be relative to the archive root")
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("must not climb outside the archive root")
	}
	return nil
}

func validAlias(a string) error {
	if strings.ContainsRune(a, filepath.Separator) {
		return errors.New("must be a single name, not a path")
	}
	if a == "." || a == ".." {
		return errors.New(`must not be "." or ".."`)
	}
	return nil
}

// ---------- resolution ----------

// ResolvedSource is one source ready for the walk: where to read from, and the
// prefix its contents take inside the archive.
type ResolvedSource struct {
	Group   string
	Path    string // absolute path on disk
	Member  string // prefix inside the archive, e.g. "dotfiles/.ssh"
	Exclude []string
}

// Resolve expands paths and computes archive members. It is pure: call
// Validate first for the safety checks.
func (c *Config) Resolve() ([]ResolvedSource, error) {
	var out []ResolvedSource
	for _, g := range c.Groups {
		for _, s := range g.Sources {
			abs, err := ExpandPath(s.Path)
			if err != nil {
				return nil, fmt.Errorf("group %q: source %q: %w", g.Name, s.Path, err)
			}
			out = append(out, ResolvedSource{
				Group:   g.Name,
				Path:    abs,
				Member:  archiveMember(g.Dest, abs, s.As),
				Exclude: g.Exclude,
			})
		}
	}
	return out, nil
}

func archiveMember(dest, absPath, alias string) string {
	name := alias
	if name == "" {
		name = filepath.Base(absPath)
	}
	if dest == "" {
		return name
	}
	return filepath.Join(filepath.Clean(dest), name)
}

// ExpandPath resolves a leading ~/ against the current user's home directory.
// A ~user form is deliberately left untouched rather than guessed at.
func ExpandPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("empty path")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot expand %q: %w", p, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Clean(p), nil
}
