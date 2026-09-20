package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// browser is a small directory walker with multi-selection.
//
// bubbles/filepicker only picks one path, and choosing what to back up is
// inherently a multi-selection over both files and directories, so this is
// hand-rolled.
type browser struct {
	cwd      string
	entries  []browserEntry
	cursor   int
	offset   int
	height   int
	selected map[string]bool
	hidden   bool
	err      error

	// filter is the fuzzy search over everything below cwd, and is nil unless
	// the search is open. While it is set the rows come from it instead of from
	// the directory listing; everything else — the cursor, the selection, the
	// keys that act on a row — works the same either way.
	filter *filter
}

type browserEntry struct {
	name  string
	path  string
	isDir bool
}

func newBrowser(start string) browser {
	b := browser{selected: map[string]bool{}, height: 12, hidden: true}
	b.load(start)
	return b
}

// list is what the browser is showing: the current directory, or the matches
// of an open search.
func (b *browser) list() []browserEntry {
	if b.filter != nil {
		return b.filter.results
	}
	return b.entries
}

// refilter rebuilds the matches and keeps the cursor on something real. It is
// called both when the query changes and when a batch of the index arrives,
// since either can change how many rows there are.
//
// The command it hands back is the index starting up. It is built on the first
// query that needs one rather than when the search opens, so the directory
// listing and a typed-out path both cost nothing.
func (b *browser) refilter() tea.Cmd {
	if b.filter == nil {
		return nil
	}
	cmd := b.filter.match(b.hidden, b.entries)
	if b.cursor >= len(b.filter.results) {
		b.cursor = maxInt(0, len(b.filter.results)-1)
	}
	b.clampOffset()
	return cmd
}

// closeFilter ends a search, stopping the scan behind it.
func (b *browser) closeFilter() {
	b.filter.close()
	b.filter = nil
	b.cursor, b.offset = 0, 0
}

func (b *browser) load(dir string) {
	// Walking into a directory ends the search that led there: its index is of
	// a tree the browser has just left.
	if b.filter != nil {
		b.closeFilter()
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		b.err = err
		return
	}
	items, err := os.ReadDir(abs)
	if err != nil {
		// Staying put on an unreadable directory is friendlier than bouncing
		// the user back to the root.
		b.err = fmt.Errorf("cannot open %s: %w", abs, err)
		return
	}

	b.err = nil
	b.cwd = abs
	b.cursor, b.offset = 0, 0
	b.entries = b.entries[:0]
	for _, it := range items {
		name := it.Name()
		if !b.hidden && strings.HasPrefix(name, ".") {
			continue
		}
		b.entries = append(b.entries, browserEntry{
			name:  name,
			path:  filepath.Join(abs, name),
			isDir: it.IsDir(),
		})
	}
	// Directories first, then names: the order people expect in a file list.
	sort.SliceStable(b.entries, func(i, j int) bool {
		if b.entries[i].isDir != b.entries[j].isDir {
			return b.entries[i].isDir
		}
		return strings.ToLower(b.entries[i].name) < strings.ToLower(b.entries[j].name)
	})
}

func (b *browser) Update(msg tea.KeyMsg) {
	switch msg.String() {
	case "up", "k":
		b.move(-1)
	case "down", "j":
		b.move(1)
	case "pgup":
		b.move(-b.height)
	case "pgdown":
		b.move(b.height)
	case "g", "home":
		b.cursor, b.offset = 0, 0
	case "G", "end":
		b.cursor = len(b.list()) - 1
		b.clampOffset()
	case "enter", "right", "l":
		if e, ok := b.current(); ok && e.isDir {
			b.load(e.path)
		}
	case "backspace", "left", "h":
		if parent := filepath.Dir(b.cwd); parent != b.cwd {
			prev := b.cwd
			b.load(parent)
			b.focus(prev)
		}
	case "~":
		if home, err := os.UserHomeDir(); err == nil {
			b.load(home)
		}
	case ".":
		b.hidden = !b.hidden
		b.load(b.cwd)
	case " ":
		b.toggle()
	}
}

func (b *browser) move(n int) {
	rows := b.list()
	if len(rows) == 0 {
		return
	}
	b.cursor += n
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= len(rows) {
		b.cursor = len(rows) - 1
	}
	b.clampOffset()
}

