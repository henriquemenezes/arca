package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/henriquemenezes/arca/internal/cli"
)

// Ready-made exclude patterns, offered by A on the excludes panel.
//
// The panel could already add one pattern at a time, typed by hand, which is
// the right primitive and the wrong first step. The patterns almost everyone
// wants first are the same twenty or thirty, they are the ones nobody
// remembers whole, and spelling "**/__pycache__" correctly on the fourth try
// is not what the screen is for.
//
// The catalogue is data in this file rather than a format on disk: changing it
// is a commit, which is also what reviewing it is.

// presetPattern is one glob and, when it needs one, the warning the panel puts
// beside it.
//
// A warning changes neither what the pattern matches nor how it is selected —
// picking a group picks all of it, because a checkbox that picks most of a
// group is a lie. It only announces itself when the group is open, the way the
// panel already says "(any depth)" rather than changing what the matcher does.
// In a backup, leaving out a directory by mistake is lost data and not saved
// space, so the ones worth a second look say so.
//
// The two warnings are not the same thing. A generic name is a directory a
// project might keep source under — build, bin, venv — and is usually build
// output anyway. A tree that holds real data is not build output at all: it is
// kept out only by someone who knows they are keeping it out.
type presetPattern struct {
	Glob string
	Warn string
}

const (
	warnGeneric = "generic name"
	warnKeeps   = "holds real data"
)

type presetGroup struct {
	Name     string
	Patterns []presetPattern
}

// glob, broad and keeps build the entries, so the catalogue below reads as the
// list it is rather than as a wall of struct literals.
func glob(g string) presetPattern  { return presetPattern{Glob: "**/" + g} }
func broad(g string) presetPattern { return presetPattern{Glob: "**/" + g, Warn: warnGeneric} }
func keeps(g string) presetPattern { return presetPattern{Glob: "**/" + g, Warn: warnKeeps} }

