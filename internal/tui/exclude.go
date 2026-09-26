package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/walk"
)

// Exclude patterns on the source screen.
//
// The configuration has carried them since the beginning — config.Group.Exclude,
// matched by the gitignore-shaped matcher in package walk — but no screen ever
// offered a way to write one, so excluding node_modules meant leaving the
// interface and editing the file by hand. This is that screen.
//
// A pattern belongs to a group in the file, and a group is a destination, which
// the source screen has not chosen yet. So the patterns are held per source
// here and unioned per destination by buildConfig. Two sources that end up
// sharing a destination therefore share their patterns; the panel says so
// rather than pretending the format is finer-grained than it is.

// nearestSource finds the chosen path that contains p, which is the source an
// exclusion under it belongs to. The deepest one wins, so excluding something
// inside ~/.config/nvim attaches to ~/.config/nvim and not to ~/.config.
func nearestSource(selected map[string]bool, p string) (string, bool) {
	best := ""
	for src := range selected {
		if !under(src, p) {
			continue
		}
		if len(src) > len(best) {
			best = src
		}
	}
	return best, best != ""
}

// under reports whether p sits strictly inside dir.
func under(dir, p string) bool {
	if dir == p {
		return false
	}
	return strings.HasPrefix(p, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// excludePattern turns a path under a source into the pattern that leaves it
// out: the path relative to that source.
//
// The pattern comes out bare, which is gitignore's convention and the one the
// matcher already implements — with a separator it is matched against the path
// relative to the source root, without one against a basename at any depth. A
// single-segment pattern is therefore broader than the one directory it was
// derived from. That is left as it is deliberately: the alternative is an
// anchored form, and package walk is where most of the correctness risk of a
// backup tool lives. The panel warns instead of the matcher changing.
func excludePattern(source, p string) (string, error) {
	rel, err := filepath.Rel(source, p)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is not inside %s", p, source)
	}
	return rel, nil
}

// broadPattern reports whether a pattern will match a basename at any depth
// rather than the one place it names.
func broadPattern(p string) bool { return !strings.ContainsRune(p, '/') }

// excluded reports whether p is left out by the source that contains it. It is
// what puts the mark on a browser row, so it answers for the path itself, not
// for what a walk would do with its children.
//
// The answer comes from package walk's matcher rather than from a comparison
// of its own. A pattern here can be any glob — the preset menu writes
// "**/node_modules" — and a literal comparison would leave the mark dark for
// everything a preset had just excluded, which is the screen misreporting the
// one thing it exists to show.
func (m *model) excluded(p string) bool {
	_, ok := m.coveredBy(p)
	return ok
}

// coveredBy names the pattern that leaves p out, which is what lets a refusal
// say why rather than only that.
func (m *model) coveredBy(p string) (string, bool) {
	src, ok := nearestSource(m.browser.selected, p)
	if !ok {
		return "", false
	}
	rel, err := excludePattern(src, p)
	if err != nil {
		return "", false
	}
	base := filepath.Base(p)
	for _, pattern := range m.srcExclude[src] {
		if walk.Matches([]string{pattern}, rel, base) {
			return pattern, true
		}
	}
	return "", false
}

// toggleExclude is the x key: leave out the highlighted path, or take it back
// in if it is already left out.
func (m *model) toggleExclude(p string) error {
	if m.browser.selected[p] {
		return errors.New("this is one of the chosen paths: press space to unselect it, " +
			"or exclude something inside it")
	}
	src, ok := nearestSource(m.browser.selected, p)
	if !ok {
		return errors.New("an exclusion applies inside a chosen path, and nothing chosen " +
			"contains this one: press space to choose one first")
	}
	pattern, err := excludePattern(src, p)
	if err != nil {
		return err
	}

	base := filepath.Base(p)
	// A pattern like "node_modules" leaves this path out only because of its
	// base name, so it says nothing about this path in particular.
	byBaseName := func(have string) bool { return broadPattern(have) && have == base }
	// x undoes only what x could have made, so a glob is not removed here:
	// dropping "**/node_modules" because the cursor sits on one of them would
	// take every other one with it. Adding a second pattern for a path already
	// covered is no better, so it says which pattern covers it and where that
	// one is removed.
	if have, ok := m.coveredBy(p); ok && have != pattern && !byBaseName(have) {
		return fmt.Errorf("%s is already left out by %s: remove that pattern in the "+
			"excludes panel (X)", base, have)
	}
	kept := m.srcExclude[src][:0:0]
	removed := false
	for _, have := range m.srcExclude[src] {
		if have == pattern || byBaseName(have) {
			removed = true
			continue
		}
		kept = append(kept, have)
	}
	if !removed {
		kept = append(kept, pattern)
	}
	m.setExclude(src, kept)
	return nil
}

// setExclude replaces one source's patterns and forgets what it was measured
// to hold. The count in the panel is produced by a walk that honours these
// patterns, so leaving the old number in place would leave the screen stating
// a size the backup is no longer going to write.
func (m *model) setExclude(source string, patterns []string) {
	if len(patterns) == 0 {
		delete(m.srcExclude, source)
	} else {
		m.srcExclude[source] = patterns
	}
	delete(m.sizes, source)
	delete(m.measuring, source)
}

// ---------- the panel ----------

// excludeRow is one line of the excludes panel: a chosen source, or one of the
// patterns under it.
type excludeRow struct {
	source  string
	pattern string // empty when the row is the source itself
}

// excludeRows lays the chosen sources and their patterns out flat, which is
// what the cursor moves over. A source with no patterns still gets its row, so
// there is always somewhere to press "a".
func (m *model) excludeRows() []excludeRow {
	var rows []excludeRow
	for _, src := range m.browser.Selected() {
		rows = append(rows, excludeRow{source: src})
		for _, p := range m.srcExclude[src] {
			rows = append(rows, excludeRow{source: src, pattern: p})
		}
	}
	return rows
}

// excludeCursor is the row the panel is on, clamped to what exists now: the
// selection can change under it while the browser has focus.
func (m *model) excludeCursor(rows []excludeRow) int {
	if m.excIndex >= len(rows) {
		return maxInt(0, len(rows)-1)
	}
	if m.excIndex < 0 {
		return 0
	}
	return m.excIndex
}

// addPattern accepts what was typed into the panel's field.
func (m *model) addPattern(source, pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return errors.New("an exclude pattern cannot be empty")
	}
	if filepath.IsAbs(pattern) {
		return errors.New("an exclude pattern is relative to the source it belongs to, " +
			"not an absolute path")
	}
	m.setExclude(source, appendUnique(append([]string(nil), m.srcExclude[source]...),
		[]string{filepath.ToSlash(pattern)}))
	return nil
}

