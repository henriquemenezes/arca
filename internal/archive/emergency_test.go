package archive_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/codec"
	"github.com/henriquemenezes/arca/internal/manifest"
)

// The central promise of this design is that an archive can be restored on a
// freshly installed machine with nothing but the stock age, zstd and tar
// binaries. If these tests pass, the tool is never a single point of failure
// for its own backups.
//
// Point ARCA_TEST_AGE at an age binary, or have one on PATH. `make check`
// fetches one into .tools/ and sets the variable.

func stockAge(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("ARCA_TEST_AGE"); p != "" {
		return p
	}
	p, err := exec.LookPath("age")
	if err != nil {
		t.Skip("no stock age binary: set ARCA_TEST_AGE or install age (see `make tools`)")
	}
	return p
}

func requireTool(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}
	return p
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// emergencyArchive builds a representative archive encrypted to an age identity
// file and returns the archive path and the identity path.
func emergencyArchive(t *testing.T) (archivePath, identityPath string) {
	t.Helper()

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	keyDir := t.TempDir()
	identityPath = filepath.Join(keyDir, "key.txt")
	if err := os.WriteFile(identityPath, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f := newFixture(t)
	f.write(".ssh/id_ed25519", "PRIVATE KEY MATERIAL", 0o600)
	f.write(".ssh/config", "Host example\n  User me\n", 0o644)
	f.write(".aws/credentials", "[default]\naws_access_key_id = AKIA\n", 0o600)
	f.write("Downloads/notes.md", strings.Repeat("compressible content\n", 500), 0o644)
	if err := os.Symlink("config", filepath.Join(f.home, ".ssh/alias")); err != nil {
		t.Fatal(err)
	}

	cfg := f.cfg(
		f.group("dotfiles", "dotfiles", ".ssh", ".aws"),
		f.group("downloads", "Downloads", "Downloads"),
	)
	archivePath = f.out("arca-emergency.tar.zst.age")

	if _, err := archive.Backup(archive.BackupOptions{
		Config:      cfg,
		ConfigTOML:  "# emergency test\n",
		Keyring:     &codec.Keyring{Recipients: []string{id.Recipient().String()}},
		OutputPath:  archivePath,
		ToolVersion: "test",
	}); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	return archivePath, identityPath
}

// TestEmergencyRestoreWithStockUnixTools is the acceptance criterion for the
// whole project: `age -d | zstd -d | tar -x`, no arca anywhere in the chain.
func TestEmergencyRestoreWithStockUnixTools(t *testing.T) {
	ageBin := stockAge(t)
	requireTool(t, "zstd")
	requireTool(t, "tar")

	archivePath, identityPath := emergencyArchive(t)
	target := t.TempDir()

	script := fmt.Sprintf(
		"set -euo pipefail; %q -d -i %q %q | zstd -d | tar -xp -C %q",
		ageBin, identityPath, archivePath, target)
	run(t, "bash", "-c", script)

	for rel, want := range map[string]string{
		"dotfiles/.ssh/id_ed25519":  "PRIVATE KEY MATERIAL",
		"dotfiles/.ssh/config":      "Host example\n  User me\n",
		"dotfiles/.aws/credentials": "[default]\naws_access_key_id = AKIA\n",
	} {
		got, err := os.ReadFile(filepath.Join(target, rel))
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}

	// Permissions have to survive the stock path too, or a restored ssh key is
	// quietly useless.
	info, err := os.Lstat(filepath.Join(target, "dotfiles/.ssh/id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("ssh key restored with mode %#o, want 0600", got)
	}

	link, err := os.Readlink(filepath.Join(target, "dotfiles/.ssh/alias"))
	if err != nil {
		t.Errorf("symlink not restored by stock tar: %v", err)
	} else if link != "config" {
		t.Errorf("symlink target = %q, want config", link)
	}

	// The mapping from the original requirement, reproduced by plain tar.
	if _, err := os.Stat(filepath.Join(target, "Downloads/Downloads/notes.md")); err != nil {
		t.Errorf("mapped download missing: %v", err)
	}
}

// The manifest must be readable with stock tools as well, so someone who finds
// an old archive can learn what it holds before committing to a restore.
func TestManifestIsReadableWithStockTools(t *testing.T) {
	ageBin := stockAge(t)
	requireTool(t, "zstd")
	requireTool(t, "tar")

	archivePath, identityPath := emergencyArchive(t)

	listing := run(t, "bash", "-c", fmt.Sprintf(
		"set -euo pipefail; %q -d -i %q %q | zstd -d | tar -t",
		ageBin, identityPath, archivePath))

	lines := strings.Split(strings.TrimSpace(listing), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != manifest.Name {
		t.Errorf("first member listed by stock tar = %q, want %s", lines[0], manifest.Name)
	}
	if last := strings.TrimSpace(lines[len(lines)-1]); last != manifest.SummaryName {
		t.Errorf("last member = %q, want %s", last, manifest.SummaryName)
	}

	body := run(t, "bash", "-c", fmt.Sprintf(
		"set -euo pipefail; %q -d -i %q %q | zstd -d | tar -xO %s",
		ageBin, identityPath, archivePath, manifest.Name))
	if _, err := manifest.ParseManifest([]byte(body)); err != nil {
		t.Errorf("manifest extracted by stock tar does not parse: %v", err)
	}
	if !strings.Contains(body, "dotfiles/.ssh") {
		t.Error("manifest does not describe the mapping")
	}
}

// Metadata secrecy, checked against the artifact itself: no file or directory
// name may appear anywhere in the bytes on disk.
func TestArchiveLeaksNoNames(t *testing.T) {
	archivePath, _ := emergencyArchive(t)
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"id_ed25519", "credentials", "dotfiles", "Downloads",
		"notes.md", ".ssh", ".aws", manifest.Name, "PRIVATE KEY MATERIAL",
	} {
		if strings.Contains(string(data), secret) {
			t.Errorf("archive leaks %q in cleartext", secret)
		}
	}

	// The only cleartext is the age header, which is unavoidable and says
	// nothing about the contents.
	if !strings.HasPrefix(string(data), "age-encryption.org/v1\n") {
		t.Error("archive does not start with the age header")
	}
}

// The artifact is a single file: no sidecar, no index, nothing else left behind.
func TestBackupProducesExactlyOneFile(t *testing.T) {
	archivePath, _ := emergencyArchive(t)
	dir := filepath.Dir(archivePath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 {
		t.Errorf("backup left %d files in the output directory: %v", len(names), names)
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("archive mode = %#o, want 0600", got)
	}
	if info.Mode()&fs.ModeType != 0 {
		t.Error("archive is not a regular file")
	}
}

// Compression has to actually pay for itself.
func TestCompressionShrinksCompressibleData(t *testing.T) {
	f := newFixture(t)
	payload := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 20000)
	f.write("d/big.txt", payload, 0o644)

	out := f.out("b.tar.zst.age")
	res := mustBackup(t, f.cfg(f.group("g", "", "d")), out)

	if res.ArchiveBytes >= int64(len(payload))/4 {
		t.Errorf("archive is %d bytes for %d bytes of highly compressible input",
			res.ArchiveBytes, len(payload))
	}
}