// presetGroups is one group per language, then the six that are not a
// language: the tooling and editor leftovers, the caches and frameworks a home
// directory accumulates, the files that are re-downloaded rather than
// restored, and the two trees that hold real data and are kept apart for it.
//
// Pairs that share a whole toolchain stay together — splitting C from C++, or
// Swift from Objective-C, would produce two groups with one list and no
// decision between them. Java and Kotlin are the edge case in the other
// direction: distinct languages, so distinct groups, with lists that overlap
// almost entirely.
//
// What is deliberately absent, and what separates this from a .gitignore:
// secrets and environment files — .env, *.pem, *.key, credentials. Git ignores
// them; a backup is the one place you want them kept.
var presetGroups = []presetGroup{
	{"JavaScript · TypeScript", []presetPattern{
		glob("node_modules"), glob("bower_components"), glob(".next"), glob(".nuxt"),
		glob(".svelte-kit"), glob(".angular"), glob(".astro"), glob(".docusaurus"),
		glob(".output"), glob(".turbo"), glob(".parcel-cache"), glob(".vite"),
		glob(".eslintcache"), glob(".yarn/cache"), glob(".pnpm-store"), glob(".pnp.*"),
		glob("coverage"), glob(".nyc_output"), glob(".cache"),
		broad("dist"), broad("build"), broad("out"),
	}},
	{"Python", []presetPattern{
		glob("__pycache__"), glob(".venv"), broad("venv"), broad("env"), glob("virtualenv"),
		glob(".tox"), glob(".nox"), glob(".mypy_cache"), glob(".pytest_cache"),
		glob(".ruff_cache"), glob(".pytype"), glob(".eggs"), glob("*.egg-info"),
		glob(".ipynb_checkpoints"), glob("htmlcov"), glob(".coverage"),
		broad("build"), broad("dist"),
	}},
	{"Go", []presetPattern{
		broad("vendor"), broad("dist"), broad("bin"),
	}},
	{"Rust", []presetPattern{
		broad("target"), glob(".rs.bk"), glob("*.rs.bk"),
	}},
	{"Java", []presetPattern{
		broad("target"), broad("build"), broad("out"), broad("bin"), glob(".gradle"),
	}},
	{"Kotlin", []presetPattern{
		broad("build"), glob(".gradle"), glob(".kotlin"), broad("out"), broad("target"),
	}},
	{"C# · .NET", []presetPattern{
		broad("bin"), glob("obj"), glob("packages"), glob("TestResults"), glob(".vs"),
	}},
	{"C · C++", []presetPattern{
		broad("build"), glob("cmake-build-*"), glob("CMakeFiles"), glob(".ccls-cache"),
		glob(".clangd"), broad("out"),
	}},
	{"Ruby", []presetPattern{
		glob(".bundle"), glob("vendor/bundle"), glob(".yardoc"), glob("tmp/cache"),
		glob("coverage"),
	}},
	{"PHP", []presetPattern{
		broad("vendor"), glob(".phpunit.cache"), glob(".phpunit.result.cache"),
		glob("var/cache"),
	}},
	{"Elixir", []presetPattern{
		glob("_build"), broad("deps"), glob(".elixir_ls"), glob("cover"),
	}},
	{"Dart · Flutter", []presetPattern{
		glob(".dart_tool"), glob(".pub-cache"), glob(".flutter-plugins"), broad("build"),
	}},
	{"Swift · Objective-C", []presetPattern{
		glob("Pods"), glob("DerivedData"), glob(".build"), glob(".swiftpm"),
		glob("*.xcworkspace/xcuserdata"), glob("*.xcodeproj/xcuserdata"),
	}},
	{"Infra · tooling", []presetPattern{
		glob(".terraform"), glob(".terraform.lock.hcl"), glob(".terragrunt-cache"),
		glob(".vagrant"), glob(".serverless"), glob(".pulumi"),
	}},
	{"Editors · OS", []presetPattern{
		glob(".idea"), glob(".vscode"), glob(".vs"), glob(".DS_Store"), glob("._*"),
		glob("Thumbs.db"), glob("desktop.ini"), glob("lost+found"), glob(".Trash"),
		glob(".Trash-*"),
	}},
	// The toolchain directories are named whole rather than one subdirectory
	// each: .npm, .rustup and go/pkg are re-downloaded in their entirety by
	// the command that installs them, so naming .npm/_cacache alone left the
	// rest of a regenerable tree in the backup for no reason.
	{"User caches", []presetPattern{
		glob(".cache"), glob(".npm"), glob(".yarn/cache"), glob(".cargo/registry"),
		glob(".cargo/git"), glob(".rustup"), glob(".gradle/caches"),
		glob(".m2/repository"), glob(".ivy2/cache"), glob(".nuget/packages"),
		glob("go/pkg"), glob(".nvm/versions"), glob(".pyenv/versions"),
		glob(".local/share/Trash"), glob(".ollama"),
	}},
	// Frameworks that were cloned or installed by a script, and come back the
	// same way.
	{"Shell frameworks", []presetPattern{
		glob(".oh-my-bash"), glob(".oh-my-zsh"), glob(".oh-my-posh"), glob(".bash_it"),
		glob(".antigen"), glob(".zinit"), glob(".zplug"), glob(".fzf"),
		glob(".sdkman/candidates"),
	}},
	// Installers and media, which are re-downloaded rather than restored. Disk
	// images a virtual machine runs on — .vdi, .qcow2, .vmdk — are deliberately
	// absent: those are real data wearing a large-file shape.
	{"Installers · images", []presetPattern{
		glob("*.iso"), glob("*.img"), glob("*.dmg"), glob("*.exe"), glob("*.msi"),
		glob("*.deb"), glob("*.rpm"), glob("*.pkg"),
	}},
	// Kept apart from everything above, and flagged, because they are not
	// build output or cache: .config is usually the most valuable thing in a
	// home directory, and .local carries share/ and bin/. A group toggle picks
	// all of a group, so these must not sit in one beside things that are
	// merely large.
	{"Home dotfile trees", []presetPattern{
		keeps(".config"), keeps(".local"),
	}},
}

// ---------- the picker ----------

// presetPicker is what A opens. The picked globs are the one piece of state:
// a group's checkbox is read back from them rather than stored beside them, so
// there are not two answers to keep in agreement.
type presetPicker struct {
	source string          // the source the cursor was on when A opened
	cursor int             // index into rows()
	open   map[int]bool    // which groups are expanded
	picked map[string]bool // glob -> picked
	allSrc bool            // this source, or every chosen one
}

func newPresetPicker(source string, allSrc bool) presetPicker {
	return presetPicker{
		source: source,
		open:   map[int]bool{},
		picked: map[string]bool{},
		allSrc: allSrc,
	}
}

// presetRow is one line: a group, or one pattern under an opened group.
type presetRow struct {
	group   int
	pattern int // -1 when the row is the group itself
}

func (r presetRow) isGroup() bool { return r.pattern < 0 }

// rows flattens the catalogue the way excludeRows flattens the sources, which
// is what the cursor moves over.
func (p presetPicker) rows() []presetRow {
	rows := make([]presetRow, 0, len(presetGroups))
	for i, g := range presetGroups {
		rows = append(rows, presetRow{group: i, pattern: -1})
		if !p.open[i] {
			continue
		}
		for j := range g.Patterns {
			rows = append(rows, presetRow{group: i, pattern: j})
		}
	}
	return rows
}

// presetState is how much of a group is picked.
type presetState int

const (
	presetNone presetState = iota
	presetSome
	presetAll
)

