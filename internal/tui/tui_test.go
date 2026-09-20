package tui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hamsa/arca/internal/archive"
	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/config"
)

// TestMain fences the whole package away from the real user config directory.
// A test that walks the menu can reach "generate an identity", and writing a
// private key into someone's home is not something a test may ever do.
func TestMain(m *testing.M) {
	sandbox, err := os.MkdirTemp("", "arca-tui-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("ARCA_HOME", filepath.Join(sandbox, "arca"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	os.Setenv("HOME", filepath.Join(sandbox, "home"))
	os.MkdirAll(filepath.Join(sandbox, "home"), 0o755)

	code := m.Run()
	os.RemoveAll(sandbox)
	os.Exit(code)
}

func key(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func send(m *model, keys ...string) {
	for _, k := range keys {
		m.Update(key(k))
	}
}

// ---------- mapping ----------

func TestMemberPreviewMatchesWhatTheArchiveWillHold(t *testing.T) {
	cases := []struct{ path, dest, want string }{
		{"/home/u/.ssh", "dotfiles", "dotfiles/.ssh"},
		{"/home/u/.aws", "dotfiles", "dotfiles/.aws"},
		{"/home/u/Downloads", "Downloads", "Downloads/Downloads"},
		{"/home/u/notes.txt", "", "notes.txt"},
	}
	for _, tc := range cases {
		if got := (mapEntry{path: tc.path, dest: tc.dest}).member(); got != tc.want {
			t.Errorf("%s into %q = %q, want %q", tc.path, tc.dest, got, tc.want)
		}
	}
}

// A plain directory is already named; it lands at the archive root so that it
// keeps that name and nothing more. Suggesting its own basename as the
// destination is what used to store Downloads as Downloads/Downloads.
func TestSuggestDestSendsAPlainPathToTheArchiveRoot(t *testing.T) {
	for _, path := range []string{"/home/u/Downloads", "/home/u/Work", "/home/u/notes.txt"} {
		if got := suggestDest(path); got != "" {
			t.Errorf("suggestDest(%q) = %q, want the archive root", path, got)
		}
		want := filepath.Base(path)
		if got := (mapEntry{path: path, dest: suggestDest(path)}).member(); got != want {
			t.Errorf("%s is stored as %q, want %q", path, got, want)
		}
	}
}

// A dotfile is the exception: its name begins with a dot precisely because
// it is not meant to be seen, and a dozen of them loose at the archive root
// is what the grouping avoids.
func TestSuggestDestGroupsDotfiles(t *testing.T) {
	for _, path := range []string{"/home/u/.ssh", "/home/u/.aws"} {
		if got := suggestDest(path); got != "dotfiles" {
			t.Errorf("suggestDest(%q) = %q, want dotfiles", path, got)
		}
	}
}

// The group at the archive root still needs a name, and the file is easier to
// read if it is not called group-1.
func TestBuildConfigNamesTheRootGroup(t *testing.T) {
	m := newModel()
	m.mapping = []mapEntry{{path: "/home/u/Downloads", dest: ""}}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Groups[0].Name != "root" {
		t.Errorf("the root group is called %q, want root", cfg.Groups[0].Name)
	}
}

// A configuration may already call one of its groups "root", and two groups
// may not share a name — so the fallback has to give way rather than build a
// configuration the engine rejects.
func TestBuildConfigKeepsTheRootNameUnique(t *testing.T) {
	m := newModel()
	m.destName = map[string]string{"Work": "root"}
	m.mapping = []mapEntry{
		{path: "/home/u/src", dest: "Work"},
		{path: "/home/u/Downloads", dest: ""},
	}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if cfg.Groups[0].Name == cfg.Groups[1].Name {
		t.Errorf("both groups are called %q", cfg.Groups[0].Name)
	}
}

// Most sources now land at the archive root, so reopening a saved
// configuration has to leave that group exactly as it was: a root group that
// came back renamed, or nested one level deeper each time, would rewrite the
// archive layout on every edit.
func TestARootGroupSurvivesAnEditRoundTrip(t *testing.T) {
	m := newModel()
	m.mapping = []mapEntry{
		{path: "/home/u/Downloads", dest: suggestDest("/home/u/Downloads")},
		{path: "/home/u/.ssh", dest: suggestDest("/home/u/.ssh")},
	}
	first, err := m.buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}

	// Reopen it the way the menu does with an arca.toml found on disk.
	again := newModel()
	again.adoptConfig(first)
	again.seedMapping(first)
	second, err := again.buildConfig()
	if err != nil {
		t.Fatalf("rebuilding the adopted configuration: %v", err)
	}

	if !reflect.DeepEqual(first.Groups, second.Groups) {
		t.Errorf("the round trip changed the groups:\n%+v\n%+v", first.Groups, second.Groups)
	}
}

func TestBuildConfigGroupsBySharedDestination(t *testing.T) {
	m := newModel()
	m.mapping = []mapEntry{
		{path: "/home/u/.ssh", dest: "dotfiles"},
		{path: "/home/u/.aws", dest: "dotfiles"},
		{path: "/home/u/Downloads", dest: "Downloads"},
	}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if len(cfg.Groups) != 2 {
		t.Fatalf("groups = %d, want 2 (one per destination)", len(cfg.Groups))
	}

	resolved, err := cfg.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range resolved {
		got[r.Path] = r.Member
	}
	for path, want := range map[string]string{
		"/home/u/.ssh":      "dotfiles/.ssh",
		"/home/u/.aws":      "dotfiles/.aws",
		"/home/u/Downloads": "Downloads/Downloads",
	} {
		if got[path] != want {
			t.Errorf("%s → %q, want %q", path, got[path], want)
		}
	}
}

// The interface must not be able to build a configuration the engine rejects.
func TestBuildConfigRefusesACollision(t *testing.T) {
	m := newModel()
	m.mapping = []mapEntry{
		{path: "/home/u/.config/nvim", dest: "dotfiles"},
		{path: "/home/u/.local/share/nvim", dest: "dotfiles"},
	}
	if _, err := m.buildConfig(); err == nil {
		t.Fatal("two sources with the same basename were accepted")
	}
}

// A session spent choosing things is only saved if the file it writes can be
// read back.
func TestSavedConfigParsesBackIdentically(t *testing.T) {
	m := newModel()
	m.mapping = []mapEntry{
		{path: "/home/u/.ssh", dest: "dotfiles"},
		{path: "/home/u/.aws", dest: "dotfiles"},
		{path: "/home/u/Downloads", dest: "Downloads"},
	}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Encryption.Recipients = []string{"age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"}

	body := m.renderTOML(cfg)
	reparsed, err := config.Parse([]byte(body))
	if err != nil {
		t.Fatalf("the interface wrote a config it cannot read back: %v\n%s", err, body)
	}
	if err := reparsed.Validate(); err != nil {
		t.Fatalf("written config does not validate: %v\n%s", err, body)
	}

	before, _ := cfg.Resolve()
	after, err := reparsed.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("source count changed: %d then %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Member != after[i].Member || before[i].Path != after[i].Path {
			t.Errorf("source %d changed: %+v then %+v", i, before[i], after[i])
		}
	}
	if len(reparsed.Encryption.Recipients) != 1 {
		t.Errorf("recipients lost in the round trip: %v", reparsed.Encryption.Recipients)
	}
}

