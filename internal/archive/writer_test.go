package archive_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/codec"
	"github.com/henriquemenezes/arca/internal/config"
	"github.com/henriquemenezes/arca/internal/manifest"
)

const testPass = "correct-horse-battery-staple-arca"

// ---------- fixtures ----------

type fixture struct {
	t      *testing.T
	home   string
	outDir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, home: t.TempDir(), outDir: t.TempDir()}
	return f
}

func (f *fixture) write(rel, content string, mode fs.FileMode) string {
	f.t.Helper()
	p := filepath.Join(f.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) cfg(groups ...config.Group) *config.Config {
	c := config.Default()
	c.Groups = groups
	return c
}

func (f *fixture) out(name string) string { return filepath.Join(f.outDir, name) }

func (f *fixture) group(name, dest string, rels ...string) config.Group {
	g := config.Group{Name: name, Dest: dest}
	for _, r := range rels {
		g.Sources = append(g.Sources, config.Source{Path: filepath.Join(f.home, r)})
	}
	return g
}

func backup(t *testing.T, cfg *config.Config, out string) (*archive.BackupResult, error) {
	t.Helper()
	return archive.Backup(archive.BackupOptions{
		Config:      cfg,
		ConfigTOML:  "# test\n",
		Keyring:     &codec.Keyring{Passphrase: []byte(testPass)},
		OutputPath:  out,
		ToolVersion: "test",
	})
}

func mustBackup(t *testing.T, cfg *config.Config, out string) *archive.BackupResult {
	t.Helper()
	res, err := backup(t, cfg, out)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	return res
}

// ---------- independent read-back ----------

type member struct {
	header  *tar.Header
	content []byte
}

// readBack decrypts and untars using the codec layer directly rather than the
// archive reader, so these tests validate the on-disk format itself.
func readBack(t *testing.T, path string) ([]member, []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	cipher, _ := codec.GetCipher("age")
	plain, err := cipher.Open(f, &codec.Keyring{Passphrase: []byte(testPass)})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	defer plain.Close()

	comp, _ := codec.GetCompressor("zstd")
	dec, err := comp.NewReader(plain)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	defer dec.Close()

	var members []member
	var names []string
	tr := tar.NewReader(dec)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read member %q: %v", h.Name, err)
		}
		members = append(members, member{header: h, content: data})
		names = append(names, strings.TrimSuffix(h.Name, "/"))
	}
	return members, names
}

func findMember(t *testing.T, ms []member, name string) member {
	t.Helper()
	for _, m := range ms {
		if strings.TrimSuffix(m.header.Name, "/") == name {
			return m
		}
	}
	var have []string
	for _, m := range ms {
		have = append(have, m.header.Name)
	}
	t.Fatalf("member %q not found; archive holds %v", name, have)
	return member{}
}

// ---------- round trip ----------

func TestArchivePreservesContentModesAndMapping(t *testing.T) {
	f := newFixture(t)
	f.write(".ssh/id_ed25519", "PRIVATE KEY", 0o600)
	f.write(".ssh/config", "Host example", 0o644)
	f.write(".aws/credentials", "CREDS", 0o600)

	cfg := f.cfg(f.group("dotfiles", "dotfiles", ".ssh", ".aws"))
	out := f.out("backup.tar.zst.age")
	mustBackup(t, cfg, out)

	ms, names := readBack(t, out)

	for _, want := range []string{
		manifest.Name,
		"dotfiles", "dotfiles/.ssh", "dotfiles/.ssh/config",
		"dotfiles/.ssh/id_ed25519", "dotfiles/.aws", "dotfiles/.aws/credentials",
		manifest.SummaryName,
	} {
		if !contains(names, want) {
			t.Errorf("missing %q; archive holds %v", want, names)
		}
	}

	key := findMember(t, ms, "dotfiles/.ssh/id_ed25519")
	if string(key.content) != "PRIVATE KEY" {
		t.Errorf("content = %q", key.content)
	}
	// An ssh key restored with loose permissions is silently rejected by ssh,
	// so the mode has to survive the round trip.
	if got := fs.FileMode(key.header.Mode).Perm(); got != 0o600 {
		t.Errorf("mode = %#o, want 0600", got)
	}
	if got := fs.FileMode(findMember(t, ms, "dotfiles/.ssh/config").header.Mode).Perm(); got != 0o644 {
		t.Errorf("config mode = %#o, want 0644", got)
	}
}

