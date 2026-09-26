package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/codec"
	"github.com/henriquemenezes/arca/internal/config"
)

// excludeTree is one chosen source with something inside it worth leaving out.
func excludeTree(t *testing.T) (root, source string) {
	t.Helper()
	root = t.TempDir()
	source = filepath.Join(root, "project")
	os.MkdirAll(filepath.Join(source, "src"), 0o755)
	os.MkdirAll(filepath.Join(source, "node_modules", "left-pad"), 0o755)
	os.WriteFile(filepath.Join(source, "src", "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(source, "node_modules", "left-pad", "index.js"), make([]byte, 4096), 0o644)
	return root, source
}

// chosen opens the source screen with one path already marked and the cursor
// standing on the thing inside it the test is about to exclude.
func chosen(t *testing.T, source, inside string) *model {
	t.Helper()
	m := sourcesModel(t, filepath.Dir(source), 100, 40)
	m.browser.selected[source] = true
	m.browser.load(source)
	for i, e := range m.browser.list() {
		if e.path == filepath.Join(source, inside) {
			m.browser.cursor = i
			return m
		}
	}
	t.Fatalf("%q is not listed under %q", inside, source)
	return nil
}

func TestExcludeDerivesThePatternFromTheChosenAncestor(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "x")

	if m.err != nil {
		t.Fatalf("x reported %v", m.err)
	}
	if got := m.srcExclude[source]; !reflect.DeepEqual(got, []string{"node_modules"}) {
		t.Errorf("patterns under %q are %v, want [node_modules]", source, got)
	}
}

func TestExcludeSpellsADeeperPathRelativeToItsSource(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	m.browser.load(filepath.Join(source, "node_modules"))
	m.browser.cursor = 0 // left-pad
	send(m, "x")

	if got := m.srcExclude[source]; !reflect.DeepEqual(got, []string{"node_modules/left-pad"}) {
		t.Errorf("patterns are %v, want [node_modules/left-pad]", got)
	}
}

// The deepest chosen path owns an exclusion under it, so a pattern is never
// written against a source that only happens to sit above the real one.
func TestExcludeAttachesToTheDeepestChosenAncestor(t *testing.T) {
	_, source := excludeTree(t)
	inner := filepath.Join(source, "src")

	m := chosen(t, source, "src")
	m.browser.selected[inner] = true
	m.browser.load(inner)
	m.browser.cursor = 0 // main.go
	send(m, "x")

	if got := m.srcExclude[inner]; !reflect.DeepEqual(got, []string{"main.go"}) {
		t.Errorf("patterns under %q are %v, want [main.go]", inner, got)
	}
	if got := m.srcExclude[source]; got != nil {
		t.Errorf("the outer source picked up %v, which belongs to the inner one", got)
	}
}

func TestExcludeRefusesWithoutAChosenAncestor(t *testing.T) {
	_, source := excludeTree(t)
	m := sourcesModel(t, source, 100, 40) // nothing selected at all
	send(m, "x")

	if m.err == nil {
		t.Fatal("excluding outside every chosen path was accepted")
	}
	if len(m.srcExclude) != 0 {
		t.Errorf("a pattern was written anyway: %v", m.srcExclude)
	}
}

func TestExcludeRefusesOnAChosenPathItself(t *testing.T) {
	root, source := excludeTree(t)
	m := sourcesModel(t, root, 100, 40)
	m.browser.selected[source] = true
	m.browser.cursor = 0 // project, the chosen path
	send(m, "x")

	if m.err == nil {
		t.Fatal("excluding a chosen path was accepted; it would contradict the selection")
	}
	if !strings.Contains(m.err.Error(), "space") {
		t.Errorf("the message does not say what to press instead: %v", m.err)
	}
}

func TestExcludeTogglesBackOff(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "x")
	m.browser.cursor = 0
	for i, e := range m.browser.list() {
		if filepath.Base(e.path) == "node_modules" {
			m.browser.cursor = i
		}
	}
	send(m, "x")

	if got := m.srcExclude[source]; len(got) != 0 {
		t.Errorf("patterns are %v, want none left", got)
	}
}

