package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/codec"
)

func codecs(t *testing.T) (codec.Compressor, codec.Cipher) {
	t.Helper()
	c, err := codec.GetCompressor("zstd")
	if err != nil {
		t.Fatal(err)
	}
	ci, err := codec.GetCipher("age")
	if err != nil {
		t.Fatal(err)
	}
	return c, ci
}

var when = time.Date(2026, 9, 13, 14, 30, 5, 0, time.UTC)

// The extensions must describe the actual pipeline, so the manual restore
// command a user types is always the right one.
func TestDefaultArchiveNameDescribesThePipeline(t *testing.T) {
	comp, ciph := codecs(t)
	got := cli.DefaultArchiveName("omarchy", when, comp, ciph)
	if got != "arca-omarchy-2026-09-13T14-30-05Z.tar.zst.age" {
		t.Errorf("got %q", got)
	}
}

func TestArchiveNameStaysPortable(t *testing.T) {
	comp, ciph := codecs(t)
	for _, host := range []string{"my host/name", "../../etc", ""} {
		got := cli.DefaultArchiveName(host, when, comp, ciph)
		if strings.ContainsAny(got, "/\\ ") {
			t.Errorf("host %q produced an unsafe name %q", host, got)
		}
		if !strings.HasSuffix(got, ".tar.zst.age") {
			t.Errorf("host %q produced %q", host, got)
		}
	}
}

func TestResolveOutputPath(t *testing.T) {
	comp, ciph := codecs(t)
	dir := t.TempDir()

	t.Run("existing directory gets a generated name", func(t *testing.T) {
		got, err := cli.ResolveOutputPath(dir, when, comp, ciph)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(got) != dir || !strings.HasSuffix(got, ".tar.zst.age") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("explicit file name is honoured", func(t *testing.T) {
		want := filepath.Join(dir, "my-backup.bin")
		got, err := cli.ResolveOutputPath(want, when, comp, ciph)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("trailing separator means directory even if missing", func(t *testing.T) {
		got, err := cli.ResolveOutputPath(filepath.Join(dir, "new")+string(filepath.Separator), when, comp, ciph)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(got) != filepath.Join(dir, "new") {
			t.Errorf("got %q", got)
		}
	})

	t.Run("empty output lands in the working directory", func(t *testing.T) {
		got, err := cli.ResolveOutputPath("", when, comp, ciph)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("got %q, want an absolute path", got)
		}
	})
}

func TestFindConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, cli.ConfigFileName)
	if err := os.WriteFile(cfg, []byte("# empty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("explicit path must exist", func(t *testing.T) {
		if _, err := cli.FindConfig(filepath.Join(dir, "nope.toml")); err == nil {
			t.Error("a missing explicit config should be an error, not a silent fallback")
		}
		got, err := cli.FindConfig(cfg)
		if err != nil || got != cfg {
			t.Errorf("got %q, %v", got, err)
		}
	})

	t.Run("working directory is searched", func(t *testing.T) {
		t.Chdir(dir)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != cli.ConfigFileName {
			t.Errorf("got %q, want %q", got, cli.ConfigFileName)
		}
	})

	t.Run("no config at all is not an error", func(t *testing.T) {
		empty := t.TempDir()
		t.Chdir(empty)
		t.Setenv("XDG_CONFIG_HOME", empty)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestUserDir(t *testing.T) {
	t.Run("ARCA_HOME wins", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(cli.HomeEnv, dir)
		if got := cli.UserDir(); got != dir {
			t.Errorf("got %q, want %q", got, dir)
		}
	})

	t.Run("otherwise it is ~/.arca", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv(cli.HomeEnv, "")
		t.Setenv("HOME", home)
		want := filepath.Join(home, cli.UserDirName)
		if got := cli.UserDir(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

// The search order is the whole contract: a config next to you beats the one in
// ~/.arca, which beats the directory arca used before it had one of its own.
func TestFindConfigSearchOrder(t *testing.T) {
	write := func(t *testing.T, dir string) string {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, cli.ConfigFileName)
		if err := os.WriteFile(p, []byte("# empty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// setup returns the three candidate directories, none of them populated.
	setup := func(t *testing.T) (cwd, user, legacy string) {
		t.Helper()
		root := t.TempDir()
		cwd = filepath.Join(root, "cwd")
		user = filepath.Join(root, "arca")
		legacy = filepath.Join(root, "xdg", "arca")
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Chdir(cwd)
		t.Setenv(cli.HomeEnv, "")
		t.Setenv("HOME", filepath.Join(root, "home"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
		return cwd, user, legacy
	}

	t.Run("the working directory wins", func(t *testing.T) {
		cwd, user, legacy := setup(t)
		write(t, cwd)
		write(t, user)
		write(t, legacy)
		t.Setenv(cli.HomeEnv, user)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != cli.ConfigFileName {
			t.Errorf("got %q, want the local %q", got, cli.ConfigFileName)
		}
	})

	t.Run("then the user directory", func(t *testing.T) {
		_, user, legacy := setup(t)
		want := write(t, user)
		write(t, legacy)
		t.Setenv(cli.HomeEnv, user)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("then the legacy directory", func(t *testing.T) {
		_, _, legacy := setup(t)
		want := write(t, legacy)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("got %q, want the legacy %q", got, want)
		}
	})

	// ARCA_HOME is an override, not an addition: it has to be enough on its own
	// for a test or a sandbox to be sure nothing else is being read.
	t.Run("ARCA_HOME switches the legacy directory off", func(t *testing.T) {
		_, user, legacy := setup(t)
		write(t, legacy)
		t.Setenv(cli.HomeEnv, user)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("got %q, want nothing: ARCA_HOME was set", got)
		}
	})
}

// Losing sight of an identity is not an inconvenience, it is an archive nobody
// can open. The old location has to stay readable.
func TestFindIdentityReadsTheLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "xdg", "arca")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(legacy, cli.DefaultIdentityName)
	if err := os.WriteFile(want, []byte("# not a real key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cli.HomeEnv, "")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	if got := cli.FindIdentity(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A key in the current location takes precedence over the old one.
	current := filepath.Join(root, "arca")
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatal(err)
	}
	newer := filepath.Join(current, cli.DefaultIdentityName)
	if err := os.WriteFile(newer, []byte("# not a real key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cli.HomeEnv, current)
	if got := cli.FindIdentity(); got != newer {
		t.Errorf("got %q, want %q", got, newer)
	}
}

func TestResolveConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv(cli.HomeEnv, home)

	t.Run("empty means the user directory", func(t *testing.T) {
		got, err := cli.ResolveConfigPath("")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, cli.ConfigFileName); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("an existing directory gets the conventional name", func(t *testing.T) {
		dir := t.TempDir()
		got, err := cli.ResolveConfigPath(dir)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, cli.ConfigFileName); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a trailing separator means a directory even if it does not exist", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "nope") + string(filepath.Separator)
		got, err := cli.ResolveConfigPath(dir)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, cli.ConfigFileName); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("anything else is taken literally", func(t *testing.T) {
		got, err := cli.ResolveConfigPath("backup.toml")
		if err != nil {
			t.Fatal(err)
		}
		if got != "backup.toml" {
			t.Errorf("got %q, want the name as given", got)
		}
	})
}
