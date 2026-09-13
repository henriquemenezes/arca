package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hamsa/arca/internal/config"
)

func parse(t *testing.T, src string) *config.Config {
	t.Helper()
	c, err := config.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c
}

func parseErr(t *testing.T, src string) error {
	t.Helper()
	c, err := config.Parse([]byte(src))
	if err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	_, err = c.Resolve()
	return err
}

const minimalGroup = `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["/tmp/a"]
`

// ---------- defaults ----------

func TestDefaultsMatchDesign(t *testing.T) {
	c := config.Default()
	if c.Settings.Compression != "best" {
		t.Errorf("compression = %q, want best", c.Settings.Compression)
	}
	if c.Settings.Compressor != "zstd" {
		t.Errorf("compressor = %q, want zstd", c.Settings.Compressor)
	}
	if c.Settings.Cipher != "age" {
		t.Errorf("cipher = %q, want age", c.Settings.Cipher)
	}
	if !c.Settings.OneFilesystem {
		t.Error("one_filesystem should default to true")
	}
	if c.Settings.FollowSymlinks {
		t.Error("follow_symlinks should default to false")
	}
}

func TestFileOverridesDefaultsButKeepsUnsetKeys(t *testing.T) {
	c := parse(t, `
[settings]
compression = "fastest"
`+minimalGroup)

	if c.Settings.Compression != "fastest" {
		t.Errorf("compression = %q, want fastest", c.Settings.Compression)
	}
	if c.Settings.Compressor != "zstd" {
		t.Errorf("unset key lost its default: compressor = %q", c.Settings.Compressor)
	}
	if !c.Settings.OneFilesystem {
		t.Error("unset one_filesystem lost its default")
	}
}

func TestExplicitFalseBeatsTrueDefault(t *testing.T) {
	c := parse(t, `
[settings]
one_filesystem = false
`+minimalGroup)

	if c.Settings.OneFilesystem {
		t.Error("explicit one_filesystem = false was ignored")
	}
}

