package archive_test

import (
	"archive/tar"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hamsa/arca/internal/archive"
	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/manifest"
)

func keyring() *codec.Keyring { return &codec.Keyring{Passphrase: []byte(testPass)} }

// craftArchive builds an archive member by member, bypassing the writer's own
// safety checks. It is how a hostile archive is simulated.
func craftArchive(t *testing.T, path string, build func(*tar.Writer)) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	cipher, _ := codec.GetCipher("age")
	enc, err := cipher.Seal(f, keyring())
	if err != nil {
		t.Fatal(err)
	}
	comp, _ := codec.GetCompressor("zstd")
	zw, err := comp.NewWriter(enc, codec.CompressOpts{Level: codec.LevelFastest})
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)

	mf, _ := manifest.Build("test", &config.Config{
		Settings: config.Default().Settings,
		Groups: []config.Group{{
			Name: "g", Dest: "", Sources: []config.Source{{Path: "/tmp/x"}},
		}},
	}, "", nil)
	data, _ := manifest.Marshal(mf)
	if err := tw.WriteHeader(&tar.Header{
		Name: manifest.Name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
	}); err != nil {
		t.Fatal(err)
	}
	tw.Write(data)

	build(tw)

	for _, c := range []func() error{tw.Close, zw.Close, enc.Close} {
		if err := c(); err != nil {
			t.Fatal(err)
		}
	}
}

func addFile(t *testing.T, tw *tar.Writer, name, content string, mode int64) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg, Format: tar.FormatPAX,
	}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(content))
}

func addSymlink(t *testing.T, tw *tar.Writer, name, target string) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Linkname: target, Typeflag: tar.TypeSymlink, Mode: 0o777, Format: tar.FormatPAX,
	}); err != nil {
		t.Fatal(err)
	}
}

// sampleArchive produces a normal archive plus the fixture that built it.
func sampleArchive(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	f.write(".ssh/id_ed25519", "PRIVATE KEY", 0o600)
	f.write(".ssh/config", "Host example", 0o644)
	f.write(".aws/credentials", "CREDS", 0o600)
	f.write("Downloads/photo.jpg", "image", 0o644)
	if err := os.Symlink("config", filepath.Join(f.home, ".ssh/alias")); err != nil {
		t.Fatal(err)
	}

	cfg := f.cfg(
		f.group("dotfiles", "dotfiles", ".ssh", ".aws"),
		f.group("downloads", "Downloads", "Downloads"),
	)
	out := f.out("b.tar.zst.age")
	mustBackup(t, cfg, out)
	return f, out
}

// ---------- automatic detection ----------

// The user should need the key, not the algorithm, and not the file name.
func TestDetectionWorksOnARenamedFile(t *testing.T) {
	_, out := sampleArchive(t)
	renamed := filepath.Join(filepath.Dir(out), "no-extension-at-all")
	if err := os.Rename(out, renamed); err != nil {
		t.Fatal(err)
	}

	info, err := archive.Inspect(renamed, keyring())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if info.Manifest.Pipeline.Compressor != "zstd" || info.Manifest.Pipeline.Cipher != "age" {
		t.Errorf("pipeline = %+v", info.Manifest.Pipeline)
	}
	if info.Cipher != "age" || info.Compressor != "zstd" {
		t.Errorf("detected cipher=%q compressor=%q", info.Cipher, info.Compressor)
	}
}

