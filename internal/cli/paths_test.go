package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquemenezes/arca/internal/cli"
	"github.com/henriquemenezes/arca/internal/codec"
)

// TestMain fences the whole package away from the real home directory.
//
// The config search ends at ~/.arca, and the identity lives there too, so a
// test that calls FindConfig or ResolveConfigPath without saying otherwise
// reaches into the home of whoever runs the suite: it finds their arca.toml and
// fails, or worse, writes over it. The e2e tests already fence themselves this
// way in newHarness; this does the same for every other test in the package,
// and an individual test is still free to point the variables somewhere else.
func TestMain(m *testing.M) {
	sandbox, err := os.MkdirTemp("", "arca-cli-test")
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(filepath.Join(sandbox, "home"), 0o755); err != nil {
		panic(err)
	}
	os.Setenv(cli.HomeEnv, filepath.Join(sandbox, "arca"))
	os.Setenv("HOME", filepath.Join(sandbox, "home"))

	code := m.Run()
	os.RemoveAll(sandbox)
	os.Exit(code)
}

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
		// Every place the search looks has to be empty, or this asserts
		// something other than what it says. ~/.arca is the one easily
		// forgotten, and the one that reaches a real home.
		empty := t.TempDir()
		t.Chdir(empty)
		t.Setenv(cli.HomeEnv, "")
		t.Setenv("HOME", empty)
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
// ~/.arca, and those two are the only places arca looks.
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

	// setup returns the two candidate directories, neither of them populated.
	setup := func(t *testing.T) (cwd, user string) {
		t.Helper()
		root := t.TempDir()
		cwd = filepath.Join(root, "cwd")
		user = filepath.Join(root, "arca")
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Chdir(cwd)
		t.Setenv(cli.HomeEnv, user)
		t.Setenv("HOME", filepath.Join(root, "home"))
		return cwd, user
	}

	t.Run("the working directory wins", func(t *testing.T) {
		cwd, user := setup(t)
		write(t, cwd)
		write(t, user)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != cli.ConfigFileName {
			t.Errorf("got %q, want the local %q", got, cli.ConfigFileName)
		}
	})

	t.Run("then the user directory", func(t *testing.T) {
		_, user := setup(t)
		want := write(t, user)
		got, err := cli.FindConfig("")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

}

// The identity lives in ~/.arca and is looked for there alone.
func TestFindIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	user := filepath.Join(root, "arca")
	if err := os.MkdirAll(user, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(cli.HomeEnv, user)

	if got := cli.FindIdentity(); got != "" {
		t.Errorf("got %q, want nothing: no key has been written", got)
	}

	want := filepath.Join(user, cli.DefaultIdentityName)
	if err := os.WriteFile(want, []byte("# not a real key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := cli.FindIdentity(); got != want {
		t.Errorf("got %q, want %q", got, want)
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
