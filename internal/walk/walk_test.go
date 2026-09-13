package walk_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/walk"
)

// ---------- helpers ----------

type tree struct {
	t    *testing.T
	root string
}

func newTree(t *testing.T) *tree { return &tree{t: t, root: t.TempDir()} }

func (tr *tree) file(rel, content string) string {
	tr.t.Helper()
	p := filepath.Join(tr.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		tr.t.Fatal(err)
	}
	return p
}

func (tr *tree) dir(rel string) string {
	tr.t.Helper()
	p := filepath.Join(tr.root, rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		tr.t.Fatal(err)
	}
	return p
}

func (tr *tree) path(rel string) string { return filepath.Join(tr.root, rel) }

func (tr *tree) source(rel, member string, exclude ...string) config.ResolvedSource {
	return config.ResolvedSource{
		Group:   "g",
		Path:    tr.path(rel),
		Member:  member,
		Exclude: exclude,
	}
}

// collect runs the walk and returns the archive members it produced.
func collect(t *testing.T, srcs []config.ResolvedSource, opts walk.Options) ([]walk.Entry, *walk.Result) {
	t.Helper()
	var entries []walk.Entry
	res, err := walk.Walk(srcs, opts, func(e walk.Entry) error {
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return entries, res
}

func members(entries []walk.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Member)
	}
	sort.Strings(out)
	return out
}

func hasMember(entries []walk.Entry, member string) bool {
	for _, e := range entries {
		if e.Member == member {
			return true
		}
	}
	return false
}

func find(t *testing.T, entries []walk.Entry, member string) walk.Entry {
	t.Helper()
	for _, e := range entries {
		if e.Member == member {
			return e
		}
	}
	t.Fatalf("member %q not found in %v", member, members(entries))
	return walk.Entry{}
}

// ---------- mapping ----------

func TestWalkAppliesArchiveMapping(t *testing.T) {
	tr := newTree(t)
	tr.file(".ssh/id_ed25519", "KEY")
	tr.file(".ssh/config", "Host x")
	tr.file(".aws/credentials", "CREDS")

	entries, res := collect(t, []config.ResolvedSource{
		tr.source(".ssh", "dotfiles/.ssh"),
		tr.source(".aws", "dotfiles/.aws"),
	}, walk.Options{})

	for _, want := range []string{
		"dotfiles",
		"dotfiles/.ssh",
		"dotfiles/.ssh/config",
		"dotfiles/.ssh/id_ed25519",
		"dotfiles/.aws",
		"dotfiles/.aws/credentials",
	} {
		if !hasMember(entries, want) {
			t.Errorf("missing member %q; got %v", want, members(entries))
		}
	}
	if res.Files != 3 {
		t.Errorf("Files = %d, want 3", res.Files)
	}
}

// The intermediate "dotfiles/" directory does not exist on disk; the archive
// still needs an entry for it so extraction gets sane permissions.
func TestSyntheticParentDirectoryIsEmittedOnceAndFirst(t *testing.T) {
	tr := newTree(t)
	tr.file(".ssh/config", "a")
	tr.file(".aws/credentials", "b")

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source(".ssh", "dotfiles/.ssh"),
		tr.source(".aws", "dotfiles/.aws"),
	}, walk.Options{})

	var idx []int
	for i, e := range entries {
		if e.Member == "dotfiles" {
			idx = append(idx, i)
		}
	}
	if len(idx) != 1 {
		t.Fatalf("expected exactly one synthetic parent entry, got %d", len(idx))
	}
	if idx[0] != 0 {
		t.Errorf("parent directory must precede its children, found at index %d", idx[0])
	}
	if e := entries[idx[0]]; e.Kind != walk.KindDir {
		t.Errorf("synthetic parent kind = %v, want dir", e.Kind)
	}
}

func TestSingleFileSourceIsSupported(t *testing.T) {
	tr := newTree(t)
	tr.file("notes.txt", "hello")

	entries, res := collect(t, []config.ResolvedSource{
		tr.source("notes.txt", "docs/notes.txt"),
	}, walk.Options{})

	if !hasMember(entries, "docs/notes.txt") {
		t.Errorf("got %v", members(entries))
	}
	if res.Files != 1 {
		t.Errorf("Files = %d, want 1", res.Files)
	}
}