func (p presetPicker) state(group int) presetState {
	n := 0
	for _, pat := range presetGroups[group].Patterns {
		if p.picked[pat.Glob] {
			n++
		}
	}
	switch n {
	case 0:
		return presetNone
	case len(presetGroups[group].Patterns):
		return presetAll
	}
	return presetSome
}

// toggle is the space key. On a group it picks all of it, or unpicks all of it
// when it is already whole; on a pattern it picks that one.
func (p *presetPicker) toggle(r presetRow) {
	if !r.isGroup() {
		glob := presetGroups[r.group].Patterns[r.pattern].Glob
		if p.picked[glob] {
			delete(p.picked, glob)
		} else {
			p.picked[glob] = true
		}
		return
	}
	pick := p.state(r.group) != presetAll
	for _, pat := range presetGroups[r.group].Patterns {
		if pick {
			p.picked[pat.Glob] = true
		} else {
			delete(p.picked, pat.Glob)
		}
	}
}

// additions is what pressing enter would actually add: the picked globs minus
// the ones have already carries.
//
// It is a union and not a sum. "**/build" belongs to six groups and "**/out"
// to four, so adding up the counts in the group headers would overstate what
// enter does — picking Java and Kotlin adds six patterns, not ten. The order
// follows the catalogue, so the same choice always produces the same file.
func (p presetPicker) additions(have []string) []string {
	existing := make(map[string]bool, len(have))
	for _, h := range have {
		existing[h] = true
	}
	var add []string
	seen := map[string]bool{}
	for _, g := range presetGroups {
		for _, pat := range g.Patterns {
			if !p.picked[pat.Glob] || existing[pat.Glob] || seen[pat.Glob] {
				continue
			}
			seen[pat.Glob] = true
			add = append(add, pat.Glob)
		}
	}
	return add
}

// targets is which sources enter writes to.
func (m *model) presetTargets() []string {
	if m.preset.allSrc {
		return m.browser.Selected()
	}
	if m.preset.source == "" {
		return nil
	}
	return []string{m.preset.source}
}

// presetAdditions is the count the footer states, which is the one the user is
// about to get. Under "all sources" the sources rarely hold the same patterns,
// so it answers for the source that would gain the most rather than for any
// one of them.
func (m *model) presetAdditions() int {
	n := 0
	for _, src := range m.presetTargets() {
		if a := len(m.preset.additions(m.srcExclude[src])); a > n {
			n = a
		}
	}
	return n
}

// applyPreset writes the picked patterns into every target source. setExclude
// forgets each source's measured size, so the totals recount on the way back.
func (m *model) applyPreset() {
	for _, src := range m.presetTargets() {
		have := append([]string(nil), m.srcExclude[src]...)
		m.setExclude(src, appendUnique(have, m.preset.additions(have)))
	}
}

// presetLabel is a glob as the panel prints it: without the "**/" every entry
// carries, since a column of identical prefixes is a column of noise.
func presetLabel(g string) string { return strings.TrimPrefix(g, "**/") }

// ---------- the panel ----------

// viewPresets draws the menu where viewExcludes draws, for the same reason:
// while it has the keyboard, it is what the screen is about.
func (m *model) viewPresets(width, rows int) string {
	list := m.preset.rows()
	cursor := m.presetCursor(list)

	// The title and the blank line under it, the scope line and its blank
	// line, and the two footer lines with their blank line above them.
	listRows := maxInt(1, rows-7)

	start := 0
	if cursor >= listRows {
		start = cursor - listRows + 1
	}
	end := minInt(len(list), start+listRows)

	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		lines = append(lines, m.presetLine(list[i], i == cursor, width))
	}
	for len(lines) < listRows {
		lines = append(lines, "")
	}
	// Sixteen groups do not fit a panel, and one opened group need not either,
	// so the list says how much of itself is out of sight rather than letting
	// the window pass for the end of it.
	if below := len(list) - end; below > 0 {
		lines[len(lines)-1] = padTo("", maxInt(0, width-10)) +
			stMuted.Render(fmt.Sprintf("↓ %d more", below))
	}

	n := m.presetAdditions()
	foot := "nothing picked yet"
	if n > 0 {
		foot = fmt.Sprintf("enter adds %s", cli.Count(n, "pattern", "patterns"))
	}

	return stTitle.Render("Preset excludes") + "\n\n" +
		m.presetScopeLine(width) + "\n\n" +
		strings.Join(lines, "\n") + "\n\n" +
		stMuted.Render(wrap("Patterns apply to every source sharing a destination.", width)) + "\n" +
		stEmph.Render(foot)
}