func TestArchivePreservesSymlinksAndHardlinks(t *testing.T) {
	f := newFixture(t)
	f.write("d/real.txt", "payload", 0o644)
	if err := os.Symlink("real.txt", filepath.Join(f.home, "d/link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(f.home, "d/real.txt"), filepath.Join(f.home, "d/twin.txt")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("g", "", "d")), out)
	ms, _ := readBack(t, out)

	link := findMember(t, ms, "d/link.txt")
	if link.header.Typeflag != tar.TypeSymlink {
		t.Errorf("typeflag = %v, want symlink", link.header.Typeflag)
	}
	if link.header.Linkname != "real.txt" {
		t.Errorf("linkname = %q", link.header.Linkname)
	}

	twin := findMember(t, ms, "d/twin.txt")
	if twin.header.Typeflag != tar.TypeLink {
		t.Errorf("hardlink typeflag = %v, want link", twin.header.Typeflag)
	}
	if twin.header.Linkname != "d/real.txt" {
		t.Errorf("hardlink target = %q, want d/real.txt", twin.header.Linkname)
	}
	if len(twin.content) != 0 {
		t.Errorf("hardlink should not duplicate content, got %d bytes", len(twin.content))
	}
}

// Sub-second timestamps need PAX; the v7/USTAR formats would silently round.
func TestArchiveUsesPAXSoTimestampsSurvive(t *testing.T) {
	f := newFixture(t)
	p := f.write("d/a.txt", "x", 0o644)
	info, _ := os.Stat(p)

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("g", "", "d")), out)
	ms, _ := readBack(t, out)

	got := findMember(t, ms, "d/a.txt").header.ModTime
	if !got.Equal(info.ModTime()) {
		t.Errorf("mtime = %v, want %v (sub-second precision lost)", got, info.ModTime())
	}
}

// ---------- manifest and summary placement ----------

func TestManifestIsFirstAndSummaryIsLast(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "x", 0o644)
	f.write("d/b.txt", "y", 0o644)

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("g", "", "d")), out)
	_, names := readBack(t, out)

	if names[0] != manifest.Name {
		t.Errorf("first member = %q, want %q", names[0], manifest.Name)
	}
	if last := names[len(names)-1]; last != manifest.SummaryName {
		t.Errorf("last member = %q, want %q", last, manifest.SummaryName)
	}
}

func TestManifestDescribesTheMappingAndPipeline(t *testing.T) {
	f := newFixture(t)
	f.write(".ssh/config", "x", 0o600)

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("dotfiles", "dotfiles", ".ssh")), out)
	ms, _ := readBack(t, out)

	m, err := manifest.ParseManifest(findMember(t, ms, manifest.Name).content)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Pipeline.Compressor != "zstd" || m.Pipeline.Cipher != "age" {
		t.Errorf("pipeline = %+v", m.Pipeline)
	}
	if m.Pipeline.EncryptionMode != "passphrase" {
		t.Errorf("encryption mode = %q, want passphrase", m.Pipeline.EncryptionMode)
	}
	if len(m.Groups) != 1 || len(m.Groups[0].Sources) != 1 {
		t.Fatalf("groups = %+v", m.Groups)
	}
	src := m.Groups[0].Sources[0]
	if src.Member != "dotfiles/.ssh" || !strings.HasSuffix(src.Path, "/.ssh") {
		t.Errorf("source mapping = %+v", src)
	}
	if m.Config != "# test\n" {
		t.Errorf("config not stored verbatim: %q", m.Config)
	}
}