func TestSplitRecipients(t *testing.T) {
	got := splitRecipients("  age1aaa, age1bbb age1ccc\n")
	want := []string{"age1aaa", "age1bbb", "age1ccc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	if len(splitRecipients("   ")) != 0 {
		t.Error("blank input should yield no recipients")
	}
}

// ---------- browser ----------

func TestBrowserSelectionAndNavigation(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha", "beta"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "zeta.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := newBrowser(root)
	if len(b.entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(b.entries))
	}
	// Directories sort before files.
	if !b.entries[0].isDir || !b.entries[1].isDir || b.entries[2].isDir {
		t.Errorf("ordering wrong: %+v", b.entries)
	}

	b.Update(key(" "))
	if len(b.Selected()) != 1 || filepath.Base(b.Selected()[0]) != "alpha" {
		t.Errorf("selection = %v", b.Selected())
	}
	// The cursor stays on the row it just marked, so a second space undoes it.
	if got, _ := b.current(); filepath.Base(got.path) != "alpha" {
		t.Errorf("space moved the cursor to %q, want it on alpha", got.path)
	}
	b.Update(key(" ")) // toggle off
	if len(b.Selected()) != 0 {
		t.Errorf("space should toggle, selection = %v", b.Selected())
	}

	// Descend and come back; the cursor should land where we left.
	b.cursor = 1
	prev := b.entries[1].path
	b.Update(key("enter"))
	if b.cwd != prev {
		t.Fatalf("cwd = %q, want %q", b.cwd, prev)
	}
	b.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if b.cwd != root {
		t.Fatalf("cwd = %q, want %q", b.cwd, root)
	}
	if got, _ := b.current(); got.path != prev {
		t.Errorf("cursor landed on %q, want %q", got.path, prev)
	}
}

func TestBrowserHidesAndShowsDotfiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".hidden"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "visible"), []byte("x"), 0o644)

	b := newBrowser(root)
	if len(b.entries) != 2 {
		t.Fatalf("dotfiles should be shown by default (they are the point of this tool): %+v", b.entries)
	}
	b.Update(key("."))
	if len(b.entries) != 1 {
		t.Errorf("toggling should hide dotfiles, got %+v", b.entries)
	}
}

func TestBrowserSurvivesAnUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read any directory")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	os.Mkdir(locked, 0o000)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	b := newBrowser(root)
	b.cursor = 0
	b.Update(key("enter"))
	if b.err == nil {
		t.Error("entering an unreadable directory should report an error")
	}
	if b.cwd != root {
		t.Errorf("should have stayed put, cwd = %q", b.cwd)
	}
}

// ---------- flow ----------

func TestMenuReachesEachFlow(t *testing.T) {
	m := newModel()
	m.fromDisk = false
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	send(m, "enter") // back up
	if m.state != stateSources {
		t.Errorf("state = %v, want sources", m.state)
	}
	send(m, "esc")
	send(m, "down", "enter") // restore
	if m.state != statePickArchive || m.intent != intentRestore {
		t.Errorf("state = %v intent = %v", m.state, m.intent)
	}
	send(m, "esc")
	// The cursor stays where it was, so one step reaches "inspect".
	send(m, "down", "enter")
	if m.state != statePickArchive || m.intent != intentInspect {
		t.Errorf("state = %v intent = %v", m.state, m.intent)
	}
	send(m, "esc")

	// Identity generation writes a private key; it must land in the sandbox.
	send(m, "down", "enter")
	if m.state != stateGenerated {
		t.Fatalf("state = %v, want generated", m.state)
	}
	if !strings.HasPrefix(m.generated, "age1") {
		t.Errorf("generated recipient = %q", m.generated)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv(cli.HomeEnv), cli.DefaultIdentityName)); err != nil {
		t.Errorf("identity was not written inside the sandbox: %v", err)
	}
}

func TestSourceSelectionFlowsIntoMapping(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".ssh"), 0o755)
	os.WriteFile(filepath.Join(root, ".ssh", "config"), []byte("x"), 0o600)

	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.state = stateSources
	m.browser = newBrowser(root)

	// Continuing with nothing chosen must say so rather than move on quietly.
	send(m, "tab")
	if m.state != stateSources || m.err == nil {
		t.Fatalf("empty selection was accepted: state=%v err=%v", m.state, m.err)
	}

	send(m, " ", "tab")
	if m.state != stateMapping {
		t.Fatalf("state = %v, want mapping", m.state)
	}
	if len(m.mapping) != 1 || m.mapping[0].dest != "dotfiles" {
		t.Errorf("mapping = %+v", m.mapping)
	}
}