// The panel's number is a promise about the archive, so a walk that honours the
// patterns is the only honest way to produce it.
func TestExcludedPathsShrinkTheMeasuredSize(t *testing.T) {
	_, source := excludeTree(t)

	m := chosen(t, source, "node_modules")
	run(m, m.measureSelection())
	full := m.sizes[source]
	if full.err != nil {
		t.Fatalf("measuring %q failed: %v", source, full.err)
	}

	run(m, press(m, "x"))
	trimmed := m.sizes[source]
	if trimmed.err != nil {
		t.Fatalf("measuring after the exclusion failed: %v", trimmed.err)
	}
	if trimmed.bytes >= full.bytes || trimmed.files >= full.files {
		t.Errorf("the count did not drop: %d files/%d bytes became %d files/%d bytes",
			full.files, full.bytes, trimmed.files, trimmed.bytes)
	}
}

func TestAPatternTypedIntoThePanelReachesTheConfiguration(t *testing.T) {
	_, source := excludeTree(t)
	m := sourcesModel(t, filepath.Dir(source), 100, 40)
	m.browser.selected[source] = true

	send(m, "X")                // focus the panel
	send(m, "a")                // open the field
	typed(m, "*.iso")           //
	send(m, "enter")            //
	send(m, "ctrl+d")           // on to the mapping screen
	m.mapping[0].dest = "Work"  //
	cfg, err := m.buildConfig() //
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Groups[0].Exclude; !reflect.DeepEqual(got, []string{"*.iso"}) {
		t.Errorf("the group carries %v, want [*.iso]", got)
	}
}

func TestRemovingAPatternRemovesItFromTheConfiguration(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "x")

	send(m, "X")    // focus the panel: row 0 is the source, row 1 its pattern
	send(m, "down") //
	send(m, "d")    //

	if got := m.srcExclude[source]; len(got) != 0 {
		t.Fatalf("patterns are %v, want none left", got)
	}
	send(m, "ctrl+d")
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Groups[0].Exclude; len(got) != 0 {
		t.Errorf("the group still carries %v", got)
	}
}

func TestThePanelRefusesToRemoveASourceRow(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "x")
	send(m, "X") // the cursor starts on the source row
	send(m, "d")

	if m.err == nil {
		t.Fatal("d on a source row was accepted")
	}
	if got := m.srcExclude[source]; len(got) != 1 {
		t.Errorf("patterns are %v, want the one that was there", got)
	}
}

func TestThePanelRefusesAnAbsolutePattern(t *testing.T) {
	_, source := excludeTree(t)
	m := sourcesModel(t, filepath.Dir(source), 100, 40)
	m.browser.selected[source] = true

	send(m, "X", "a")
	typed(m, "/etc/passwd")
	send(m, "enter")

	if m.err == nil {
		t.Fatal("an absolute exclude pattern was accepted")
	}
	if len(m.srcExclude) != 0 {
		t.Errorf("it was stored anyway: %v", m.srcExclude)
	}
}

// Editing a configuration must hand back the exclude patterns it came in with,
// including for a group of several sources: they are attributed to each source
// on the way in and unioned per destination on the way out.
func TestGroupExcludesSurviveAnEditWithSeveralSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{".ssh", ".aws", "Work"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}

	before := &config.Config{
		Settings: config.Default().Settings,
		Groups: []config.Group{
			{
				Name:    "dotfiles",
				Dest:    "dotfiles",
				Sources: []config.Source{{Path: "~/.ssh"}, {Path: "~/.aws"}},
				Exclude: []string{"**/known_hosts.old", "*.pem"},
			},
			{
				Name:    "projects",
				Dest:    "Work",
				Sources: []config.Source{{Path: "~/Work"}},
				Exclude: []string{"**/node_modules"},
			},
		},
	}
	if err := before.Validate(); err != nil {
		t.Fatal(err)
	}

	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.cfg = before
	m.state = stateReview
	m.editConfig()
	send(m, "ctrl+d") // sources → mapping
	send(m, "enter")  // mapping → build

	after, err := m.buildConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Groups) != len(before.Groups) {
		t.Fatalf("groups went from %d to %d", len(before.Groups), len(after.Groups))
	}
	for i, g := range after.Groups {
		if want := before.Groups[i].Exclude; !reflect.DeepEqual(g.Exclude, want) {
			t.Errorf("group %q carries %v, want %v", g.Name, g.Exclude, want)
		}
	}
}