func TestSummaryRecordsStatsAndWarnings(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "12345", 0o644)

	cfg := f.cfg(f.group("g", "", "d"))
	cfg.Groups = append(cfg.Groups, config.Group{
		Name:    "missing",
		Dest:    "gone",
		Sources: []config.Source{{Path: filepath.Join(f.home, "not-there")}},
	})

	out := f.out("b.tar.zst.age")
	res := mustBackup(t, cfg, out)
	if len(res.Warnings) == 0 {
		t.Fatal("expected a warning for the missing source")
	}

	ms, _ := readBack(t, out)
	s, err := manifest.ParseSummary(findMember(t, ms, manifest.SummaryName).content)
	if err != nil {
		t.Fatalf("ParseSummary: %v", err)
	}
	if s.Stats.Files != 1 || s.Stats.Bytes != 5 {
		t.Errorf("stats = %+v, want 1 file / 5 bytes", s.Stats)
	}
	if len(s.Warnings) == 0 {
		t.Error("warnings did not reach the summary")
	}
}

// ---------- file handling safety ----------

func TestOutputIsPrivateAndLeavesNoPartial(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "x", 0o644)

	out := f.out("b.tar.zst.age")
	mustBackup(t, f.cfg(f.group("g", "", "d")), out)

	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("archive mode = %#o, want 0600", got)
	}
	if _, err := os.Stat(out + archive.PartialSuffix); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a .partial file survived a successful backup")
	}
}

func TestExistingArchiveIsNeverOverwritten(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "x", 0o644)
	out := f.out("b.tar.zst.age")
	if err := os.WriteFile(out, []byte("PRECIOUS OLD BACKUP"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := backup(t, f.cfg(f.group("g", "", "d")), out); err == nil {
		t.Fatal("expected a refusal to overwrite an existing archive")
	}
	data, _ := os.ReadFile(out)
	if string(data) != "PRECIOUS OLD BACKUP" {
		t.Error("the existing archive was destroyed")
	}
}

func TestStalePartialIsReportedNotClobbered(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "x", 0o644)
	out := f.out("b.tar.zst.age")
	if err := os.WriteFile(out+archive.PartialSuffix, []byte("half written"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := backup(t, f.cfg(f.group("g", "", "d")), out)
	if err == nil {
		t.Fatal("expected a refusal while a .partial exists")
	}
	if !strings.Contains(err.Error(), archive.PartialSuffix) {
		t.Errorf("error should name the stale file, got: %v", err)
	}
}

// Writing the archive into a directory being backed up must not make it eat
// itself.
func TestArchiveDoesNotSwallowItself(t *testing.T) {
	f := newFixture(t)
	f.write("Downloads/photo.jpg", "image data", 0o644)
	out := filepath.Join(f.home, "Downloads", "backup.tar.zst.age")

	mustBackup(t, f.cfg(f.group("dl", "Downloads", "Downloads")), out)
	_, names := readBack(t, out)

	for _, n := range names {
		if strings.Contains(n, "backup.tar.zst.age") {
			t.Errorf("archive included itself as %q", n)
		}
	}
	if !contains(names, "Downloads/Downloads/photo.jpg") {
		t.Errorf("real content lost; archive holds %v", names)
	}
}

// A keyring the cipher rejects must fail before any file appears on disk.
func TestInvalidKeyringFailsBeforeTouchingDisk(t *testing.T) {
	f := newFixture(t)
	f.write("d/a.txt", "x", 0o644)
	out := f.out("b.tar.zst.age")

	_, err := archive.Backup(archive.BackupOptions{
		Config:     f.cfg(f.group("g", "", "d")),
		Keyring:    &codec.Keyring{Passphrase: []byte("p"), Recipients: []string{"age1xyz"}},
		OutputPath: out,
	})
	if err == nil {
		t.Fatal("expected the mixed-mode keyring to be rejected")
	}
	for _, p := range []string{out, out + archive.PartialSuffix} {
		if _, statErr := os.Stat(p); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("%s was created despite the failure", p)
		}
	}
}

func TestNoSourcesLeavesNoArchiveBehind(t *testing.T) {
	f := newFixture(t)
	out := f.out("b.tar.zst.age")

	_, err := backup(t, f.cfg(f.group("g", "", "does-not-exist")), out)
	if err == nil {
		t.Fatal("expected an error when nothing exists to back up")
	}
	for _, p := range []string{out, out + archive.PartialSuffix} {
		if _, statErr := os.Stat(p); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("%s survived a failed backup", p)
		}
	}
}