func TestUnknownKeyIsRejected(t *testing.T) {
	_, err := config.Parse([]byte(`
[settings]
compresion = "best"
` + minimalGroup))
	if err == nil {
		t.Fatal("expected a typo in a config key to be an error")
	}
	if !strings.Contains(err.Error(), "compresion") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

// ---------- dual source form ----------

func TestSourceAcceptsStringAndTableForms(t *testing.T) {
	c := parse(t, `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = [
  "/home/u/.ssh",
  { path = "/home/u/.config/nvim", as = "nvim-config" },
]
`)
	got := c.Groups[0].Sources
	if len(got) != 2 {
		t.Fatalf("got %d sources, want 2", len(got))
	}
	if got[0].Path != "/home/u/.ssh" || got[0].As != "" {
		t.Errorf("string form = %+v", got[0])
	}
	if got[1].Path != "/home/u/.config/nvim" || got[1].As != "nvim-config" {
		t.Errorf("table form = %+v", got[1])
	}
}

func TestSourceTableRejectsUnknownKey(t *testing.T) {
	_, err := config.Parse([]byte(`
[[group]]
name    = "g"
dest    = "d"
sources = [{ path = "/tmp/a", alias = "b" }]
`))
	if err == nil || !strings.Contains(err.Error(), "alias") {
		t.Errorf("expected unknown source key to be named, got: %v", err)
	}
}

func TestSourceRejectsWrongType(t *testing.T) {
	_, err := config.Parse([]byte(`
[[group]]
name    = "g"
dest    = "d"
sources = [42]
`))
	if err == nil {
		t.Error("expected a non-string, non-table source to be rejected")
	}
}

// ---------- path handling ----------

func TestTildeExpansion(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	c := parse(t, `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["~/.ssh", "~"]
`)
	res, err := c.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res[0].Path != "/home/tester/.ssh" {
		t.Errorf("path = %q, want /home/tester/.ssh", res[0].Path)
	}
	if res[1].Path != "/home/tester" {
		t.Errorf("bare ~ = %q, want /home/tester", res[1].Path)
	}
}

// "~notauser/x" must stay literal rather than silently becoming something else.
func TestTildeOnlyExpandsForCurrentUser(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	c := parse(t, `
[[group]]
name    = "g"
dest    = "d"
sources = ["~other/file"]
`)
	res, err := c.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if strings.HasPrefix(res[0].Path, "/home/tester") {
		t.Errorf("~other was wrongly expanded to %q", res[0].Path)
	}
}

// ---------- the mapping from the original requirement ----------

func TestResolveProducesRequestedMapping(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	c := parse(t, `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["~/.ssh", "~/.aws"]

[[group]]
name    = "downloads"
dest    = "Downloads"
sources = ["~/Downloads"]
`)
	res, err := c.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := map[string]string{
		"/home/tester/.ssh":      "dotfiles/.ssh",
		"/home/tester/.aws":      "dotfiles/.aws",
		"/home/tester/Downloads": "Downloads/Downloads",
	}
	got := map[string]string{}
	for _, r := range res {
		got[r.Path] = r.Member
	}
	for path, member := range want {
		if got[path] != member {
			t.Errorf("%s -> %q, want %q", path, got[path], member)
		}
	}
}

func TestResolveUsesAliasWhenGiven(t *testing.T) {
	c := parse(t, `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = [{ path = "/home/u/.config/nvim", as = "nvim-config" }]
`)
	res, _ := c.Resolve()
	if res[0].Member != filepath.Join("dotfiles", "nvim-config") {
		t.Errorf("member = %q", res[0].Member)
	}
}

func TestResolveCarriesGroupExcludes(t *testing.T) {
	c := parse(t, `
[[group]]
name    = "projects"
dest    = "Work"
sources = ["/home/u/Work"]
exclude = ["**/node_modules", "**/target"]
`)
	res, _ := c.Resolve()
	if len(res[0].Exclude) != 2 || res[0].Exclude[0] != "**/node_modules" {
		t.Errorf("exclude = %v", res[0].Exclude)
	}
}

// ---------- collisions ----------

// Two sources with the same basename land on the same archive path; that must
// be a loud error at plan time, never a silent overwrite.
func TestCollisionAcrossGroupsIsRejected(t *testing.T) {
	err := parseErr(t, `
[[group]]
name    = "a"
dest    = "dotfiles"
sources = ["/home/u/.config/nvim"]

[[group]]
name    = "b"
dest    = "dotfiles"
sources = ["/home/u/.local/share/nvim"]
`)
	if err == nil {
		t.Fatal("expected a collision error")
	}
	for _, want := range []string{"dotfiles/nvim", "/home/u/.config/nvim", "/home/u/.local/share/nvim"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestCollisionWithinGroupIsRejected(t *testing.T) {
	err := parseErr(t, `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["/home/u/.config/nvim", "/home/u/.local/share/nvim"]
`)
	if err == nil {
		t.Fatal("expected a collision error")
	}
}

func TestAliasResolvesCollision(t *testing.T) {
	if err := parseErr(t, `
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = [
  { path = "/home/u/.config/nvim",      as = "nvim-config" },
  { path = "/home/u/.local/share/nvim", as = "nvim-data"   },
]
`); err != nil {
		t.Errorf("aliases should resolve the collision, got: %v", err)
	}
}

// The manifest is the first member of the archive; nothing may shadow it.
func TestSourceCannotShadowManifest(t *testing.T) {
	err := parseErr(t, `
[[group]]
name    = "g"
dest    = ""
sources = [{ path = "/tmp/x", as = "BACKUP-MANIFEST.json" }]
`)
	if err == nil || !strings.Contains(err.Error(), config.ManifestName) {
		t.Errorf("expected refusal to shadow the manifest, got: %v", err)
	}
}

// ---------- validation ----------

func TestValidationRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no groups at all": `
[settings]
compression = "best"
`,
		"empty group name": `
[[group]]
name    = ""
dest    = "d"
sources = ["/tmp/a"]
`,
		"duplicate group name": `
[[group]]
name    = "g"
dest    = "d1"
sources = ["/tmp/a"]

[[group]]
name    = "g"
dest    = "d2"
sources = ["/tmp/b"]
`,
		"group with no sources": `
[[group]]
name    = "g"
dest    = "d"
sources = []
`,
		"absolute dest escapes the archive": `
[[group]]
name    = "g"
dest    = "/etc"
sources = ["/tmp/a"]
`,
		"dest climbing out of the archive": `
[[group]]
name    = "g"
dest    = "../outside"
sources = ["/tmp/a"]
`,
		"alias containing a separator": `
[[group]]
name    = "g"
dest    = "d"
sources = [{ path = "/tmp/a", as = "sub/dir" }]
`,
		"alias climbing out": `
[[group]]
name    = "g"
dest    = "d"
sources = [{ path = "/tmp/a", as = ".." }]
`,
		"empty source path": `
[[group]]
name    = "g"
dest    = "d"
sources = [""]
`,
		"relative source path": `
[[group]]
name    = "g"
dest    = "d"
sources = ["relative/path"]
`,
		"unknown compression level": `
[settings]
compression = "ludicrous"
` + minimalGroup,
		"unknown compressor": `
[settings]
compressor = "brotli"
` + minimalGroup,
		"unknown cipher": `
[settings]
cipher = "rot13"
` + minimalGroup,
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if err := parseErr(t, src); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestValidConfigPasses(t *testing.T) {
	if err := parseErr(t, `
[settings]
compression    = "best"
threads        = 4
one_filesystem = true

[encryption]
recipients = ["age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"]

[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["/home/u/.ssh", "/home/u/.aws"]
exclude = ["**/known_hosts.old"]

[[group]]
name    = "projects"
dest    = "Work"
sources = ["/home/u/Work"]
exclude = ["**/node_modules"]
`); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

// ---------- CLI overrides (defaults < file < flags) ----------

func TestOverridesBeatFileValues(t *testing.T) {
	c := parse(t, `
[settings]
compression = "fastest"
threads     = 2

[encryption]
recipients = ["age1fromfile"]
`+minimalGroup)

	level, threads := "best", 8
	err := c.Apply(config.Overrides{
		Compression: &level,
		Threads:     &threads,
		Recipients:  []string{"age1fromflag"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if c.Settings.Compression != "best" {
		t.Errorf("compression = %q, want best", c.Settings.Compression)
	}
	if c.Settings.Threads != 8 {
		t.Errorf("threads = %d, want 8", c.Settings.Threads)
	}
	if len(c.Encryption.Recipients) != 1 || c.Encryption.Recipients[0] != "age1fromflag" {
		t.Errorf("recipients = %v, want the flag value to replace the file value", c.Encryption.Recipients)
	}
}

func TestEmptyOverridesChangeNothing(t *testing.T) {
	c := parse(t, `
[settings]
compression = "fastest"
`+minimalGroup)

	if err := c.Apply(config.Overrides{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if c.Settings.Compression != "fastest" {
		t.Errorf("compression = %q, want the file value to survive", c.Settings.Compression)
	}
}

// Ad-hoc backup with no config file at all.
func TestAdHocSourcesFormAGroup(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	c := config.Default()
	if err := c.Apply(config.Overrides{Sources: []string{"~/.ssh:dotfiles", "~/Downloads"}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	res, err := c.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got := map[string]string{}
	for _, r := range res {
		got[r.Path] = r.Member
	}
	if got["/home/tester/.ssh"] != "dotfiles/.ssh" {
		t.Errorf("~/.ssh:dotfiles -> %q", got["/home/tester/.ssh"])
	}
	if got["/home/tester/Downloads"] != "Downloads" {
		t.Errorf("bare source -> %q, want archive root", got["/home/tester/Downloads"])
	}
}

func TestSourceSpecParsing(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	c := config.Default()
	err := c.Apply(config.Overrides{Sources: []string{":nopath"}})
	if err == nil {
		t.Error("expected an empty path in a source spec to be rejected")
	}
}

func TestSourceCannotShadowSummary(t *testing.T) {
	err := parseErr(t, `
[[group]]
name    = "g"
dest    = ""
sources = [{ path = "/tmp/x", as = "BACKUP-SUMMARY.json" }]
`)
	if err == nil || !strings.Contains(err.Error(), config.SummaryName) {
		t.Errorf("expected refusal to shadow the summary, got: %v", err)
	}
}
