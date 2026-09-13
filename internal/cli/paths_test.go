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