func TestDeterministicOrdering(t *testing.T) {
	tr := newTree(t)
	for _, n := range []string{"c", "a", "b", "d"} {
		tr.file("dir/"+n, n)
	}
	src := []config.ResolvedSource{tr.source("dir", "dir")}

	first, _ := collect(t, src, walk.Options{})
	second, _ := collect(t, src, walk.Options{})

	if fmt.Sprint(members(first)) != fmt.Sprint(members(second)) {
		t.Error("walk order is not deterministic")
	}
	var names []string
	for _, e := range first {
		if e.Kind == walk.KindRegular {
			names = append(names, filepath.Base(e.Member))
		}
	}
	if got := strings.Join(names, ","); got != "a,b,c,d" {
		t.Errorf("entries not sorted: %s", got)
	}
}

// ---------- excludes ----------

func TestExcludePatterns(t *testing.T) {
	tr := newTree(t)
	tr.file("Work/app/main.go", "code")
	tr.file("Work/app/node_modules/left-pad/index.js", "junk")
	tr.file("Work/app/target/debug/bin", "junk")
	tr.file("Work/big.iso", "junk")
	tr.file("Work/keep.txt", "keep")

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("Work", "Work", "**/node_modules", "**/target", "*.iso"),
	}, walk.Options{})

	for _, gone := range []string{
		"Work/app/node_modules",
		"Work/app/node_modules/left-pad/index.js",
		"Work/app/target",
		"Work/big.iso",
	} {
		if hasMember(entries, gone) {
			t.Errorf("%q should have been excluded", gone)
		}
	}
	for _, kept := range []string{"Work/app/main.go", "Work/keep.txt"} {
		if !hasMember(entries, kept) {
			t.Errorf("%q should have been kept; got %v", kept, members(entries))
		}
	}
}

// A pattern without a separator matches a basename anywhere, as in gitignore.
func TestBasenamePatternMatchesAtAnyDepth(t *testing.T) {
	tr := newTree(t)
	tr.file("a/b/c/.DS_Store", "junk")
	tr.file("a/.DS_Store", "junk")
	tr.file("a/b/real.txt", "keep")

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("a", "a", ".DS_Store"),
	}, walk.Options{})

	for _, e := range entries {
		if filepath.Base(e.Member) == ".DS_Store" {
			t.Errorf("%q should have been excluded", e.Member)
		}
	}
	if !hasMember(entries, "a/b/real.txt") {
		t.Error("real file was lost")
	}
}

// Pruning matters for performance: an excluded directory must not be descended.
func TestExcludedDirectoryIsNotDescended(t *testing.T) {
	tr := newTree(t)
	tr.file("Work/node_modules/deep/deeper/file.js", "junk")
	tr.file("Work/ok.txt", "keep")

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("Work", "Work", "**/node_modules"),
	}, walk.Options{})

	for _, e := range entries {
		if strings.Contains(e.Member, "node_modules") {
			t.Errorf("descended into an excluded directory: %q", e.Member)
		}
	}
}

// The archive must never swallow itself.
func TestSkipPathsExcludesTheOutputFile(t *testing.T) {
	tr := newTree(t)
	tr.file("Downloads/photo.jpg", "img")
	out := tr.file("Downloads/backup.tar.zst.age", "PARTIAL")

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("Downloads", "Downloads"),
	}, walk.Options{Skip: map[string]bool{out: true}})

	if hasMember(entries, "Downloads/backup.tar.zst.age") {
		t.Error("the backup file included itself")
	}
	if !hasMember(entries, "Downloads/photo.jpg") {
		t.Error("real file was lost")
	}
}

// ---------- symlinks ----------

func TestSymlinkStoredAsLinkByDefault(t *testing.T) {
	tr := newTree(t)
	tr.file("real/target.txt", "content")
	if err := os.Symlink(tr.path("real/target.txt"), tr.path("real/link.txt")); err != nil {
		t.Fatal(err)
	}

	entries, res := collect(t, []config.ResolvedSource{
		tr.source("real", "real"),
	}, walk.Options{})

	e := find(t, entries, "real/link.txt")
	if e.Kind != walk.KindSymlink {
		t.Errorf("kind = %v, want symlink", e.Kind)
	}
	if e.LinkTarget != tr.path("real/target.txt") {
		t.Errorf("target = %q", e.LinkTarget)
	}
	if res.Symlinks != 1 {
		t.Errorf("Symlinks = %d, want 1", res.Symlinks)
	}
}

