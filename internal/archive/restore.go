package archive

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/manifest"
)

type RestoreOptions struct {
	Path    string
	Keyring *codec.Keyring
	Target  string
	// Groups restricts the restore to these manifest groups. Empty means all.
	Groups []string
	DryRun bool
	// Overwrite allows replacing files that already exist in the target.
	Overwrite bool
	// PreserveOwner restores uid/gid. It only has an effect when running as
	// root; otherwise the restored files belong to the current user, which is
	// almost always what is wanted.
	PreserveOwner bool
	Progress      func(Progress)
}

type RestoreResult struct {
	Target    string
	Files     int
	Dirs      int
	Symlinks  int
	Hardlinks int
	Bytes     int64
	Warnings  []string
	Manifest  *manifest.Manifest
}

// deferredDir remembers a directory's final metadata, applied after its
// children are written. A directory stored read-only (0500) would otherwise
// block its own contents.
type deferredDir struct {
	path string
	hdr  *tar.Header
}

// Restore extracts the archive into target.
//
// The mapping is not re-derived here: the archive already stores members under
// their mapped names, so extraction reproduces the intended layout directly.
// The manifest is used for group filtering and for explaining the result.
func Restore(opts RestoreOptions) (*RestoreResult, error) {
	if strings.TrimSpace(opts.Target) == "" {
		return nil, errors.New("no restore target given")
	}
	target, err := filepath.Abs(opts.Target)
	if err != nil {
		return nil, fmt.Errorf("resolving target: %w", err)
	}

	r, err := Open(opts.Path, opts.Keyring)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	filter, err := newGroupFilter(r.Manifest(), opts.Groups)
	if err != nil {
		return nil, err
	}

	if !opts.DryRun {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, fmt.Errorf("creating target directory: %w", err)
		}
	}

	x := &extractor{opts: opts, target: target, result: &RestoreResult{Target: target, Manifest: r.Manifest()}}
	err = eachMember(r, func(h *tar.Header, body io.Reader) error {
		if !filter.allows(memberName(h)) {
			return nil
		}
		return x.member(h, body)
	}, nil)
	if err != nil {
		return nil, err
	}
	if err := x.applyDeferredDirs(); err != nil {
		return nil, err
	}
	return x.result, nil
}

// ---------- group filtering ----------

type groupFilter struct {
	all      bool
	prefixes []string
}

func newGroupFilter(m *manifest.Manifest, groups []string) (*groupFilter, error) {
	if len(groups) == 0 {
		return &groupFilter{all: true}, nil
	}
	known := map[string][]string{}
	var names []string
	for _, g := range m.Groups {
		names = append(names, g.Name)
		for _, s := range g.Sources {
			known[g.Name] = append(known[g.Name], s.Member)
		}
	}

	f := &groupFilter{}
	for _, want := range groups {
		members, ok := known[want]
		if !ok {
			return nil, fmt.Errorf("unknown group %q: this archive holds %s",
				want, strings.Join(names, ", "))
		}
		f.prefixes = append(f.prefixes, members...)
	}
	return f, nil
}

func (f *groupFilter) allows(member string) bool {
	if f.all {
		return true
	}
	for _, p := range f.prefixes {
		if member == p || strings.HasPrefix(member, p+"/") {
			return true
		}
	}
	return false
}

// ---------- extraction ----------

type extractor struct {
	opts     RestoreOptions
	target   string
	result   *RestoreResult
	deferred []deferredDir
	progress Progress
}

func (x *extractor) warn(name, reason string) {
	x.result.Warnings = append(x.result.Warnings, name+": "+reason)
}

func (x *extractor) member(h *tar.Header, body io.Reader) error {
	name := memberName(h)

	dest, err := x.safePath(name)
	if err != nil {
		return err
	}

	switch h.Typeflag {
	case tar.TypeDir:
		if err := x.dir(dest, h); err != nil {
			return err
		}
		x.result.Dirs++
	case tar.TypeReg:
		if err := x.file(dest, h, body); err != nil {
			return err
		}
		x.result.Files++
		x.result.Bytes += h.Size
	case tar.TypeSymlink:
		if err := x.symlink(dest, h); err != nil {
			return err
		}
		x.result.Symlinks++
	case tar.TypeLink:
		if err := x.hardlink(dest, h); err != nil {
			return err
		}
		x.result.Hardlinks++
	default:
		// Device nodes, fifos and sockets are never written by this tool, so
		// meeting one means the archive is not what it claims. Materialising a
		// device node during a restore is exactly the kind of surprise a
		// backup tool must not deliver.
		x.warn(name, "unsupported member type "+kindOf(h)+", skipped")
		return nil
	}

	if x.opts.Progress != nil {
		x.progress.Member = name
		x.progress.Files = x.result.Files
		x.progress.Dirs = x.result.Dirs
		x.progress.Bytes = x.result.Bytes
		x.opts.Progress(x.progress)
	}
	return nil
}

// safePath resolves an archive member to a path inside the target, refusing
// every known way out.
func (x *extractor) safePath(member string) (string, error) {
	if member == "" {
		return "", errors.New("archive contains a member with an empty name")
	}
	if path.IsAbs(member) || filepath.IsAbs(member) {
		return "", fmt.Errorf("refusing to restore %q: absolute paths are not allowed in an archive", member)
	}
	clean := path.Clean(member)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("refusing to restore %q: the path climbs outside the restore target", member)
	}

	dest := filepath.Join(x.target, filepath.FromSlash(clean))
	if dest != x.target && !strings.HasPrefix(dest, x.target+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to restore %q: it resolves outside the restore target", member)
	}
	if err := x.ensureNoSymlinkedParent(clean); err != nil {
		return "", err
	}
	return dest, nil
}

