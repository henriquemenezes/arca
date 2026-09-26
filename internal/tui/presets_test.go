package tui

import (
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/henriquemenezes/arca/internal/walk"
)

// ---------- the catalogue ----------

func TestPresetCatalogueIsWellFormed(t *testing.T) {
	if len(presetGroups) == 0 {
		t.Fatal("the catalogue is empty")
	}
	names := map[string]bool{}
	for _, g := range presetGroups {
		if strings.TrimSpace(g.Name) == "" {
			t.Error("a group has no name")
		}
		if names[g.Name] {
			t.Errorf("two groups are called %q", g.Name)
		}
		names[g.Name] = true
		if len(g.Patterns) == 0 {
			t.Errorf("group %q has no patterns", g.Name)
		}
		seen := map[string]bool{}
		for _, p := range g.Patterns {
			if strings.TrimSpace(p.Glob) == "" {
				t.Errorf("group %q has an empty glob", g.Name)
			}
			if seen[p.Glob] {
				t.Errorf("group %q lists %q twice", g.Name, p.Glob)
			}
			seen[p.Glob] = true
		}
	}
}

// A generic directory name is one a project may legitimately keep source in,
// and leaving it out of a backup is lost data rather than saved space. The
// catalogue must not grow an unflagged one.
func TestEveryGenericNameIsMarkedBroad(t *testing.T) {
	generic := map[string]bool{
		"build": true, "bin": true, "out": true, "dist": true,
		"target": true, "vendor": true, "venv": true, "env": true,
		"deps": true, "lib": true, "src": true, "doc": true, "docs": true,
	}
	for _, g := range presetGroups {
		for _, p := range g.Patterns {
			if generic[path.Base(p.Glob)] && p.Warn != warnGeneric {
				t.Errorf("group %q: %q is a generic name and is not flagged as one", g.Name, p.Glob)
			}
		}
	}
}