func TestNonArchiveIsReportedClearly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "random.bin")
	if err := os.WriteFile(p, []byte("this is just a text file, not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := archive.Inspect(p, keyring())
	if err == nil {
		t.Fatal("expected an error for a non-archive file")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "encrypt") {
		t.Errorf("error should explain what was expected, got: %v", err)
	}
}

func TestWrongPassphraseIsReportedClearly(t *testing.T) {
	_, out := sampleArchive(t)
	_, err := archive.Inspect(out, &codec.Keyring{Passphrase: []byte("wrong-passphrase-entirely")})
	if err == nil {
		t.Fatal("expected failure with the wrong passphrase")
	}
}

// ---------- inspect and list ----------

func TestInspectReadsOnlyTheManifest(t *testing.T) {
	_, out := sampleArchive(t)
	info, err := archive.Inspect(out, keyring())
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(info.Manifest.Groups) != 2 {
		t.Errorf("groups = %d, want 2", len(info.Manifest.Groups))
	}
	if info.Manifest.Host.OS == "" {
		t.Error("host information missing from the manifest")
	}
}

func TestListEnumeratesMembersAndSummary(t *testing.T) {
	_, out := sampleArchive(t)
	res, err := archive.List(out, keyring())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Summary == nil {
		t.Fatal("summary missing: the archive should carry its completion record")
	}
	if res.Summary.Stats.Files != 4 {
		t.Errorf("summary files = %d, want 4", res.Summary.Stats.Files)
	}

	var names []string
	for _, e := range res.Entries {
		names = append(names, e.Name)
	}
	for _, want := range []string{"dotfiles/.ssh/id_ed25519", "dotfiles/.aws/credentials", "Downloads/Downloads/photo.jpg"} {
		if !contains(names, want) {
			t.Errorf("missing %q; got %v", want, names)
		}
	}
	// The tool's own documents are bookkeeping, not user content.
	for _, reserved := range []string{manifest.Name, manifest.SummaryName} {
		if contains(names, reserved) {
			t.Errorf("%q should not be listed as user content", reserved)
		}
	}
}

// ---------- verify ----------

func TestVerifyAcceptsAGoodArchive(t *testing.T) {
	_, out := sampleArchive(t)
	res, err := archive.Verify(out, keyring())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Complete {
		t.Error("a freshly written archive should be marked complete")
	}
	if res.Stats.Files != 4 {
		t.Errorf("files = %d, want 4", res.Stats.Files)
	}
}

func TestVerifyDetectsATamperedByte(t *testing.T) {
	_, out := sampleArchive(t)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-32] ^= 0x01
	if err := os.WriteFile(out, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Verify(out, keyring()); err == nil {
		t.Fatal("a tampered archive passed verification")
	}
}

// Dropping the external checksum was only safe because the cipher itself
// detects truncation; this proves it.
func TestVerifyDetectsTruncation(t *testing.T) {
	_, out := sampleArchive(t)
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data[:len(data)-100], 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Verify(out, keyring()); err == nil {
		t.Fatal("a truncated archive passed verification")
	}
}

// ---------- restore ----------

func restore(t *testing.T, out, target string, mut func(*archive.RestoreOptions)) (*archive.RestoreResult, error) {
	t.Helper()
	opts := archive.RestoreOptions{Path: out, Keyring: keyring(), Target: target}
	if mut != nil {
		mut(&opts)
	}
	return archive.Restore(opts)
}

func TestRestoreRebuildsTheMappedLayout(t *testing.T) {
	_, out := sampleArchive(t)
	target := t.TempDir()

	res, err := restore(t, out, target, nil)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Files != 4 {
		t.Errorf("restored %d files, want 4", res.Files)
	}

	for path, want := range map[string]string{
		"dotfiles/.ssh/id_ed25519":      "PRIVATE KEY",
		"dotfiles/.ssh/config":          "Host example",
		"dotfiles/.aws/credentials":     "CREDS",
		"Downloads/Downloads/photo.jpg": "image",
	} {
		got, err := os.ReadFile(filepath.Join(target, path))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

// ssh silently refuses keys with loose permissions, so this is the difference
// between a restore that works and one that appears to.
func TestRestoreReproducesModesRegardlessOfUmask(t *testing.T) {
	old := syscallUmask(0o077)
	defer syscallUmask(old)

	_, out := sampleArchive(t)
	target := t.TempDir()
	if _, err := restore(t, out, target, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for path, want := range map[string]fs.FileMode{
		"dotfiles/.ssh/id_ed25519": 0o600,
		"dotfiles/.ssh/config":     0o644,
	} {
		info, err := os.Lstat(filepath.Join(target, path))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %#o, want %#o", path, got, want)
		}
	}
}

func TestRestoreRecreatesSymlinks(t *testing.T) {
	_, out := sampleArchive(t)
	target := t.TempDir()
	if _, err := restore(t, out, target, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	link := filepath.Join(target, "dotfiles/.ssh/alias")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatal("alias was not restored as a symlink")
	}
	if got, _ := os.Readlink(link); got != "config" {
		t.Errorf("target = %q, want config", got)
	}
}

// A directory stored read-only must not block its own children.
func TestRestoreHandlesReadOnlyDirectories(t *testing.T) {
	f := newFixture(t)
	f.write("d/sub/file.txt", "content", 0o644)
	if err := os.Chmod(filepath.Join(f.home, "d/sub"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(f.home, "d/sub"), 0o755) })

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("g", "", "d")), out)

	target := t.TempDir()
	// Registered after t.TempDir, so it runs before TempDir's own cleanup,
	// which cannot delete through a 0500 directory.
	t.Cleanup(func() { os.Chmod(filepath.Join(target, "d/sub"), 0o755) })

	if _, err := restore(t, out, target, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "d/sub/file.txt")); err != nil || string(got) != "content" {
		t.Errorf("content = %q, err = %v", got, err)
	}
	info, err := os.Stat(filepath.Join(target, "d/sub"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o500 {
		t.Errorf("directory mode = %#o, want 0500 after restore completes", got)
	}
}

func TestRestoreGroupFilter(t *testing.T) {
	_, out := sampleArchive(t)
	target := t.TempDir()

	if _, err := restore(t, out, target, func(o *archive.RestoreOptions) {
		o.Groups = []string{"dotfiles"}
	}); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "dotfiles/.ssh/config")); err != nil {
		t.Errorf("requested group missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "Downloads")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a group that was not requested was restored")
	}
}

