// Package walk turns the resolved sources into an ordered stream of archive
// entries.
//
// It is where most of the correctness risk of a backup tool lives: symlink
// loops, hardlinks, device nodes, unreadable directories, mount boundaries and
// the archive swallowing itself. Every one of those is handled explicitly and
// reported, because a backup that silently skips things is worse than one that
// fails loudly.
package walk

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/hamsa/arca/internal/config"
)

type Kind int

const (
	KindDir Kind = iota
	KindRegular
	KindSymlink
	KindHardlink
)

func (k Kind) String() string {
	switch k {
	case KindDir:
		return "dir"
	case KindRegular:
		return "regular"
	case KindSymlink:
		return "symlink"
	case KindHardlink:
		return "hardlink"
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Entry is one member of the archive, already carrying its mapped name.
type Entry struct {
	Kind       Kind
	Path       string      // absolute path on disk; empty for synthetic directories
	Member     string      // path inside the archive
	Info       fs.FileInfo // never nil
	LinkTarget string      // symlink target, as stored
	HardlinkTo string      // archive member holding the content, for KindHardlink

	// Group and Source attribute the entry to the configuration that asked for
	// it, so a plan can report per-source sizes. Source is -1 for the synthetic
	// parent directories the mapping implies.
	Group  string
	Source int
}

// Warning records something deliberately skipped. Warnings are surfaced to the
// user and recorded in the archive manifest.
type Warning struct {
	Path   string
	Reason string
}

func (w Warning) String() string { return w.Path + ": " + w.Reason }

type Result struct {
	Files     int
	Dirs      int
	Symlinks  int
	Hardlinks int
	Bytes     int64
	Warnings  []Warning
}

type Options struct {
	FollowSymlinks bool
	OneFilesystem  bool
	// Skip holds absolute paths to leave out unconditionally, such as the
	// backup file currently being written.
	Skip map[string]bool
}

type inode struct{ dev, ino uint64 }

type walker struct {
	opts    Options
	visit   func(Entry) error
	res     Result
	started time.Time

	group  string
	source int

	emitted     map[string]bool  // archive members already written
	inodes      map[inode]string // first archive member holding each inode
	visitedDirs map[inode]bool   // loop guard, only used when following symlinks
	rootDev     uint64
	haveRootDev bool
}

// Walk visits every entry of every source in a deterministic order, emitting
// synthetic parent directories so the mapped layout is complete.
//
// A source that does not exist is a warning, not a failure: configurations are
// shared across machines and `~/.gnupg` may simply be absent. If no source at
// all exists, that is an error, because an empty archive is never what the user
// meant.
func Walk(sources []config.ResolvedSource, opts Options, visit func(Entry) error) (*Result, error) {
	w := &walker{
		opts:        opts,
		visit:       visit,
		started:     time.Now(),
		emitted:     map[string]bool{},
		inodes:      map[inode]string{},
		visitedDirs: map[inode]bool{},
	}

	found := 0
	for i, src := range sources {
		w.group, w.source = src.Group, i
		info, err := w.statSource(src.Path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				w.warn(src.Path, "source does not exist, skipped")
				continue
			}
			w.warn(src.Path, "cannot read source: "+err.Error())
			continue
		}
		found++

		w.rootDev, w.haveRootDev = deviceOf(info)
		if err := w.emitParents(src.Member); err != nil {
			return &w.res, err
		}
		if err := w.walk(src.Path, src.Member, info, newMatcher(src.Exclude), src.Path); err != nil {
			return &w.res, err
		}
	}

	if found == 0 {
		return &w.res, fmt.Errorf("no source exists: nothing to back up (%d source(s) configured)", len(sources))
	}
	return &w.res, nil
}

func (w *walker) statSource(p string) (fs.FileInfo, error) {
	if w.opts.FollowSymlinks {
		return os.Stat(p)
	}
	return os.Lstat(p)
}

func (w *walker) warn(path, reason string) {
	w.res.Warnings = append(w.res.Warnings, Warning{Path: path, Reason: reason})
}

// emitParents writes directory entries for the ancestors a mapped member
// implies but which do not exist on disk, such as the "dotfiles" in
// "dotfiles/.ssh".
func (w *walker) emitParents(member string) error {
	dir := path.Dir(member)
	if dir == "." || dir == "/" || dir == "" {
		return nil
	}
	var ancestors []string
	for d := dir; d != "." && d != "/" && d != ""; d = path.Dir(d) {
		ancestors = append([]string{d}, ancestors...)
	}
	for _, a := range ancestors {
		if w.emitted[a] {
			continue
		}
		if err := w.emit(Entry{
			Kind:   KindDir,
			Member: a,
			Info:   syntheticDir{name: path.Base(a), modTime: w.started},
			Group:  w.group,
			Source: -1,
		}); err != nil {
			return err
		}
	}
	return nil
}

// entry stamps the currently walked source onto an entry.
func (w *walker) entry(e Entry) Entry {
	e.Group, e.Source = w.group, w.source
	return e
}

func (w *walker) emit(e Entry) error {
	if w.emitted[e.Member] {
		return nil
	}
	w.emitted[e.Member] = true

	switch e.Kind {
	case KindDir:
		w.res.Dirs++
	case KindRegular:
		w.res.Files++
		w.res.Bytes += e.Info.Size()
	case KindSymlink:
		w.res.Symlinks++
	case KindHardlink:
		w.res.Hardlinks++
	}
	if err := w.visit(e); err != nil {
		return fmt.Errorf("writing %q: %w", e.Member, err)
	}
	return nil
}

// walk handles one path, recursing into directories. root is the source root,
// used to build the relative path that exclude patterns match against.
func (w *walker) walk(p, member string, info fs.FileInfo, m matcher, root string) error {
	if w.opts.Skip[p] {
		return nil
	}
	if dev, ok := deviceOf(info); w.opts.OneFilesystem && w.haveRootDev && ok && dev != w.rootDev {
		w.warn(p, "on a different filesystem, skipped (one_filesystem is on)")
		return nil
	}

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return w.emitSymlink(p, member, info)

	case info.IsDir():
		if w.opts.FollowSymlinks {
			if id, ok := inodeOf(info); ok {
				if w.visitedDirs[id] {
					w.warn(p, "already visited through another path, skipped (symlink loop)")
					return nil
				}
				w.visitedDirs[id] = true
			}
		}
		if err := w.emit(w.entry(Entry{Kind: KindDir, Path: p, Member: member, Info: info})); err != nil {
			return err
		}
		return w.walkDir(p, member, m, root)

	case info.Mode().IsRegular():
		return w.emitRegular(p, member, info)

	default:
		w.warn(p, "not a regular file, directory or symlink ("+specialKind(info.Mode())+"), skipped")
		return nil
	}
}