func TestPassphraseScreenRefusesWeakInput(t *testing.T) {
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.cfg = config.Default()
	m.state = statePassphrase
	m.passStage = 0
	m.passInput.SetValue("senha123")

	send(m, "enter")
	if m.err == nil {
		t.Fatal("a weak passphrase was accepted")
	}
	if m.passStage != 0 {
		t.Error("should have stayed on the first entry")
	}

	m.passInput.SetValue("gaslight-tremor-unmasked-cufflink-shallot")
	send(m, "enter")
	if m.err != nil || m.passStage != 1 {
		t.Fatalf("a strong passphrase should advance to confirmation: err=%v stage=%d", m.err, m.passStage)
	}

	m.confirmPass.SetValue("something-else-entirely")
	send(m, "enter")
	if m.err == nil {
		t.Error("a mismatched confirmation was accepted")
	}
}

func TestStrengthMeterHandlesEveryInput(t *testing.T) {
	for _, s := range []string{"", "a", "senha123", strings.Repeat("x", 500)} {
		if out := strengthMeter(s, 30); out == "" {
			t.Errorf("no meter rendered for %q", s)
		}
	}
}

// The interface must produce archives through exactly the same path as the CLI.
func TestInterfaceProducesARealArchive(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o755)
	os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("KEY"), 0o600)
	out := t.TempDir()

	m := newModel()
	m.mapping = []mapEntry{{path: filepath.Join(home, ".ssh"), dest: "dotfiles"}}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	m.cfg = cfg
	m.outPath = filepath.Join(out, "from-tui.tar.zst.age")
	m.backupKey = &codec.Keyring{Passphrase: []byte("gaslight-tremor-unmasked-cufflink-shallot")}

	if cmd := m.refreshPlan(); cmd != nil {
		t.Fatalf("refreshPlan returned a command: %v", m.err)
	}
	if m.err != nil {
		t.Fatalf("plan failed: %v", m.err)
	}
	if m.plan.Stats.Files != 1 {
		t.Errorf("plan files = %d, want 1", m.plan.Stats.Files)
	}

	msg := startBackup(archive.BackupOptions{
		Config:      m.cfg,
		Keyring:     m.backupKey,
		OutputPath:  m.outPath,
		ToolVersion: "test",
		Planned:     &m.plan.Stats,
	}, m.msgs)()

	done, ok := msg.(backupDoneMsg)
	if !ok {
		t.Fatalf("unexpected message %T", msg)
	}
	if done.err != nil {
		t.Fatalf("backup failed: %v", done.err)
	}

	res, err := archive.Verify(m.outPath, m.backupKey)
	if err != nil {
		t.Fatalf("the archive the interface wrote does not verify: %v", err)
	}
	if res.Stats.Files != 1 {
		t.Errorf("archive holds %d files, want 1", res.Stats.Files)
	}
}

// Every screen must render at any size without panicking.
func TestEveryScreenRenders(t *testing.T) {
	states := []state{
		stateMenu, stateSources, stateMapping, stateCrypto, statePassphrase,
		stateRecipients, stateOutput, stateReview, stateSaveConfig, stateRunning, stateDone,
		statePickArchive, stateArchiveKey, stateArchiveInfo, stateRestoreTarget,
		stateRestoring, stateRestoreDone, stateGenerated,
	}
	for _, width := range []int{40, 80, 200} {
		for _, s := range states {
			m := newModel()
			m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
			m.cfg = config.Default()
			m.state = s
			m.mapping = []mapEntry{{path: "/home/u/.ssh", dest: "dotfiles"}}
			if out := m.View(); out == "" {
				t.Errorf("state %v at width %d rendered nothing", s, width)
			}
		}
	}
}