func TestRestoreUnknownGroupIsAnError(t *testing.T) {
	_, out := sampleArchive(t)
	_, err := restore(t, out, t.TempDir(), func(o *archive.RestoreOptions) {
		o.Groups = []string{"nonexistent"}
	})
	if err == nil {
		t.Fatal("expected an error naming the unknown group")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should name the group, got: %v", err)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	_, out := sampleArchive(t)
	target := t.TempDir()

	res, err := restore(t, out, target, func(o *archive.RestoreOptions) { o.DryRun = true })
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.Files != 4 {
		t.Errorf("dry run should still report %d files, got %d", 4, res.Files)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("dry run created %d entries in the target", len(entries))
	}
}

func TestRestoreRefusesToClobberWithoutOverwrite(t *testing.T) {
	_, out := sampleArchive(t)
	target := t.TempDir()
	existing := filepath.Join(target, "dotfiles/.ssh/config")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("MY CURRENT CONFIG"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := restore(t, out, target, nil); err == nil {
		t.Fatal("expected a refusal to overwrite an existing file")
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "MY CURRENT CONFIG" {
		t.Error("the existing file was overwritten anyway")
	}
}

func TestRestoreOverwriteWhenAsked(t *testing.T) {
	_, out := sampleArchive(t)
	target := t.TempDir()
	existing := filepath.Join(target, "dotfiles/.ssh/config")
	os.MkdirAll(filepath.Dir(existing), 0o755)
	os.WriteFile(existing, []byte("OLD"), 0o644)

	if _, err := restore(t, out, target, func(o *archive.RestoreOptions) { o.Overwrite = true }); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, _ := os.ReadFile(existing)
	if string(got) != "Host example" {
		t.Errorf("content = %q, want the archived version", got)
	}
}

func TestRestorePreservesModTimes(t *testing.T) {
	f := newFixture(t)
	p := f.write("d/a.txt", "x", 0o644)
	want := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(p, want, want); err != nil {
		t.Fatal(err)
	}

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("g", "", "d")), out)

	target := t.TempDir()
	if _, err := restore(t, out, target, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(target, "d/a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().UTC().Equal(want) {
		t.Errorf("mtime = %v, want %v", info.ModTime().UTC(), want)
	}
}

// ---------- extraction attacks ----------

// Zip Slip: a member whose path climbs out of the target.
func TestRestoreRejectsParentTraversal(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "evil.tar.zst.age")
	craftArchive(t, out, func(tw *tar.Writer) {
		addFile(t, tw, "../escaped.txt", "PWNED", 0o644)
	})

	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Restore(archive.RestoreOptions{Path: out, Keyring: keyring(), Target: target}); err == nil {
		t.Fatal("a traversing member was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a file was written outside the restore target")
	}
}

func TestRestoreRejectsAbsoluteMemberPaths(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "evil.tar.zst.age")
	victim := filepath.Join(dir, "victim.txt")
	craftArchive(t, out, func(tw *tar.Writer) {
		addFile(t, tw, victim, "PWNED", 0o644)
	})

	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0o755)
	if _, err := archive.Restore(archive.RestoreOptions{Path: out, Keyring: keyring(), Target: target}); err == nil {
		t.Fatal("an absolute member path was accepted")
	}
	if _, err := os.Stat(victim); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a file was written at an absolute path outside the target")
	}
}