func TestFollowSymlinksStoresContent(t *testing.T) {
	tr := newTree(t)
	tr.file("real/target.txt", "content")
	tr.dir("out")
	if err := os.Symlink(tr.path("real/target.txt"), tr.path("out/link.txt")); err != nil {
		t.Fatal(err)
	}

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("out", "out"),
	}, walk.Options{FollowSymlinks: true})

	e := find(t, entries, "out/link.txt")
	if e.Kind != walk.KindRegular {
		t.Errorf("kind = %v, want regular when following symlinks", e.Kind)
	}
}

// Following symlinks must not hang on a cycle.
func TestSymlinkLoopIsNotFollowedForever(t *testing.T) {
	tr := newTree(t)
	tr.dir("loop")
	if err := os.Symlink(tr.path("loop"), tr.path("loop/self")); err != nil {
		t.Fatal(err)
	}

	_, res := collect(t, []config.ResolvedSource{
		tr.source("loop", "loop"),
	}, walk.Options{FollowSymlinks: true})

	if len(res.Warnings) == 0 {
		t.Error("expected a warning about the symlink loop")
	}
}

// A dangling symlink is normal in dotfiles; it must be archived, not fatal.
func TestDanglingSymlinkIsArchived(t *testing.T) {
	tr := newTree(t)
	tr.dir("d")
	if err := os.Symlink("/nonexistent/target", tr.path("d/dangling")); err != nil {
		t.Fatal(err)
	}

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("d", "d"),
	}, walk.Options{})

	e := find(t, entries, "d/dangling")
	if e.Kind != walk.KindSymlink || e.LinkTarget != "/nonexistent/target" {
		t.Errorf("dangling symlink = %+v", e)
	}
}

// ---------- special files ----------

func TestFifoAndSocketAreSkippedWithWarnings(t *testing.T) {
	tr := newTree(t)
	tr.dir("ssh")
	tr.file("ssh/config", "Host x")

	fifo := tr.path("ssh/fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}
	sock := tr.path("ssh/agent.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("cannot create unix socket: %v", err)
	}
	defer l.Close()

	entries, res := collect(t, []config.ResolvedSource{
		tr.source("ssh", "dotfiles/.ssh"),
	}, walk.Options{})

	for _, gone := range []string{"dotfiles/.ssh/fifo", "dotfiles/.ssh/agent.sock"} {
		if hasMember(entries, gone) {
			t.Errorf("special file %q should have been skipped", gone)
		}
	}
	if !hasMember(entries, "dotfiles/.ssh/config") {
		t.Error("regular file was lost")
	}
	if len(res.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d: %v", len(res.Warnings), res.Warnings)
	}
}

// ---------- hardlinks ----------

func TestHardlinkIsRecordedAsLinkToTheFirstCopy(t *testing.T) {
	tr := newTree(t)
	tr.file("d/original.bin", "payload")
	if err := os.Link(tr.path("d/original.bin"), tr.path("d/twin.bin")); err != nil {
		t.Skipf("cannot create hardlink: %v", err)
	}

	entries, _ := collect(t, []config.ResolvedSource{
		tr.source("d", "d"),
	}, walk.Options{})

	first := find(t, entries, "d/original.bin")
	second := find(t, entries, "d/twin.bin")
	if first.Kind != walk.KindRegular {
		t.Errorf("first occurrence should be stored in full, got %v", first.Kind)
	}
	if second.Kind != walk.KindHardlink {
		t.Fatalf("second occurrence should be a hardlink, got %v", second.Kind)
	}
	if second.HardlinkTo != "d/original.bin" {
		t.Errorf("HardlinkTo = %q, want d/original.bin", second.HardlinkTo)
	}
}

func TestUnlinkedRegularFilesAreNotConfusedForHardlinks(t *testing.T) {
	tr := newTree(t)
	tr.file("d/a", "a")
	tr.file("d/b", "b")

	entries, _ := collect(t, []config.ResolvedSource{tr.source("d", "d")}, walk.Options{})
	for _, m := range []string{"d/a", "d/b"} {
		if find(t, entries, m).Kind != walk.KindRegular {
			t.Errorf("%q should be a plain regular file", m)
		}
	}
}

// ---------- missing and unreadable sources ----------

func TestMissingSourceWarnsAndContinues(t *testing.T) {
	tr := newTree(t)
	tr.file("present/file.txt", "x")

	entries, res := collect(t, []config.ResolvedSource{
		tr.source("present", "present"),
		tr.source("absent", "absent"),
	}, walk.Options{})

	if !hasMember(entries, "present/file.txt") {
		t.Error("present source was lost")
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0].Reason, "does not exist") {
		t.Errorf("expected one missing-source warning, got %v", res.Warnings)
	}
}