// presetScopeLine says where enter would write, which is the one thing about
// this panel that is not visible in the list.
func (m *model) presetScopeLine(width int) string {
	// The key is named here rather than left to the footer, which is the first
	// place a narrow terminal drops a key from, and this is the one key the
	// panel adds to the browser's grammar. The line gives up its words before
	// it gives up tab, and it never takes a second row: wrapped, it costs the
	// list a line and reads as a sentence that lost its way.
	n := len(m.browser.selected)
	for _, f := range []struct{ lead, here, all string }{
		{"apply to: ", "this source", fmt.Sprintf("all sources (%d)", n)},
		{"apply to: ", "this source", fmt.Sprintf("all (%d)", n)},
		{"", "this source", fmt.Sprintf("all (%d)", n)},
		{"", "this", fmt.Sprintf("all (%d)", n)},
	} {
		line := f.lead
		if m.preset.allSrc {
			line += f.here + "  [" + f.all + "]"
		} else {
			line += "[" + f.here + "]  " + f.all
		}
		if pad := width - lipgloss.Width(line) - 3; pad >= 1 {
			return stMuted.Render(f.lead) + m.presetScopeMarks(f) + strings.Repeat(" ", pad) +
				stMuted.Render("tab")
		}
	}
	// Narrower than any of those: the state alone, which is the part that
	// changes what enter does.
	one := "this"
	if m.preset.allSrc {
		one = fmt.Sprintf("all (%d)", n)
	}
	return stSelected.Render("["+one+"]") + " " + stMuted.Render("tab")
}

// presetCursor clamps the cursor to the rows that exist, which changes as
// groups open and close under it.
func (m *model) presetCursor(rows []presetRow) int {
	if m.preset.cursor >= len(rows) {
		return maxInt(0, len(rows)-1)
	}
	if m.preset.cursor < 0 {
		return 0
	}
	return m.preset.cursor
}

// presetBox is the checkbox, which for a group is read back from its patterns.
func presetBox(s presetState) string {
	switch s {
	case presetAll:
		return "[x]"
	case presetSome:
		return "[-]"
	}
	return "[ ]"
}

func (m *model) presetLine(row presetRow, onCursor bool, width int) string {
	if row.isGroup() {
		g := presetGroups[row.group]
		fold := " "
		if m.preset.open[row.group] {
			fold = "▾"
		}
		count := cli.Count(len(g.Patterns), "pattern", "patterns")
		label := fmt.Sprintf("%s %s %s", fold, presetBox(m.preset.state(row.group)), g.Name)
		return presetRowLine(label, count, onCursor, width)
	}

	pat := presetGroups[row.group].Patterns[row.pattern]
	mark := "[ ]"
	if m.preset.picked[pat.Glob] {
		mark = "[x]"
	}
	label := "   " + mark + " " + presetLabel(pat.Glob)

	// Said, not enforced: picking a group picks all of it. In a backup,
	// leaving out a directory by mistake is lost data, so the names a project
	// might legitimately keep source under say so where they are read.
	note := pat.Warn
	if m.presetAlreadyHas(pat.Glob) {
		note = "already set"
	}
	return presetRowLine(label, note, onCursor, width)
}

// presetAlreadyHas reports whether every source enter would write to carries
// this glob already, which is what makes it an annotation rather than an
// addition.
func (m *model) presetAlreadyHas(g string) bool {
	targets := m.presetTargets()
	if len(targets) == 0 {
		return false
	}
	for _, src := range targets {
		found := false
		for _, have := range m.srcExclude[src] {
			if have == g {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// presetRowLine sets one row: the cursor mark, the label, and the note pushed
// to the right edge.
//
// The mark occupies the column the leading space occupies on every other row,
// so walking down the list does not slide it sideways. A row with no note is
// not padded out to the edge, which would leave a ragged run of blanks on the
// one layout where the panel is the whole screen.
func presetRowLine(label, note string, onCursor bool, width int) string {
	mark := " "
	if onCursor {
		mark = "▸"
	}
	// clip and not shorten: shorten keeps the informative tail, which is right
	// for a path and wrong for a row whose first three columns are its
	// checkbox. A row that drops the box to fit says nothing at all.
	if note == "" {
		text := clip(label, maxInt(2, width-1))
		if onCursor {
			return stSelected.Render(mark + text)
		}
		return mark + text
	}
	room := maxInt(0, width-lipgloss.Width(note)-2)
	text := padTo(clip(label, maxInt(2, room)), room)
	if onCursor {
		return stSelected.Render(mark+text) + " " + stMuted.Render(note)
	}
	return mark + text + " " + stMuted.Render(note)
}

// presetScopeMarks styles the chosen half of the scope line, which is the half
// that says where enter writes.
func (m *model) presetScopeMarks(f struct{ lead, here, all string }) string {
	if m.preset.allSrc {
		return stMuted.Render(f.here) + "  " + stSelected.Render("["+f.all+"]")
	}
	return stSelected.Render("["+f.here+"]") + "  " + stMuted.Render(f.all)
}
