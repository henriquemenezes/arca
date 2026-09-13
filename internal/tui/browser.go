package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

func (b *browser) load(dir string) {
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
		b.cursor = len(b.entries) - 1
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
		if e, ok := b.current(); ok {
			if b.selected[e.path] {
				delete(b.selected, e.path)
			} else {
				b.selected[e.path] = true
			}
			b.move(1)
		}
	}
}

func (b *browser) move(n int) {
	if len(b.entries) == 0 {
		return
	}
	b.cursor += n
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= len(b.entries) {
		b.cursor = len(b.entries) - 1
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
	if b.cursor < 0 || b.cursor >= len(b.entries) {
		return browserEntry{}, false
	}
	return b.entries[b.cursor], true
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

func (b *browser) View(width int) string {
	var sb strings.Builder
	sb.WriteString(stMuted.Render(shorten(b.cwd, width-2)) + "\n\n")

	if b.err != nil {
		sb.WriteString(stErr.Render(b.err.Error()) + "\n")
	}
	if len(b.entries) == 0 && b.err == nil {
		sb.WriteString(stMuted.Render("  (empty)") + "\n")
	}

	end := b.offset + b.height
	if end > len(b.entries) {
		end = len(b.entries)
	}
	for i := b.offset; i < end; i++ {
		e := b.entries[i]

		mark := "  "
		if b.selected[e.path] {
			mark = stMark.Render("● ")
		}
		name := e.name
		if e.isDir {
			name = stDir.Render(name + "/")
		}
		cursor := "  "
		if i == b.cursor {
			cursor = stSelected.Render("▸ ")
			if !e.isDir {
				name = stSelected.Render(e.name)
			}
		}
		sb.WriteString(cursor + mark + name + "\n")
	}
	if len(b.entries) > b.height {
		sb.WriteString(stCrumb.Render(fmt.Sprintf("  %d/%d", b.cursor+1, len(b.entries))) + "\n")
	}
	return sb.String()
}

// shorten keeps the informative tail of a long path.
func shorten(s string, max int) string {
	if max <= 4 || len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max+1:]
}