// ---------- reviewing, editing and saving a configuration ----------

// configFixture writes a configuration exercising every field the mapping
// screen never asks about, over source directories that really exist so the
// plan can be built from it.
func configFixture(t *testing.T) (root string, cfg *config.Config, body string) {
	t.Helper()
	root = t.TempDir()
	for _, dir := range []string{".ssh", ".aws", "Work", "config/nvim", "share/nvim"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	body = fmt.Sprintf(`[settings]
compression     = "fastest"
compressor      = "zstd"
cipher          = "age"
threads         = 4
follow_symlinks = true
one_filesystem  = false

[encryption]
recipients = ["age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"]

[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = [
  %q,
  %q,
  { path = %q, as = "nvim-config" },
  { path = %q, as = "nvim-data" },
]
exclude = ["**/known_hosts.old", "*.tmp"]

[[group]]
name    = "projects"
dest    = "Work"
sources = [%q]
exclude = ["**/node_modules"]
`,
		filepath.Join(root, ".ssh"),
		filepath.Join(root, ".aws"),
		filepath.Join(root, "config/nvim"),
		filepath.Join(root, "share/nvim"),
		filepath.Join(root, "Work"))

	cfg, err := config.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return root, cfg, body
}

// reviewing returns a model sitting on the review screen with a configuration
// in hand, the way the menu leaves it when an arca.toml was found.
func reviewing(t *testing.T) (*model, string) {
	t.Helper()
	root, cfg, body := configFixture(t)

	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.cfg, m.cfgTOML, m.cfgPath, m.fromDisk = cfg, body, filepath.Join(root, "arca.toml"), true
	m.adoptConfig(cfg)
	m.state = stateReview
	m.back = []state{stateMenu}
	return m, root
}

// Editing has to start from what the configuration already says, or it is not
// editing — it is being asked the same questions over again.
func TestReviewEditStartsFromTheCurrentConfiguration(t *testing.T) {
	m, _ := reviewing(t)

	send(m, "e")
	if m.state != stateSources {
		t.Fatalf("state = %v, want sources", m.state)
	}
	if m.intentCfg != cfgEdit {
		t.Errorf("intent = %v, want cfgEdit", m.intentCfg)
	}

	if len(m.browser.selected) != 5 {
		t.Errorf("browser marks %d paths, want the 5 the configuration names", len(m.browser.selected))
	}
	for _, g := range m.cfg.Groups {
		for _, src := range g.Sources {
			abs, err := config.ExpandPath(src.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !m.browser.selected[abs] {
				t.Errorf("%s is in the configuration but not marked in the browser", abs)
			}
		}
	}

	dests := map[string]string{}
	aliases := map[string]string{}
	for _, e := range m.mapping {
		dests[filepath.Base(e.path)] = e.dest
		if e.as != "" {
			aliases[e.path] = e.as
		}
	}
	if dests[".ssh"] != "dotfiles" || dests["Work"] != "Work" {
		t.Errorf("mapping did not inherit the destinations: %+v", dests)
	}
	if len(aliases) != 2 {
		t.Errorf("aliases lost on the way into the mapping screen: %+v", aliases)
	}
}

// "New" is the opposite offer: nothing carried over, including the settings.
func TestReviewNewStartsFromNothing(t *testing.T) {
	m, _ := reviewing(t)

	send(m, "n")
	if m.state != stateSources {
		t.Fatalf("state = %v, want sources", m.state)
	}
	if m.intentCfg != cfgNew {
		t.Errorf("intent = %v, want cfgNew", m.intentCfg)
	}
	if len(m.browser.selected) != 0 || len(m.mapping) != 0 {
		t.Errorf("a new configuration started with %d marks and %d mapped entries",
			len(m.browser.selected), len(m.mapping))
	}
	if m.base.Settings.Threads != config.Default().Settings.Threads {
		t.Errorf("a new configuration inherited the old settings: %+v", m.base.Settings)
	}
	if len(m.base.Encryption.Recipients) != 0 {
		t.Errorf("a new configuration inherited the old recipients: %v", m.base.Encryption.Recipients)
	}
}

// The mapping screen asks about sources and destinations and nothing else, so
// everything else has to come back out of an edit untouched. Losing an exclude
// pattern here would quietly put node_modules in the archive.
func TestEditingKeepsWhatTheMappingScreenNeverAsks(t *testing.T) {
	m, _ := reviewing(t)
	before := m.cfg

	send(m, "e")   // review  → sources, already marked
	send(m, "tab") // sources → mapping, destinations inherited
	send(m, "tab") // mapping → save screen
	if m.state != stateSaveConfig {
		t.Fatalf("state = %v (err %v), want the save screen", m.state, m.err)
	}

	got := m.cfg
	if got.Settings != before.Settings {
		t.Errorf("settings changed across an edit:\n got %+v\nwant %+v", got.Settings, before.Settings)
	}
	if len(got.Encryption.Recipients) != 1 || got.Encryption.Recipients[0] != before.Encryption.Recipients[0] {
		t.Errorf("recipients changed across an edit: %v", got.Encryption.Recipients)
	}

	excludes := map[string][]string{}
	names := map[string]string{}
	for _, g := range got.Groups {
		excludes[g.Dest] = g.Exclude
		names[g.Dest] = g.Name
	}
	if len(excludes["dotfiles"]) != 2 || len(excludes["Work"]) != 1 {
		t.Errorf("exclude patterns lost across an edit: %+v", excludes)
	}
	if names["Work"] != "projects" {
		t.Errorf("group name lost across an edit: %q, want %q", names["Work"], "projects")
	}

	// The aliases are what keep the two nvim directories apart in the archive.
	members := map[string]bool{}
	resolved, err := got.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resolved {
		members[r.Member] = true
	}
	if !members["dotfiles/nvim-config"] || !members["dotfiles/nvim-data"] {
		t.Errorf("aliases lost across an edit: %v", members)
	}
}

// Walking back to the browser and forward again must not throw away the
// destinations already decided, whether they were typed or inherited.
func TestSourceReselectionKeepsChosenDestinations(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Pictures"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.state = stateSources
	m.browser = newBrowser(root)

	send(m, " ", "tab")
	if m.state != stateMapping {
		t.Fatalf("state = %v, want mapping", m.state)
	}

	send(m, "e")
	m.destInput.SetValue("media")
	send(m, "enter")
	if m.mapping[0].dest != "media" {
		t.Fatalf("dest = %q, want media", m.mapping[0].dest)
	}

	send(m, "esc") // back to the browser
	send(m, "tab") // and forward again
	if m.state != stateMapping {
		t.Fatalf("state = %v, want mapping", m.state)
	}
	if m.mapping[0].dest != "media" {
		t.Errorf("dest = %q after a trip back through the browser, want media", m.mapping[0].dest)
	}
}

// What is written has to describe the whole configuration. This file can
// replace one the user wrote by hand, so a dropped key is a silently weakened
// backup rather than a cosmetic loss.
func TestRenderedConfigKeepsEveryFieldConfigHolds(t *testing.T) {
	_, cfg, _ := configFixture(t)

	m := newModel()
	body := m.renderTOML(cfg)
	got, err := config.Parse([]byte(body))
	if err != nil {
		t.Fatalf("the interface wrote a config it cannot read back: %v\n%s", err, body)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("written config does not validate: %v\n%s", err, body)
	}

	if got.Settings != cfg.Settings {
		t.Errorf("settings:\n got %+v\nwant %+v\n%s", got.Settings, cfg.Settings, body)
	}
	if !reflect.DeepEqual(got.Encryption, cfg.Encryption) {
		t.Errorf("encryption: got %+v, want %+v", got.Encryption, cfg.Encryption)
	}
	if !reflect.DeepEqual(got.Groups, cfg.Groups) {
		t.Errorf("groups:\n got %+v\nwant %+v\n%s", got.Groups, cfg.Groups, body)
	}
}

// An absolute path picked in the browser is written back as ~/ when it is under
// the home directory, so a saved configuration is not pinned to one machine.
func TestRenderedConfigWritesPathsRelativeToHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}

	m := newModel()
	m.mapping = []mapEntry{
		{path: filepath.Join(home, ".ssh"), dest: "dotfiles"},
		{path: "/etc/hosts", dest: "system"},
	}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatal(err)
	}

	body := m.renderTOML(cfg)
	if !strings.Contains(body, `"~/.ssh"`) {
		t.Errorf("a path under home was not contracted to ~/:\n%s", body)
	}
	if !strings.Contains(body, `"/etc/hosts"`) {
		t.Errorf("a path outside home should have been left alone:\n%s", body)
	}
}