// The classic tar attack: plant a symlink pointing outside, then write
// "through" it with a later member.
func TestRestoreRejectsWritingThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "evil.tar.zst.age")
	craftArchive(t, out, func(tw *tar.Writer) {
		addSymlink(t, tw, "escape", outside)
		addFile(t, tw, "escape/planted.txt", "PWNED", 0o644)
	})

	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0o755)
	_, err := archive.Restore(archive.RestoreOptions{Path: out, Keyring: keyring(), Target: target})
	if err == nil {
		t.Fatal("writing through a symlinked parent was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "planted.txt")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatal("a file was planted outside the restore target through a symlink")
	}
}

func TestRestoreRejectsHardlinkEscape(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "evil.tar.zst.age")
	craftArchive(t, out, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{
			Name: "stolen.txt", Linkname: "../secret.txt", Typeflag: tar.TypeLink, Format: tar.FormatPAX,
		}); err != nil {
			t.Fatal(err)
		}
	})

	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0o755)
	if _, err := archive.Restore(archive.RestoreOptions{Path: out, Keyring: keyring(), Target: target}); err == nil {
		t.Fatal("a hardlink pointing outside the target was accepted")
	}
}

// Special file types have no place in this archive format and must not be
// materialised on restore.
func TestRestoreRejectsDeviceNodes(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "evil.tar.zst.age")
	craftArchive(t, out, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{
			Name: "hda", Typeflag: tar.TypeBlock, Mode: 0o666, Devmajor: 8, Devminor: 0, Format: tar.FormatPAX,
		}); err != nil {
			t.Fatal(err)
		}
	})

	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0o755)
	res, err := archive.Restore(archive.RestoreOptions{Path: out, Keyring: keyring(), Target: target})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(res.Warnings) == 0 {
		t.Error("skipping a device node should be reported")
	}
	if _, statErr := os.Stat(filepath.Join(target, "hda")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Error("a device node was created")
	}
}

func TestRestoreCreatesTargetDirectory(t *testing.T) {
	_, out := sampleArchive(t)
	target := filepath.Join(t.TempDir(), "deep", "new", "target")

	if _, err := restore(t, out, target, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "dotfiles/.ssh/config")); err != nil {
		t.Errorf("restore did not create its target: %v", err)
	}
}

func TestRestoreReportsProgress(t *testing.T) {
	_, out := sampleArchive(t)
	n := 0
	if _, err := restore(t, out, t.TempDir(), func(o *archive.RestoreOptions) {
		o.Progress = func(archive.Progress) { n++ }
	}); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("restore never reported progress")
	}
}

// ---------- streaming discipline ----------

// Reading must not depend on seeking, so the same code path works on a pipe.
func TestReaderIsPurelySequential(t *testing.T) {
	_, out := sampleArchive(t)
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	r, err := archive.NewReader(sequentialOnly{f}, keyring())
	if err != nil {
		t.Fatalf("NewReader on a non-seekable stream: %v", err)
	}
	defer r.Close()

	if r.Manifest() == nil {
		t.Fatal("manifest not read")
	}
	count := 0
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if h.Name != "" {
			count++
		}
	}
	if count == 0 {
		t.Error("no members read from the sequential stream")
	}
}

// sequentialOnly hides any Seek/ReadAt the underlying file may offer.
type sequentialOnly struct{ r io.Reader }

func (s sequentialOnly) Read(p []byte) (int, error) { return s.r.Read(p) }