func (b *browser) clampOffset() {
	if b.cursor < b.offset {
		b.offset = b.cursor
	}
	if b.cursor >= b.offset+b.height {
		b.offset = b.cursor - b.height + 1
	}
	if b.offset < 0 {
		b.offset = 0
	}
}

// focus puts the cursor on a known child, so stepping out of a directory lands
// where the user came from.
func (b *browser) focus(path string) {
	for i, e := range b.entries {
		if e.path == path {
			b.cursor = i
			b.clampOffset()
			return
		}
	}
}

func (b *browser) current() (browserEntry, bool) {
	rows := b.list()
	if b.cursor < 0 || b.cursor >= len(rows) {
		return browserEntry{}, false
	}
	return rows[b.cursor], true
}

// toggle marks or unmarks the highlighted row. It is what space does, split out
// because the search reuses it on a row that is not in the current directory.
//
// The cursor stays where it is: marking is the one action here people get
// wrong, and leaving the row under the cursor means a second space takes it
// back without first having to find the row again.
func (b *browser) toggle() {
	if e, ok := b.current(); ok {
		if b.selected[e.path] {
			delete(b.selected, e.path)
		} else {
			b.selected[e.path] = true
		}
	}
}

// Selected returns the chosen paths in a stable order.
func (b *browser) Selected() []string {
	out := make([]string, 0, len(b.selected))
	for p := range b.selected {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// View draws the list. excluded says which rows are left out by an exclude
// pattern; it may be nil on the screens that have none.
func (b *browser) View(width int, excluded func(string) bool) string {
	var sb strings.Builder
	if b.filter != nil {
		// The search replaces the working directory line with what it is
		// looking through, since the rows are no longer from one directory.
		// MaxWidth truncates without cutting an escape sequence in half. The
		// field is already sized to the column, but that sizing is what keeps
		// the useful end of a long path visible, not what guarantees the width.
		sb.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(b.filter.input.View()) + "\n")
		sb.WriteString(stCrumb.Render(clip(b.filter.status(width-2), width-2)) + "\n")
	} else {
		sb.WriteString(stMuted.Render(shorten(b.cwd, width-2)) + "\n\n")
	}

	if b.err != nil {
		sb.WriteString(stErr.Render(b.err.Error()) + "\n")
	}
	rows := b.list()
	if len(rows) == 0 && b.err == nil {
		sb.WriteString(stMuted.Render("  (empty)") + "\n")
	}

	end := b.offset + b.height
	if end > len(rows) {
		end = len(rows)
	}
	for i := b.offset; i < end; i++ {
		e := rows[i]

		mark := "  "
		switch {
		case b.selected[e.path]:
			mark = stMark.Render("● ")
		case excluded != nil && excluded(e.path):
			mark = stExclude.Render("✗ ")
		}

		// The cursor and the mark are drawn before the name, and the screen may
		// be sharing its width with a panel, so the name gets whatever is left
		// and not a column more.
		//
		// A search draws the whole path: its rows come from all over the tree,
		// and "nvim" on its own does not say which of the three it is.
		room := width - 4
		var text string
		switch {
		case b.filter != nil && e.isDir:
			// The separator is part of what the row says, so the path is
			// fitted to what is left once it has been accounted for.
			text = fitPath(e.path, room-1) + "/"
		case b.filter != nil:
			text = fitPath(e.path, room)
		case e.isDir:
			text = clip(e.name+"/", room)
		default:
			text = clip(e.name, room)
		}

		style := lipgloss.NewStyle()
		switch {
		case i == b.cursor:
			style = stSelected
		case e.isDir:
			style = stDir
		}
		// Highlighting runs against the text as drawn, not the whole path, so
		// letters the width has already dropped cannot light up in the wrong
		// places.
		var hits []int
		if b.filter != nil {
			hits = b.filter.mark(text)
		}
		name := highlight(text, hits, style)

		cursor := "  "
		if i == b.cursor {
			cursor = stSelected.Render("▸ ")
		}
		sb.WriteString(cursor + mark + name + "\n")
	}
	if len(rows) > b.height {
		sb.WriteString(stCrumb.Render(fmt.Sprintf("  %d/%d", b.cursor+1, len(rows))) + "\n")
	}
	return sb.String()
}

// clip keeps the head of a name, which is where a file is recognised, and
// marks what was cut.
func clip(s string, max int) string {
	if max <= 1 {
		return ""
	}
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

// shorten keeps the informative tail of a long path.
func shorten(s string, max int) string {
	if max <= 4 || len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max+1:]
}