// removePattern drops the pattern the cursor is on.
func (m *model) removePattern(row excludeRow) {
	if row.pattern == "" {
		return
	}
	kept := make([]string, 0, len(m.srcExclude[row.source]))
	for _, have := range m.srcExclude[row.source] {
		if have != row.pattern {
			kept = append(kept, have)
		}
	}
	m.setExclude(row.source, kept)
}

// viewExcludes draws the panel. It replaces the selection list rather than
// sitting under it: while the panel has focus the patterns are what the screen
// is about, and the totals stay pinned at the bottom because they are the one
// thing the patterns change.
func (m *model) viewExcludes(width, rows int) string {
	list := m.excludeRows()
	if len(list) == 0 {
		return stTitle.Render("Excludes") + "\n\n" +
			stMuted.Render("Nothing chosen yet.") + "\n\n" +
			stCrumb.Render(wrap("choose a path first; an exclusion is always something left out of one.", width))
	}

	cursor := m.excludeCursor(list)
	// The title and the blank line under it, the field when it is open, and the
	// two footer lines with their blank line above them.
	listRows := maxInt(1, rows-5)
	if m.mode == srcExcludeInput {
		listRows = maxInt(1, listRows-2)
	}

	start := 0
	if cursor >= listRows {
		start = cursor - listRows + 1
	}
	end := minInt(len(list), start+listRows)

	lines := make([]string, 0, rows)
	for i := start; i < end; i++ {
		lines = append(lines, m.excludeLine(list[i], i == cursor, width))
	}
	for len(lines) < listRows {
		lines = append(lines, "")
	}

	if m.mode == srcExcludeInput {
		lines = append(lines, "", m.excInput.View())
	}

	files, bytes, pending := m.selectionTotals()
	total := cli.Count(files, "file", "files") + " · " + humanBytes(bytes)
	if pending > 0 {
		total += " · counting…"
	}
	lines = append(lines, "",
		stMuted.Render(wrap("Patterns apply to every source sharing a destination.", width)),
		stEmph.Render(total))

	return stTitle.Render("Excludes") + "\n\n" + strings.Join(lines, "\n")
}

func (m *model) excludeLine(row excludeRow, onCursor bool, width int) string {
	if row.pattern == "" {
		name := shorten(contractHome(row.source), width-2)
		if onCursor {
			return stSelected.Render("▸ " + name)
		}
		return "  " + stDir.Render(name)
	}

	label := row.pattern
	if broadPattern(label) {
		// A pattern with no separator matches that name wherever it turns up,
		// which is rarely what someone picking one directory out of a tree
		// meant. Saying so is cheaper than a semantics nobody can see.
		label += " (any depth)"
	}
	label = shorten(label, width-6)
	if onCursor {
		return stSelected.Render("  ▸ " + label)
	}
	return "    " + stExclude.Render("✗ ") + stMuted.Render(label)
}

// ---------- carrying the patterns in and out ----------

// excludesFor is what one mapping entry contributes. The mapping spells a path
// the way its configuration file did, which may be a "~/", while the patterns
// are keyed by what the browser hands back.
func (m *model) excludesFor(path string) []string {
	if p, ok := m.srcExclude[path]; ok {
		return p
	}
	if abs, err := config.ExpandPath(path); err == nil {
		return m.srcExclude[abs]
	}
	return nil
}

// seedExcludes attributes a configuration's group patterns to each of that
// group's sources, which is what lets the source screen show and edit them.
//
// Spreading a group's patterns across its sources and unioning them back per
// destination is exact, including for a group of several sources: every source
// carries the same set, so the union is the set it started as.
func (m *model) seedExcludes(cfg *config.Config) {
	m.srcExclude = map[string][]string{}
	for _, g := range cfg.Groups {
		if len(g.Exclude) == 0 {
			continue
		}
		for _, s := range g.Sources {
			abs, err := config.ExpandPath(s.Path)
			if err != nil {
				continue
			}
			m.srcExclude[abs] = appendUnique(append([]string(nil), m.srcExclude[abs]...), g.Exclude)
		}
	}
}

// excludeCount is how many patterns the selection carries, which is what the
// selection panel reports without having to list them.
func (m *model) excludeCount() int {
	n := 0
	for _, src := range m.browser.Selected() {
		n += len(m.srcExclude[src])
	}
	return n
}