// Replacing a configuration the user wrote by hand takes two presses of enter,
// because the first one is a question.
func TestSavingOverAnExistingConfigurationAsksFirst(t *testing.T) {
	m, root := reviewing(t)
	path := filepath.Join(root, "arca.toml")
	original := []byte("# hand written, do not lose me by accident\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	m.state = stateSaveConfig
	m.cfgInput.SetValue(path)

	send(m, "enter")
	if m.state != stateSaveConfig {
		t.Fatalf("the first enter left the screen: state = %v", m.state)
	}
	if m.confirmSave != path {
		t.Errorf("the first enter did not ask: confirmSave = %q", m.confirmSave)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, original) {
		t.Fatalf("the first enter already replaced the file:\n%s", now)
	}

	send(m, "enter")
	if m.state != stateReview {
		t.Fatalf("state = %v (err %v), want review", m.state, m.err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(written, original) {
		t.Fatal("the second enter did not write")
	}
	if _, err := config.Parse(written); err != nil {
		t.Errorf("what was written does not parse: %v\n%s", err, written)
	}
	if m.cfgPath != path || !m.fromDisk {
		t.Errorf("cfgPath = %q fromDisk = %v after saving", m.cfgPath, m.fromDisk)
	}
}

// A path with nothing at it needs no confirmation, and the directory is arca's
// own, shared with the identity, so it is created at 0700.
func TestSavingToANewPathWritesStraightAway(t *testing.T) {
	m, root := reviewing(t)
	path := filepath.Join(root, "fresh", "arca.toml")

	m.state = stateSaveConfig
	m.cfgInput.SetValue(path)

	send(m, "enter")
	if m.state != stateReview {
		t.Fatalf("state = %v (err %v), want review", m.state, m.err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nothing was written: %v", err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %04o, want 0700", perm)
	}
}

// Skipping the save has to leave the disk exactly as it was, and stop the rest
// of the interface claiming a file backs what it is about to run.
func TestSkippingTheSaveLeavesTheDiskAlone(t *testing.T) {
	m, root := reviewing(t)
	path := filepath.Join(root, "arca.toml")
	original := []byte("# untouched\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// Drive the real route in, so the state the save screen inherits is the
	// state an edit actually leaves behind.
	send(m, "e", "tab", "tab")
	if m.state != stateSaveConfig {
		t.Fatalf("state = %v (err %v), want the save screen", m.state, m.err)
	}
	m.cfgInput.SetValue(path)

	send(m, "ctrl+d")
	if m.state != stateReview {
		t.Fatalf("state = %v (err %v), want review", m.state, m.err)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, original) {
		t.Errorf("the file was written anyway:\n%s", now)
	}
	if m.fromDisk {
		t.Error("an unsaved configuration still claims a file backs it")
	}
	// It is still the configuration in hand, so the menu goes back to reviewing
	// it rather than starting over.
	if !m.hasConfig() {
		t.Error("the edited configuration was dropped along with the save")
	}
}

// esc out of an edit must not walk back through the screens that built it.
func TestReturningFromAnEditLandsOnTheReview(t *testing.T) {
	m, _ := reviewing(t)

	send(m, "e", "tab", "tab", "ctrl+d")
	if m.state != stateReview {
		t.Fatalf("state = %v, want review", m.state)
	}
	send(m, "esc")
	if m.state != stateMenu {
		t.Errorf("esc from the review led to %v, want the menu", m.state)
	}
}

// ---------- keys must not outlive the run that chose them ----------

// The passphrase typed to open somebody else's archive must never become the
// passphrase a later backup is encrypted with. Nothing on screen would say it
// had happened, and the archive would be locked behind a secret the user never
// chose for it.
func TestInspectingAnArchiveDoesNotSupplyTheNextBackupsKey(t *testing.T) {
	m, _ := reviewing(t)
	m.cfg.Encryption.Recipients = nil // passphrase mode, so nothing else can fill in
	m.adoptConfig(m.cfg)

	// Stand where unlocking an archive leaves the model, then walk back out.
	m.archiveKey = &codec.Keyring{Passphrase: []byte("the archive's passphrase")}
	m.state = stateArchiveInfo
	m.back = []state{stateMenu}
	send(m, "esc")
	if m.state != stateMenu {
		t.Fatalf("state = %v, want the menu", m.state)
	}

	send(m, "enter") // back up
	if m.state != stateReview {
		t.Fatalf("state = %v (err %v), want review", m.state, m.err)
	}
	send(m, "enter") // start it

	if m.backupKey != nil {
		t.Fatalf("a backup key appeared without a screen asking: %q",
			m.backupKey.Passphrase)
	}
	if m.state != stateCrypto {
		t.Errorf("state = %v, want the encryption screen to ask", m.state)
	}
}

// A finished run's key belongs to that run. Reusing it would encrypt a second
// archive with a secret chosen for the first, again without asking.
func TestAFinishedBackupDoesNotLendItsKeyToTheNext(t *testing.T) {
	m, _ := reviewing(t)
	m.cfg.Encryption.Recipients = nil
	m.adoptConfig(m.cfg)

	m.backupKey = &codec.Keyring{Passphrase: []byte("chosen for the first archive")}
	m.genPass = "chosen for the first archive"
	m.outPath = "/tmp/already-written.tar.zst.age"
	m.result = &archive.BackupResult{Path: m.outPath}
	m.state = stateDone

	send(m, "enter") // back to the menu
	if m.backupKey != nil {
		t.Errorf("the key outlived its run: %q", m.backupKey.Passphrase)
	}
	if m.genPass != "" {
		t.Errorf("a generated passphrase outlived the screen that showed it: %q", m.genPass)
	}
	// A reused output path names an archive that now exists, which the writer
	// refuses; the next run has to generate a fresh name.
	if m.outPath != "" {
		t.Errorf("the output path outlived its run: %q", m.outPath)
	}

	send(m, "enter") // back up again
	if m.state != stateReview {
		t.Fatalf("state = %v (err %v), want review", m.state, m.err)
	}
	send(m, "enter")
	if m.state != stateCrypto {
		t.Errorf("state = %v, want the encryption screen to ask again", m.state)
	}
}

// Recipients are the exception, and deliberately so: they are public keys the
// configuration itself names, so there is nothing to ask about.
func TestRecipientsInTheConfigurationStillNeedNoPrompt(t *testing.T) {
	m, _ := reviewing(t)
	if cmd := m.refreshPlan(); cmd != nil || m.err != nil {
		t.Fatalf("plan failed: %v", m.err)
	}

	send(m, "enter") // start the backup straight from the review
	if m.state == stateCrypto {
		t.Fatal("a configuration naming recipients should not ask for a key")
	}
	if m.backupKey == nil || len(m.backupKey.Recipients) != 1 {
		t.Fatalf("recipients did not become the backup key: %+v", m.backupKey)
	}
}