// ensureNoSymlinkedParent blocks the classic tar attack: an earlier member
// plants a symlink pointing outside the target, and a later member is written
// "through" it. Archives written by this tool never nest members under a stored
// symlink, so refusing to traverse one costs nothing.
func (x *extractor) ensureNoSymlinkedParent(rel string) error {
	parts := strings.Split(rel, "/")
	cur := x.target
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // nothing below exists yet
		}
		if err != nil {
			return fmt.Errorf("checking %s: %w", cur, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf(
				"refusing to restore through the symlink %s: an archive that writes through a symlink "+
					"can plant files anywhere on the filesystem", cur)
		}
	}
	return nil
}

func (x *extractor) dir(dest string, h *tar.Header) error {
	if x.opts.DryRun {
		return nil
	}
	// Created permissively for now; the recorded mode is applied once the
	// directory's children are in place.
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return fmt.Errorf("creating directory %s: %w", dest, err)
	}
	x.deferred = append(x.deferred, deferredDir{path: dest, hdr: h})
	return nil
}

func (x *extractor) file(dest string, h *tar.Header, body io.Reader) error {
	if err := x.prepareLeaf(dest); err != nil {
		return err
	}
	if x.opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("creating parent of %s: %w", dest, err)
	}

	// O_EXCL after the explicit removal above: never follow an existing
	// symlink at the leaf, and never race with something else creating it.
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dest, err)
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", dest, err)
	}
	return x.applyMetadata(dest, h)
}

func (x *extractor) symlink(dest string, h *tar.Header) error {
	if err := x.prepareLeaf(dest); err != nil {
		return err
	}
	if x.opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("creating parent of %s: %w", dest, err)
	}
	if err := os.Symlink(h.Linkname, dest); err != nil {
		return fmt.Errorf("creating symlink %s: %w", dest, err)
	}
	// A symlink's own mode is not meaningful; only ownership can be restored.
	return x.chown(dest, h)
}

func (x *extractor) hardlink(dest string, h *tar.Header) error {
	// The link target is itself an archive member, so it gets the same checks.
	source, err := x.safePath(h.Linkname)
	if err != nil {
		return fmt.Errorf("hardlink %s: %w", memberName(h), err)
	}
	if err := x.prepareLeaf(dest); err != nil {
		return err
	}
	if x.opts.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("creating parent of %s: %w", dest, err)
	}
	if err := os.Link(source, dest); err != nil {
		return fmt.Errorf("linking %s to %s: %w", dest, source, err)
	}
	return nil
}

// prepareLeaf enforces the overwrite policy and clears the way for an O_EXCL
// create, so an existing symlink is replaced rather than written through.
func (x *extractor) prepareLeaf(dest string) error {
	info, err := os.Lstat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking %s: %w", dest, err)
	}
	if !x.opts.Overwrite {
		return fmt.Errorf(
			"%s already exists: refusing to overwrite it. Restore into an empty directory, "+
				"or pass --overwrite if you mean to replace what is there", dest)
	}
	if x.opts.DryRun {
		return nil
	}
	if info.IsDir() {
		return fmt.Errorf("%s exists and is a directory, but the archive holds a file there", dest)
	}
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("replacing %s: %w", dest, err)
	}
	return nil
}

// applyMetadata restores the recorded permissions explicitly, so the process
// umask cannot loosen or tighten them. An ssh private key restored as 0644 is
// silently ignored by ssh, which is the kind of failure this prevents.
func (x *extractor) applyMetadata(dest string, h *tar.Header) error {
	mode := h.FileInfo().Mode().Perm()
	if err := os.Chmod(dest, mode); err != nil {
		return fmt.Errorf("setting mode on %s: %w", dest, err)
	}
	if err := x.chown(dest, h); err != nil {
		return err
	}
	if !h.ModTime.IsZero() {
		if err := os.Chtimes(dest, h.ModTime, h.ModTime); err != nil {
			x.warn(dest, "could not restore timestamps: "+err.Error())
		}
	}
	return nil
}

// chown only acts as root. Restoring someone else's uid as an unprivileged user
// cannot work, and quietly trying would just produce noise.
func (x *extractor) chown(dest string, h *tar.Header) error {
	if !x.opts.PreserveOwner || os.Geteuid() != 0 {
		return nil
	}
	if err := os.Lchown(dest, h.Uid, h.Gid); err != nil {
		x.warn(dest, "could not restore ownership: "+err.Error())
	}
	return nil
}

// applyDeferredDirs sets directory modes and timestamps from the deepest path
// upwards, once nothing more will be written into them.
func (x *extractor) applyDeferredDirs() error {
	if x.opts.DryRun {
		return nil
	}
	sort.Slice(x.deferred, func(i, j int) bool {
		return len(x.deferred[i].path) > len(x.deferred[j].path)
	})
	for _, d := range x.deferred {
		if err := x.applyMetadata(d.path, d.hdr); err != nil {
			return err
		}
	}
	return nil
}