func TestTheExcludesPanelKeepsToItsColumn(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "x")
	send(m, "X")

	_, previewWidth := m.sourcesLayout()
	for _, line := range strings.Split(m.viewExcludes(previewWidth-4, 12), "\n") {
		if w := lipgloss.Width(line); w > previewWidth-4 {
			t.Errorf("line is %d columns wide, want at most %d: %q", w, previewWidth-4, line)
		}
	}
}

// Every mode has to fit, including the narrow terminal where the panel takes
// the whole width.
func TestTheSourceScreenFitsEveryModeAndWidth(t *testing.T) {
	root, source := excludeTree(t)

	for _, width := range []int{60, 80, 100} {
		for _, mode := range []sourcesMode{srcBrowse, srcPath, srcExcludes, srcExcludeInput} {
			m := sourcesModel(t, root, width, 30)
			m.browser.selected[source] = true
			// With a pattern, so both panels have their deepest row.
			m.addPattern(source, "**/node_modules/**/*.min.js")
			m.mode = mode
			if mode == srcPath {
				// The field is only drawn as a field while it holds the
				// keyboard, which is the state whose width is at stake.
				m.focusPath()
			}

			for _, line := range strings.Split(m.View(), "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("width %d, mode %v: line is %d columns: %q", width, mode, w, line)
				}
			}
		}
	}
}

// The whole point of the screen, checked against the only thing that settles
// it: what the archive ends up holding. The plan's count and the archive's
// count must agree, and neither may include what was excluded.
func TestAnExcludedPathNeverReachesTheArchive(t *testing.T) {
	_, source := excludeTree(t)
	out := t.TempDir()

	m := chosen(t, source, "node_modules")
	send(m, "x") // leave node_modules out
	send(m, "ctrl+d")
	send(m, "enter") // through the mapping screen, building the configuration

	m.outPath = filepath.Join(out, "from-tui.tar.zst.age")
	m.backupKey = &codec.Keyring{Passphrase: []byte("gaslight-tremor-unmasked-cufflink-shallot")}
	if cmd := m.refreshPlan(); cmd != nil || m.err != nil {
		t.Fatalf("planning failed: %v", m.err)
	}
	// src/main.go is all that is left; node_modules/left-pad/index.js is not.
	if m.plan.Stats.Files != 1 {
		t.Fatalf("the plan counts %d files, want 1", m.plan.Stats.Files)
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

	for _, g := range m.cfg.Groups {
		if !reflect.DeepEqual(g.Exclude, []string{"node_modules"}) {
			t.Errorf("group %q went into the backup carrying %v, want [node_modules]", g.Name, g.Exclude)
		}
	}

	res, err := archive.Verify(m.outPath, m.backupKey)
	if err != nil {
		t.Fatalf("the archive does not verify: %v", err)
	}
	if res.Stats.Files != 1 {
		t.Errorf("the archive holds %d files, want 1", res.Stats.Files)
	}
}

// A glob pattern is what the preset menu writes, and until the panel borrowed
// the walk's matcher the file list stayed dark for everything a preset had
// just excluded: the screen said one thing and the backup did another.
func TestExcludedLightsUpForAGlobPattern(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	m.setExclude(source, []string{"**/node_modules"})

	if !m.excluded(filepath.Join(source, "node_modules")) {
		t.Error("**/node_modules does not mark node_modules as excluded")
	}
	if m.excluded(filepath.Join(source, "src")) {
		t.Error("**/node_modules marks src as excluded")
	}
}

// x on something a glob already covers used to add a second, redundant
// pattern. It says what is already covering it instead, and where that one is
// removed, because x only undoes what x could have made.
func TestExcludeRefusesAPathAGlobAlreadyCovers(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	m.setExclude(source, []string{"**/node_modules"})
	send(m, "x")

	if m.err == nil {
		t.Fatal("x accepted a path that **/node_modules already covers")
	}
	if !strings.Contains(m.err.Error(), "**/node_modules") {
		t.Errorf("the message does not name the pattern: %v", m.err)
	}
	if got := m.srcExclude[source]; !reflect.DeepEqual(got, []string{"**/node_modules"}) {
		t.Errorf("patterns are %v, want the glob alone", got)
	}
}