func (w *walker) walkDir(p, member string, m matcher, root string) error {
	names, err := os.ReadDir(p)
	if err != nil {
		w.warn(p, "cannot list directory: "+err.Error())
		return nil
	}
	// os.ReadDir sorts by filename, which keeps archives reproducible.
	for _, de := range names {
		childPath := filepath.Join(p, de.Name())
		childMember := path.Join(member, de.Name())

		rel, relErr := filepath.Rel(root, childPath)
		if relErr != nil {
			rel = de.Name()
		}
		if m.match(childPath, rel, de.Name()) {
			continue
		}

		info, err := w.statChild(childPath)
		if err != nil {
			w.warn(childPath, "cannot stat: "+err.Error())
			continue
		}
		if err := w.walk(childPath, childMember, info, m, root); err != nil {
			return err
		}
	}
	return nil
}

// statChild resolves a child entry. When following symlinks a dangling target
// is not fatal: the link itself is still archived.
func (w *walker) statChild(p string) (fs.FileInfo, error) {
	if !w.opts.FollowSymlinks {
		return os.Lstat(p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return os.Lstat(p)
	}
	return info, nil
}

func (w *walker) emitSymlink(p, member string, info fs.FileInfo) error {
	target, err := os.Readlink(p)
	if err != nil {
		w.warn(p, "cannot read symlink: "+err.Error())
		return nil
	}
	return w.emit(w.entry(Entry{Kind: KindSymlink, Path: p, Member: member, Info: info, LinkTarget: target}))
}

// emitRegular stores content the first time an inode is seen and a hardlink
// reference afterwards, so a file with many names is not duplicated.
func (w *walker) emitRegular(p, member string, info fs.FileInfo) error {
	if id, ok := inodeOf(info); ok && linkCount(info) > 1 {
		if first, seen := w.inodes[id]; seen {
			return w.emit(w.entry(Entry{
				Kind: KindHardlink, Path: p, Member: member, Info: info, HardlinkTo: first,
			}))
		}
		w.inodes[id] = member
	}
	return w.emit(w.entry(Entry{Kind: KindRegular, Path: p, Member: member, Info: info}))
}

func specialKind(m fs.FileMode) string {
	switch {
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeNamedPipe != 0:
		return "fifo"
	case m&fs.ModeDevice != 0:
		return "device"
	case m&fs.ModeCharDevice != 0:
		return "character device"
	}
	return m.String()
}

// ---------- exclude matching ----------

// matcher follows gitignore's most useful convention: a pattern without a
// separator matches a basename at any depth, while a pattern with one is
// matched against the path.
type matcher struct{ patterns []string }

func newMatcher(patterns []string) matcher { return matcher{patterns: patterns} }

// Matches answers the same question for callers outside the walk, so that a
// screen marking a row as left out and the walk leaving it out are the same
// decision rather than two implementations of one rule that drift apart.
//
// absPath is the one part the walk has and a screen may not; a pattern is
// matched against it only as a last resort, so passing rel alone is exact for
// every pattern relative to a source root.
func Matches(patterns []string, relPath, base string) bool {
	return newMatcher(patterns).match("", relPath, base)
}

func (m matcher) match(absPath, relPath, base string) bool {
	for _, p := range m.patterns {
		if p == "" {
			continue
		}
		if !strings.ContainsRune(p, '/') {
			if ok, _ := doublestar.Match(p, base); ok {
				return true
			}
			continue
		}
		if ok, _ := doublestar.Match(p, filepath.ToSlash(relPath)); ok {
			return true
		}
		// Matches passes no absolute path; an empty one is not a path a
		// pattern should be allowed to match.
		if absPath == "" {
			continue
		}
		if ok, _ := doublestar.Match(p, filepath.ToSlash(absPath)); ok {
			return true
		}
	}
	return false
}

// ---------- platform bits ----------

func statT(info fs.FileInfo) (*syscall.Stat_t, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	return st, ok
}

func deviceOf(info fs.FileInfo) (uint64, bool) {
	st, ok := statT(info)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true
}

func inodeOf(info fs.FileInfo) (inode, bool) {
	st, ok := statT(info)
	if !ok {
		return inode{}, false
	}
	return inode{dev: uint64(st.Dev), ino: uint64(st.Ino)}, true
}

func linkCount(info fs.FileInfo) uint64 {
	st, ok := statT(info)
	if !ok {
		return 1
	}
	return uint64(st.Nlink)
}

// syntheticDir stands in for a mapped parent directory that has no counterpart
// on disk, so downstream code never has to special-case a nil FileInfo.
type syntheticDir struct {
	name    string
	modTime time.Time
}

func (d syntheticDir) Name() string       { return d.name }
func (d syntheticDir) Size() int64        { return 0 }
func (d syntheticDir) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (d syntheticDir) ModTime() time.Time { return d.modTime }
func (d syntheticDir) IsDir() bool        { return true }
func (d syntheticDir) Sys() any           { return nil }