func TestEveryGlobIsRelativeAndSlashed(t *testing.T) {
	for _, g := range presetGroups {
		for _, p := range g.Patterns {
			if strings.HasPrefix(p.Glob, "/") {
				t.Errorf("group %q: %q is absolute", g.Name, p.Glob)
			}
			if strings.Contains(p.Glob, `\`) {
				t.Errorf("group %q: %q is not slash-separated", g.Name, p.Glob)
			}
		}
	}
}

// ---------- rows ----------

func TestPresetRowsListGroupsUntilOneIsOpened(t *testing.T) {
	p := newPresetPicker("/src", false)
	if got := len(p.rows()); got != len(presetGroups) {
		t.Errorf("closed, the list is %d rows, want %d", got, len(presetGroups))
	}

	p.open[1] = true
	want := len(presetGroups) + len(presetGroups[1].Patterns)
	if got := len(p.rows()); got != want {
		t.Errorf("with group 1 open, the list is %d rows, want %d", got, want)
	}
	if r := p.rows()[2]; r.group != 1 || r.pattern != 0 {
		t.Errorf("the row after an opened group is %+v, want the group's first pattern", r)
	}
}

// ---------- selection ----------

func TestSpaceOnAGroupPicksAllOfIt(t *testing.T) {
	p := newPresetPicker("/src", false)
	p.toggle(presetRow{group: 0, pattern: -1})

	if p.state(0) != presetAll {
		t.Errorf("group 0 reads %v after being picked, want all", p.state(0))
	}
	for _, pat := range presetGroups[0].Patterns {
		if !p.picked[pat.Glob] {
			t.Errorf("%q was not picked", pat.Glob)
		}
	}

	p.toggle(presetRow{group: 0, pattern: -1})
	if p.state(0) != presetNone {
		t.Errorf("group 0 reads %v after being unpicked, want none", p.state(0))
	}
}

func TestUnpickingOnePatternLeavesTheGroupPartial(t *testing.T) {
	p := newPresetPicker("/src", false)
	p.toggle(presetRow{group: 0, pattern: -1})
	p.toggle(presetRow{group: 0, pattern: 0})

	if p.state(0) != presetSome {
		t.Errorf("group 0 reads %v, want some", p.state(0))
	}
	if p.picked[presetGroups[0].Patterns[0].Glob] {
		t.Error("the pattern is still picked")
	}
}

// ---------- the count ----------

// groupNamed finds a group by name, so a test can say what it means rather
// than carry an index that the next entry in the catalogue would shift.
func groupNamed(t *testing.T, name string) int {
	t.Helper()
	for i, g := range presetGroups {
		if g.Name == name {
			return i
		}
	}
	t.Fatalf("no group called %q", name)
	return -1
}

// build° is in six groups and out° in four, so the sum of the headers is not
// what pressing enter would add. Java and Kotlin are the sharpest case: five
// patterns each, six between them.
func TestTheCountIsTheUnionAndNotTheSum(t *testing.T) {
	p := newPresetPicker("/src", false)
	java, kotlin := groupNamed(t, "Java"), groupNamed(t, "Kotlin")
	p.toggle(presetRow{group: java, pattern: -1})
	p.toggle(presetRow{group: kotlin, pattern: -1})

	sum := len(presetGroups[java].Patterns) + len(presetGroups[kotlin].Patterns)
	got := len(p.additions(nil))
	if got == sum {
		t.Fatalf("the count is the sum of the headers (%d); the groups overlap", sum)
	}
	if got != 6 {
		t.Errorf("Java and Kotlin together add %d patterns, want 6", got)
	}
}

func TestTheCountLeavesOutWhatTheSourceAlreadyHas(t *testing.T) {
	p := presetPicker{picked: map[string]bool{"**/node_modules": true, "**/target": true}}
	if got := p.additions([]string{"**/target"}); !reflect.DeepEqual(got, []string{"**/node_modules"}) {
		t.Errorf("additions are %v, want [**/node_modules] alone", got)
	}
}

// ---------- applying ----------

func TestApplyWritesToTheCursorsSourceAlone(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	other := t.TempDir()
	m.browser.selected[other] = true

	m.preset = newPresetPicker(source, false)
	m.preset.picked = map[string]bool{"**/node_modules": true}
	m.applyPreset()

	if got := m.srcExclude[source]; !reflect.DeepEqual(got, []string{"**/node_modules"}) {
		t.Errorf("patterns under the source are %v", got)
	}
	if got := m.srcExclude[other]; got != nil {
		t.Errorf("the other source got %v, want nothing", got)
	}
}

func TestApplyReachesEverySourceUnderAllScope(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	other := t.TempDir()
	m.browser.selected[other] = true

	m.preset = newPresetPicker(source, true)
	m.preset.picked = map[string]bool{"**/node_modules": true}
	m.applyPreset()

	for _, src := range []string{source, other} {
		if got := m.srcExclude[src]; !reflect.DeepEqual(got, []string{"**/node_modules"}) {
			t.Errorf("patterns under %q are %v", src, got)
		}
	}
}

func TestApplyDoesNotRepeatAPatternTheSourceHad(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	m.setExclude(source, []string{"**/node_modules"})

	m.preset = newPresetPicker(source, false)
	m.preset.picked = map[string]bool{"**/node_modules": true, "**/dist": true}
	m.applyPreset()

	want := []string{"**/node_modules", "**/dist"}
	if got := m.srcExclude[source]; !reflect.DeepEqual(got, want) {
		t.Errorf("patterns are %v, want %v", got, want)
	}
}

// ---------- the keyboard ----------

// openPresets reaches the menu the way a user does: X for the panel, A for the
// ready-made patterns.
func openPresets(t *testing.T) (*model, string) {
	t.Helper()
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "X", "A")
	if m.mode != srcPreset {
		t.Fatalf("A left the screen in mode %v, want srcPreset", m.mode)
	}
	return m, source
}

func TestAOpensThePresetMenuOnTheCursorsSource(t *testing.T) {
	m, source := openPresets(t)
	if m.preset.source != source {
		t.Errorf("the menu opened on %q, want %q", m.preset.source, source)
	}
	if m.preset.allSrc {
		t.Error("the menu opened on every source; this source is the default")
	}
}

func TestAIsNotOfferedFromTheFileList(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "A")
	if m.mode == srcPreset {
		t.Error("A opened the preset menu from the file list; X comes first")
	}
}

func TestTabTogglesTheScope(t *testing.T) {
	m, _ := openPresets(t)
	send(m, "tab")
	if !m.preset.allSrc {
		t.Error("tab did not widen the scope to every source")
	}
	send(m, "tab")
	if m.preset.allSrc {
		t.Error("tab did not narrow the scope back")
	}
}

func TestRightOpensAGroupAndLeftClosesIt(t *testing.T) {
	m, _ := openPresets(t)
	send(m, "right")
	if !m.preset.open[0] {
		t.Error("right did not open the group under the cursor")
	}
	send(m, "left")
	if m.preset.open[0] {
		t.Error("left did not close it")
	}
}

func TestSpaceAndEnterAddThePickedPatterns(t *testing.T) {
	m, source := openPresets(t)
	send(m, " ", "enter")

	if m.mode != srcExcludes {
		t.Errorf("enter left the screen in mode %v, want the excludes panel", m.mode)
	}
	want := len(presetGroups[0].Patterns)
	if got := len(m.srcExclude[source]); got != want {
		t.Errorf("%d patterns landed under the source, want %d", got, want)
	}
}

func TestEscLeavesThePatternsAlone(t *testing.T) {
	m, source := openPresets(t)
	send(m, " ", "esc")

	if m.mode != srcExcludes {
		t.Errorf("esc left the screen in mode %v, want the excludes panel", m.mode)
	}
	if got := m.srcExclude[source]; got != nil {
		t.Errorf("esc added %v", got)
	}
}

// Enter with nothing picked would be a no-op that looks like a mistake.
func TestEnterWithNothingPickedSaysSo(t *testing.T) {
	m, _ := openPresets(t)
	send(m, "enter")
	if m.err == nil {
		t.Error("enter with nothing picked reported nothing")
	}
}

// ---------- the panel ----------

func TestThePresetPanelNamesItsGroupsAndScope(t *testing.T) {
	m, _ := openPresets(t)
	got := plain(m.viewPresets(60, 18))

	for _, want := range []string{"Preset excludes", "apply to", "this source", "JavaScript", "Python"} {
		if !strings.Contains(got, want) {
			t.Errorf("the panel does not say %q:\n%s", want, got)
		}
	}
}

// A generic name is the one entry in an opened group worth a second look.
func TestAnOpenedGroupFlagsItsGenericNames(t *testing.T) {
	m, _ := openPresets(t)
	m.preset.cursor = groupNamed(t, "Python")
	send(m, "right")

	got := plain(m.viewPresets(60, 30))
	if !strings.Contains(got, "venv") {
		t.Fatalf("the opened group does not list its patterns:\n%s", got)
	}
	if !strings.Contains(got, "generic name") {
		t.Errorf("the opened group does not flag env as a generic name:\n%s", got)
	}
}

// Sixteen groups do not fit a panel, so the list must not appear to end where
// the window does.
func TestThePresetPanelSaysHowMuchIsBelow(t *testing.T) {
	m, _ := openPresets(t)
	got := plain(m.viewPresets(60, 10))
	if !strings.Contains(got, "more") {
		t.Errorf("the panel does not say the list continues:\n%s", got)
	}
}

func TestThePresetFooterCountsWhatEnterWouldAdd(t *testing.T) {
	m, _ := openPresets(t)
	send(m, " ")
	got := plain(m.viewPresets(60, 18))
	if !strings.Contains(got, "adds 22") {
		t.Errorf("the footer does not count the 22 patterns of the first group:\n%s", got)
	}
}

// column is where r is drawn, counted in cells rather than in bytes.
func column(s string, r rune) int {
	for i, have := range []rune(s) {
		if have == r {
			return i
		}
	}
	return -1
}

// The cursor must not shift the column its row is drawn in; a list that moves
// sideways as you walk down it is a list that is hard to read.
func TestThePresetCursorKeepsTheColumns(t *testing.T) {
	m, _ := openPresets(t)
	m.preset.cursor = 0
	send(m, "right")

	m.preset.cursor = 1 // the group's first pattern
	onCursor := plain(m.presetLine(presetRow{group: 0, pattern: 0}, true, 56))
	off := plain(m.presetLine(presetRow{group: 0, pattern: 0}, false, 56))

	// Columns, not bytes: the cursor mark is three bytes wide and one column.
	if column(onCursor, '[') != column(off, '[') {
		t.Errorf("the checkbox moves with the cursor:\n%q\n%q", onCursor, off)
	}
}

// tab is the one key this panel adds to the browser's grammar, and the footer
// is too narrow to be relied on for it.
func TestTheScopeLineNamesTheKeyThatChangesIt(t *testing.T) {
	m, _ := openPresets(t)
	if got := plain(m.presetScopeLine(56)); !strings.Contains(got, "tab") {
		t.Errorf("the scope line does not name tab: %q", got)
	}
}

// Trailing blanks on a row are invisible until the panel is the whole screen,
// and then they are a ragged right edge.
func TestPresetRowsDoNotTrailBlanks(t *testing.T) {
	m, _ := openPresets(t)
	// node_modules carries no note: nothing to pad out to.
	line := plain(m.presetLine(presetRow{group: 0, pattern: 0}, false, 56))
	if line != strings.TrimRight(line, " ") {
		t.Errorf("a pattern row with no note trails blanks: %q", line)
	}
}

// ---------- out the other end ----------

// The patterns are held per source and unioned per destination, so the whole
// point of the menu is what buildConfig makes of it.
func TestPickedPatternsReachTheConfiguration(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "X", "A")
	m.preset.cursor = groupNamed(t, "Rust")
	send(m, " ", "enter")

	m.mapping = []mapEntry{{path: source, dest: "code"}}
	cfg, err := m.buildConfig()
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	if len(cfg.Groups) != 1 {
		t.Fatalf("the configuration has %d groups, want 1", len(cfg.Groups))
	}
	if !reflect.DeepEqual(cfg.Groups[0].Exclude, []string{"**/target", "**/.rs.bk", "**/*.rs.bk"}) {
		t.Errorf("the group carries %v", cfg.Groups[0].Exclude)
	}
}

// At a hundred columns the footer has room for three of this panel's keys, and
// A is the one nobody guesses. The subtitle is always drawn, so it is where
// the panel says the menu exists.
func TestTheExcludesPanelAdvertisesThePresetMenu(t *testing.T) {
	_, source := excludeTree(t)
	m := chosen(t, source, "node_modules")
	send(m, "X")

	if got := plain(m.View()); !strings.Contains(got, "A ") || !strings.Contains(got, "ready-made") {
		t.Errorf("the excludes panel does not mention the preset menu:\n%s", got)
	}
}

// ---------- the narrow panel ----------

// A truncated row must keep its checkbox: the box is the state, and a row that
// drops it to fit says nothing at all.
func TestANarrowRowKeepsItsCheckbox(t *testing.T) {
	m, _ := openPresets(t)
	for _, width := range []int{34, 28, 22} {
		for _, r := range []presetRow{{group: 0, pattern: -1}, {group: 0, pattern: 0}} {
			line := plain(m.presetLine(r, false, width))
			if !strings.Contains(line, "[") {
				t.Errorf("at %d columns, %+v drew no checkbox: %q", width, r, line)
			}
			if lipgloss.Width(line) > width {
				t.Errorf("at %d columns, %+v is %d wide: %q", width, r, lipgloss.Width(line), line)
			}
		}
	}
}

// The scope line is one line. Wrapped onto two it costs the list a row and
// reads as a sentence that lost its way.
func TestTheScopeLineFitsOnOneLine(t *testing.T) {
	m, _ := openPresets(t)
	for _, width := range []int{56, 40, 34, 28} {
		line := plain(m.presetScopeLine(width))
		if lipgloss.Width(line) > width {
			t.Errorf("at %d columns the scope line is %d wide: %q", width, lipgloss.Width(line), line)
		}
		if !strings.Contains(line, "tab") {
			t.Errorf("at %d columns the scope line lost tab: %q", width, line)
		}
	}
}

// ---------- coverage ----------

// covered reports the catalogue entry that would leave d out, if any.
func covered(d string) (string, bool) {
	base := d
	if i := strings.LastIndex(d, "/"); i >= 0 {
		base = d[i+1:]
	}
	for _, g := range presetGroups {
		for _, p := range g.Patterns {
			if walk.Matches([]string{p.Glob}, d, base) {
				return p.Glob, true
			}
		}
	}
	return "", false
}

func TestTheCatalogueCoversTheCommonHomeDirectories(t *testing.T) {
	for _, d := range []string{
		".local", ".npm", ".cache", ".ollama", ".next", ".ruff_cache",
		".pytest_cache", ".config", ".rustup", ".vscode", "go/pkg",
		"disk.iso", "setup.exe", ".oh-my-bash",
	} {
		if _, ok := covered(d); !ok {
			t.Errorf("nothing in the catalogue leaves out %q", d)
		}
	}
}

// ---------- warnings ----------

// A directory that holds real data is not the same warning as a directory
// whose name is generic, and a backup is the wrong place to conflate them:
// .config is usually the most valuable thing in a home directory.
func TestTheTreesThatHoldRealDataSaySo(t *testing.T) {
	want := map[string]bool{"**/.config": true, "**/.local": true}
	seen := map[string]bool{}
	for _, g := range presetGroups {
		for _, p := range g.Patterns {
			if !want[p.Glob] {
				continue
			}
			seen[p.Glob] = true
			if p.Warn == "" {
				t.Errorf("%q carries no warning", p.Glob)
			}
			if p.Warn == warnGeneric {
				t.Errorf("%q is warned about as a generic name, which is not what it is", p.Glob)
			}
		}
	}
	for g := range want {
		if !seen[g] {
			t.Errorf("%q is not in the catalogue", g)
		}
	}
}

// Nothing that holds real data may be picked by a group that also holds
// throwaway caches: the group toggle picks all of it, so the two must not
// share one.
func TestRealDataDoesNotShareAGroupWithCaches(t *testing.T) {
	for _, g := range presetGroups {
		keeps, plain := 0, 0
		for _, p := range g.Patterns {
			if p.Warn == warnKeeps {
				keeps++
			} else {
				plain++
			}
		}
		if keeps > 0 && plain > 0 {
			t.Errorf("group %q mixes %d trees holding real data with %d other patterns",
				g.Name, keeps, plain)
		}
	}
}
