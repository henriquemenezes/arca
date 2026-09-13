package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hamsa/arca/internal/archive"
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

func TestSuggestDestGroupsDotfiles(t *testing.T) {
	for path, want := range map[string]string{
		"/home/u/.ssh":      "dotfiles",
		"/home/u/.aws":      "dotfiles",
		"/home/u/Downloads": "Downloads",
		"/home/u/Work":      "Work",
	} {
		if got := suggestDest(path); got != want {
			t.Errorf("suggestDest(%q) = %q, want %q", path, got, want)
		}
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
	b.Update(key("up"))
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
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "arca", "identity.age")); err != nil {
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
	m.keyring = &codec.Keyring{Passphrase: []byte("gaslight-tremor-unmasked-cufflink-shallot")}

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
		Keyring:     m.keyring,
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

	res, err := archive.Verify(m.outPath, m.keyring)
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
		stateRecipients, stateOutput, stateReview, stateRunning, stateDone,
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
