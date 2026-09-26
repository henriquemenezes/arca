package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquemenezes/arca/internal/cli"
)

// These exercise the whole command tree the way a user does, in process.

type harness struct {
	t    *testing.T
	dir  string
	pass string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	// Fence the whole run away from the real home. `arca init` and `arca
	// gen-key` now default to ~/.arca, so without this a test would write a
	// config — or a private key — into the home of whoever runs the suite.
	t.Setenv("ARCA_HOME", filepath.Join(dir, "arca"))
	t.Setenv("HOME", filepath.Join(dir, "home"))

	h := &harness{t: t, dir: dir, pass: "gaslight-tremor-unmasked-cufflink-shallot-pesky"}
	passFile := filepath.Join(dir, "pass.txt")
	if err := os.WriteFile(passFile, []byte(h.pass+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(passFile, 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) passFile() string { return filepath.Join(h.dir, "pass.txt") }

func (h *harness) run(args ...string) (string, error) {
	h.t.Helper()
	var out bytes.Buffer
	cmd := cli.NewRootCommand(nil)
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func (h *harness) mustRun(args ...string) string {
	h.t.Helper()
	out, err := h.run(args...)
	if err != nil {
		h.t.Fatalf("arca %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (h *harness) tree() {
	h.t.Helper()
	write := func(rel, content string, mode os.FileMode) {
		p := filepath.Join(h.dir, "home", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			h.t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			h.t.Fatal(err)
		}
	}
	write(".ssh/id_ed25519", "PRIVATE KEY", 0o600)
	write(".ssh/config", "Host example", 0o644)
	write(".aws/credentials", "CREDS", 0o600)
	write("Downloads/photo.jpg", "image", 0o644)
	write("Downloads/node_modules/junk.js", "junk", 0o644)
}

func (h *harness) config() {
	h.t.Helper()
	home := filepath.Join(h.dir, "home")
	body := `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["` + home + `/.ssh", "` + home + `/.aws"]

[[group]]
name    = "downloads"
dest    = "Downloads"
sources = ["` + home + `/Downloads"]
exclude = ["**/node_modules"]
`
	if err := os.WriteFile(filepath.Join(h.dir, cli.ConfigFileName), []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) archivePath() string {
	h.t.Helper()
	matches, err := filepath.Glob(filepath.Join(h.dir, "out", "*.age"))
	if err != nil || len(matches) != 1 {
		h.t.Fatalf("expected exactly one archive, got %v (%v)", matches, err)
	}
	return matches[0]
}

// ---------- the full journey ----------

func TestFullJourneyFromInitToRestore(t *testing.T) {
	h := newHarness(t)
	h.tree()

	// With no argument init writes the user-wide config, so this also proves
	// the later commands find it without -c and without an arca.toml here.
	globalCfg := filepath.Join(os.Getenv(cli.HomeEnv), cli.ConfigFileName)
	out := h.mustRun("init")
	if !strings.Contains(out, globalCfg) {
		t.Errorf("init did not report the file it created:\n%s", out)
	}
	if _, err := os.Stat(globalCfg); err != nil {
		t.Fatalf("init wrote no config: %v", err)
	}
	// The generated config points at ~; replace it with the test tree.
	h.config()

	out = h.mustRun("plan")
	for _, want := range []string{"dotfiles/.ssh", "dotfiles/.aws", "Downloads/Downloads"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan did not show the mapping %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "node_modules") {
		t.Error("plan included an excluded directory")
	}

	h.mustRun("backup", "-o", "out/", "--passphrase-file", h.passFile())
	archive := h.archivePath()

	out = h.mustRun("verify", archive, "--passphrase-file", h.passFile())
	if !strings.Contains(out, "Verified") {
		t.Errorf("verify did not confirm the archive:\n%s", out)
	}

	out = h.mustRun("list", archive, "--passphrase-file", h.passFile())
	if !strings.Contains(out, "dotfiles/.ssh/id_ed25519") {
		t.Errorf("list did not show the members:\n%s", out)
	}

	target := filepath.Join(h.dir, "restored")
	h.mustRun("restore", archive, "--passphrase-file", h.passFile(), "--target", target)

	got, err := os.ReadFile(filepath.Join(target, "dotfiles/.ssh/id_ed25519"))
	if err != nil || string(got) != "PRIVATE KEY" {
		t.Fatalf("restored key = %q, err = %v", got, err)
	}
	info, err := os.Lstat(filepath.Join(target, "dotfiles/.ssh/id_ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("restored key mode = %#o, want 0600", mode)
	}
}

// ---------- ad-hoc use, with no config file ----------

func TestBackupAndRestoreWithNoConfigFile(t *testing.T) {
	h := newHarness(t)
	h.tree()
	home := filepath.Join(h.dir, "home")

	h.mustRun("backup",
		"--source", home+"/.ssh:dotfiles",
		"--source", home+"/Downloads",
		"-o", "out/",
		"--passphrase-file", h.passFile())

	target := filepath.Join(h.dir, "restored")
	h.mustRun("restore", h.archivePath(), "--passphrase-file", h.passFile(), "--target", target)

	for _, want := range []string{"dotfiles/.ssh/config", "Downloads/photo.jpg"} {
		if _, err := os.Stat(filepath.Join(target, want)); err != nil {
			t.Errorf("%s missing after ad-hoc restore: %v", want, err)
		}
	}
}

func TestNothingToBackUpIsExplained(t *testing.T) {
	h := newHarness(t)
	out, err := h.run("backup", "-o", "out/", "--passphrase-file", h.passFile())
	if err == nil {
		t.Fatal("expected an error when there is no config and no --source")
	}
	if !strings.Contains(err.Error(), "arca init") {
		t.Errorf("error should point at the fix, got: %v\n%s", err, out)
	}
}

// ---------- key mode ----------

func TestKeyModeRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.tree()

	keyPath := filepath.Join(h.dir, "key.age")
	out := h.mustRun("gen-key", "-o", keyPath)

	recipient := ""
	for _, line := range strings.Split(out, "\n") {
		if f := strings.TrimSpace(line); strings.HasPrefix(f, "age1") {
			recipient = f
			break
		}
	}
	if recipient == "" {
		t.Fatalf("gen-key did not print a recipient:\n%s", out)
	}
	if info, err := os.Stat(keyPath); err != nil {
		t.Fatal(err)
	} else if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("identity file mode = %#o, want 0600", mode)
	}

	// Regenerating over an existing identity would orphan every archive
	// encrypted to the old one.
	if _, err := h.run("gen-key", "-o", keyPath); err == nil {
		t.Error("gen-key overwrote an existing identity")
	}

	h.mustRun("backup",
		"--source", filepath.Join(h.dir, "home", ".ssh")+":dotfiles",
		"-o", "out/", "-r", recipient)

	target := filepath.Join(h.dir, "restored")
	h.mustRun("restore", h.archivePath(), "-i", keyPath, "--target", target)

	if _, err := os.Stat(filepath.Join(target, "dotfiles/.ssh/config")); err != nil {
		t.Errorf("key-mode restore failed: %v", err)
	}
}

// ---------- refusals ----------

func TestWeakPassphraseIsRefusedUnlessOverridden(t *testing.T) {
	h := newHarness(t)
	h.tree()
	weak := filepath.Join(h.dir, "weak.txt")
	if err := os.WriteFile(weak, []byte("senha123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Chmod(weak, 0o600)

	args := []string{"backup", "--source", filepath.Join(h.dir, "home", ".ssh"), "-o", "out/", "--passphrase-file", weak}
	if _, err := h.run(args...); err == nil {
		t.Fatal("a weak passphrase was accepted")
	}
	if _, err := h.run(append(args, "--allow-weak-passphrase")...); err != nil {
		t.Errorf("--allow-weak-passphrase should permit it: %v", err)
	}
}

func TestRestoreRefusesToClobberWithoutTheFlag(t *testing.T) {
	h := newHarness(t)
	h.tree()
	h.mustRun("backup", "--source", filepath.Join(h.dir, "home", ".ssh")+":dotfiles",
		"-o", "out/", "--passphrase-file", h.passFile())
	archive := h.archivePath()

	target := filepath.Join(h.dir, "restored")
	h.mustRun("restore", archive, "--passphrase-file", h.passFile(), "--target", target)

	if _, err := h.run("restore", archive, "--passphrase-file", h.passFile(), "--target", target); err == nil {
		t.Fatal("a second restore silently overwrote the first")
	}
	if _, err := h.run("restore", archive, "--passphrase-file", h.passFile(), "--target", target, "--overwrite"); err != nil {
		t.Errorf("--overwrite should allow it: %v", err)
	}
}

func TestBackupRefusesToOverwriteAnExistingArchive(t *testing.T) {
	h := newHarness(t)
	h.tree()
	dest := filepath.Join(h.dir, "fixed-name.tar.zst.age")
	args := []string{"backup", "--source", filepath.Join(h.dir, "home", ".ssh"), "-o", dest, "--passphrase-file", h.passFile()}

	h.mustRun(args...)
	before, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.run(args...); err == nil {
		t.Fatal("the second backup overwrote the first")
	}
	after, _ := os.ReadFile(dest)
	if !bytes.Equal(before, after) {
		t.Error("the existing archive was modified")
	}
}

// A config typo must stop the run, not silently disable a setting.
func TestConfigTypoIsRejected(t *testing.T) {
	h := newHarness(t)
	h.tree()
	if err := os.WriteFile(cli.ConfigFileName, []byte(`
[settings]
compresion = "best"

[[group]]
name    = "g"
dest    = "d"
sources = ["`+filepath.Join(h.dir, "home", ".ssh")+`"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := h.run("plan")
	if err == nil {
		t.Fatal("a misspelled config key was accepted")
	}
	if !strings.Contains(err.Error(), "compresion") {
		t.Errorf("error should name the typo, got: %v", err)
	}
}

// The printed manual-restore command is the project's promise; it must match
// the pipeline that was actually used.
func TestBackupPrintsTheManualRestorePath(t *testing.T) {
	h := newHarness(t)
	h.tree()
	out := h.mustRun("backup", "--source", filepath.Join(h.dir, "home", ".ssh"),
		"-o", "out/", "--passphrase-file", h.passFile())

	if !strings.Contains(out, "age -d") || !strings.Contains(out, "zstd -d") || !strings.Contains(out, "tar -xp") {
		t.Errorf("backup did not print the stock-tools restore command:\n%s", out)
	}
	if !strings.Contains(out, "KEEP THE SECRET") {
		t.Errorf("backup did not print the secret-handling notice:\n%s", out)
	}
	if n := strings.Count(out, "KEEP THE SECRET"); n != 1 {
		t.Errorf("the notice was printed %d times, want exactly 1", n)
	}
}

// Encrypting a backup with an ssh key that lives inside that same backup means
// losing the machine loses both at once. That has to be said out loud.
func TestCircularSSHKeyDependencyIsWarnedAbout(t *testing.T) {
	h := newHarness(t)
	home := filepath.Join(h.dir, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519.pub"),
		[]byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJZ5hKqZ8F7VQe7bXQ0YvXoK7Tq1N5zZJ2xW9pLmQ3Rs test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host x"), 0o600); err != nil {
		t.Fatal(err)
	}

	pub, err := os.ReadFile(filepath.Join(home, ".ssh", "id_ed25519.pub"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.run("backup",
		"--source", filepath.Join(home, ".ssh")+":dotfiles",
		"-o", "out/", "-r", strings.TrimSpace(string(pub)))
	if err != nil {
		t.Fatalf("backup failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "you lose the key and the backup together") {
		t.Errorf("no warning about the circular dependency:\n%s", out)
	}
}

// init now defaults to the user-wide directory, so where it writes — and what
// it refuses to clobber — is worth pinning down on its own.
func TestInitWritesWhereItIsTold(t *testing.T) {
	t.Run("no argument writes the user-wide config", func(t *testing.T) {
		h := newHarness(t)
		want := filepath.Join(os.Getenv(cli.HomeEnv), cli.ConfigFileName)

		h.mustRun("init")
		info, err := os.Stat(want)
		if err != nil {
			t.Fatalf("init wrote nothing to %s: %v", want, err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Errorf("config mode is %o, want 644: it holds no secret", got)
		}
		// The directory is shared with identity.age, so it may not be loose.
		dir, err := os.Stat(filepath.Dir(want))
		if err != nil {
			t.Fatal(err)
		}
		if got := dir.Mode().Perm(); got != 0o700 {
			t.Errorf("%s is mode %o, want 700: it holds key material", filepath.Dir(want), got)
		}
		// Nothing was left behind in the working directory.
		if _, err := os.Stat(cli.ConfigFileName); err == nil {
			t.Error("init also wrote a config into the working directory")
		}

		if _, err := h.run("init"); err == nil {
			t.Error("a second init replaced the existing config")
		}
	})

	t.Run("a directory argument gets the conventional name", func(t *testing.T) {
		h := newHarness(t)
		dir := filepath.Join(h.dir, "project")
		h.mustRun("init", dir+string(filepath.Separator))
		if _, err := os.Stat(filepath.Join(dir, cli.ConfigFileName)); err != nil {
			t.Fatalf("init did not write into %s: %v", dir, err)
		}
	})

	t.Run("a file argument is taken literally", func(t *testing.T) {
		h := newHarness(t)
		h.mustRun("init", "elsewhere.toml")
		if _, err := os.Stat(filepath.Join(h.dir, "elsewhere.toml")); err != nil {
			t.Fatalf("init did not honour the name given: %v", err)
		}
	})
}