// ---------- progress ----------

func TestProgressReportsGrowingCounts(t *testing.T) {
	f := newFixture(t)
	for _, n := range []string{"a", "b", "c"} {
		f.write("d/"+n, strings.Repeat(n, 100), 0o644)
	}

	var last archive.Progress
	updates := 0
	_, err := archive.Backup(archive.BackupOptions{
		Config:     f.cfg(f.group("g", "", "d")),
		Keyring:    &codec.Keyring{Passphrase: []byte(testPass)},
		OutputPath: f.out("b.tar.zst.age"),
		Progress: func(p archive.Progress) {
			updates++
			if p.Bytes < last.Bytes {
				t.Errorf("byte count went backwards: %d then %d", last.Bytes, p.Bytes)
			}
			last = p
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updates == 0 {
		t.Fatal("progress callback was never invoked")
	}
	if last.Bytes != 300 {
		t.Errorf("final byte count = %d, want 300", last.Bytes)
	}
	if last.Files != 3 {
		t.Errorf("final file count = %d, want 3", last.Files)
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// ---------- content that changes while being read ----------

type shortReader struct{ data []byte }

func (r *shortReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// A file shrinking mid-read would desync the tar stream; the declared size must
// be honoured by padding.
func TestCopyExactlyPadsAShrunkSource(t *testing.T) {
	var buf bytes.Buffer
	copied, grew, err := archive.CopyExactly(&buf, &shortReader{data: []byte("abc")}, 10)
	if err != nil {
		t.Fatalf("CopyExactly: %v", err)
	}
	if copied != 3 {
		t.Errorf("copied = %d, want 3", copied)
	}
	if grew {
		t.Error("grew should be false for a shrunk source")
	}
	if buf.Len() != 10 {
		t.Fatalf("wrote %d bytes, want exactly the declared 10", buf.Len())
	}
	if !bytes.Equal(buf.Bytes(), append([]byte("abc"), make([]byte, 7)...)) {
		t.Errorf("padding = % x", buf.Bytes())
	}
}

// A file growing mid-read must be truncated, not overflow the declared size.
func TestCopyExactlyTruncatesAGrownSource(t *testing.T) {
	var buf bytes.Buffer
	copied, grew, err := archive.CopyExactly(&buf, &shortReader{data: []byte("abcdefghij")}, 4)
	if err != nil {
		t.Fatalf("CopyExactly: %v", err)
	}
	if copied != 4 || buf.Len() != 4 {
		t.Errorf("copied = %d, wrote %d, want 4 and 4", copied, buf.Len())
	}
	if !grew {
		t.Error("grew should be true when the source had more to give")
	}
}

func TestCopyExactlyExactSource(t *testing.T) {
	var buf bytes.Buffer
	copied, grew, err := archive.CopyExactly(&buf, &shortReader{data: []byte("abcd")}, 4)
	if err != nil || copied != 4 || grew || buf.Len() != 4 {
		t.Errorf("copied=%d grew=%v len=%d err=%v", copied, grew, buf.Len(), err)
	}
}

func TestCopyExactlyZeroSize(t *testing.T) {
	var buf bytes.Buffer
	copied, grew, err := archive.CopyExactly(&buf, &shortReader{}, 0)
	if err != nil || copied != 0 || grew || buf.Len() != 0 {
		t.Errorf("copied=%d grew=%v len=%d err=%v", copied, grew, buf.Len(), err)
	}
}

func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