// Backing up nothing at all is a failure, not an empty archive.
func TestAllSourcesMissingIsAnError(t *testing.T) {
	tr := newTree(t)
	_, err := walk.Walk([]config.ResolvedSource{
		tr.source("nope", "nope"),
	}, walk.Options{}, func(walk.Entry) error { return nil })

	if err == nil {
		t.Fatal("expected an error when no source exists")
	}
}

func TestUnreadableDirectoryWarnsAndContinues(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any directory")
	}
	tr := newTree(t)
	tr.file("ok/file.txt", "x")
	locked := tr.dir("ok/locked")
	tr.file("ok/locked/secret.txt", "x")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	entries, res := collect(t, []config.ResolvedSource{
		tr.source("ok", "ok"),
	}, walk.Options{})

	if !hasMember(entries, "ok/file.txt") {
		t.Error("readable sibling was lost")
	}
	if len(res.Warnings) == 0 {
		t.Error("expected a warning for the unreadable directory")
	}
}

// ---------- one filesystem ----------

func TestOneFilesystemStopsAtAMountBoundary(t *testing.T) {
	other := "/dev/shm"
	oi, err := os.Stat(other)
	if err != nil {
		t.Skip("no /dev/shm on this machine")
	}
	tr := newTree(t)
	ri, err := os.Stat(tr.root)
	if err != nil {
		t.Fatal(err)
	}
	if oi.Sys().(*syscall.Stat_t).Dev == ri.Sys().(*syscall.Stat_t).Dev {
		t.Skip("/dev/shm is on the same filesystem as the temp dir")
	}

	tr.dir("d")
	tr.file("d/local.txt", "x")
	if err := os.Symlink(other, tr.path("d/elsewhere")); err != nil {
		t.Fatal(err)
	}

	entries, res := collect(t, []config.ResolvedSource{
		tr.source("d", "d"),
	}, walk.Options{FollowSymlinks: true, OneFilesystem: true})

	for _, e := range entries {
		if strings.HasPrefix(e.Member, "d/elsewhere/") {
			t.Errorf("crossed a filesystem boundary: %q", e.Member)
		}
	}
	if !hasMember(entries, "d/local.txt") {
		t.Error("same-filesystem file was lost")
	}
	if len(res.Warnings) == 0 {
		t.Error("crossing a mount boundary should be reported, not silent")
	}
}

// ---------- accounting ----------

func TestResultCountsAndBytes(t *testing.T) {
	tr := newTree(t)
	tr.file("d/a.txt", "12345")
	tr.file("d/sub/b.txt", "678")

	_, res := collect(t, []config.ResolvedSource{tr.source("d", "d")}, walk.Options{})

	if res.Files != 2 {
		t.Errorf("Files = %d, want 2", res.Files)
	}
	if res.Bytes != 8 {
		t.Errorf("Bytes = %d, want 8", res.Bytes)
	}
	if res.Dirs < 2 {
		t.Errorf("Dirs = %d, want at least 2 (d and d/sub)", res.Dirs)
	}
}

func TestVisitErrorAbortsTheWalk(t *testing.T) {
	tr := newTree(t)
	for _, n := range []string{"a", "b", "c"} {
		tr.file("d/"+n, n)
	}
	wantErr := fmt.Errorf("disk full")
	seen := 0
	_, err := walk.Walk([]config.ResolvedSource{tr.source("d", "d")}, walk.Options{}, func(walk.Entry) error {
		seen++
		if seen == 2 {
			return wantErr
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want the visitor error to propagate", err)
	}
}
