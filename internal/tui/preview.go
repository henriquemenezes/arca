package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/walk"
)

// The source screen is two columns: the browser on the left, and what has been
// chosen so far on the right. The preview only appears when both columns can
// still do their job — below that the browser keeps the whole width, because a
// file list too narrow to read is worse than no preview at all.
const (
	previewMinWidth = 36
	previewMaxWidth = 48
	previewGap      = 2
	browserMinWidth = 34
)

// sizeStat is what one chosen path holds. It is filled in by a background
// count, so a path can be in the preview before its numbers are.
type sizeStat struct {
	files int
	bytes int64
	err   error
}

// sizeMsg carries a finished count back to the interface.
type sizeMsg struct {
	path string
	stat sizeStat
}

// measureSelection starts a count for every chosen path that has none yet.
//
// Counting is per path rather than over the whole selection, so marking one
// more directory never re-walks the ones already counted, and each row can
// fill in as soon as its own walk finishes.
func (m *model) measureSelection() tea.Cmd {
	opts := m.walkOptions()
	var cmds []tea.Cmd
	for _, p := range m.browser.Selected() {
		if _, counted := m.sizes[p]; counted || m.measuring[p] {
			continue
		}
		m.measuring[p] = true
		cmds = append(cmds, measurePath(p, opts))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// walkOptions mirrors the settings the backup will actually run with, so the
// preview counts what the plan will count.
func (m *model) walkOptions() walk.Options {
	settings := config.Default().Settings
	switch {
	case m.base != nil:
		settings = m.base.Settings
	case m.cfg != nil:
		settings = m.cfg.Settings
	}
	return walk.Options{
		FollowSymlinks: settings.FollowSymlinks,
		OneFilesystem:  settings.OneFilesystem,
	}
}

// measurePath counts one path through the same walker the plan uses, off the
// interface's goroutine. Only metadata is read, so even a home directory is
// cheap, and nothing on screen waits for it.
//
// The patterns go in because the number is a promise about what the archive
// will hold. Counting a tree the backup is about to leave half of behind would
// make the panel state a size no run of the tool ever produces.
func measurePath(path string, opts walk.Options) tea.Cmd {
	return func() tea.Msg {
		// Asking first tells a path that is gone apart from one that cannot be
		// read; the walker reports both as "no source exists".
		if _, err := os.Lstat(path); err != nil {
			return sizeMsg{path: path, stat: sizeStat{err: err}}
		}
		src := config.ResolvedSource{
			Path:   path,
			Member: filepath.Base(path),
		}
		res, err := walk.Walk([]config.ResolvedSource{src}, opts, func(walk.Entry) error { return nil })
		if err != nil {
			return sizeMsg{path: path, stat: sizeStat{err: err}}
		}
		return sizeMsg{path: path, stat: sizeStat{files: res.Files, bytes: res.Bytes}}
	}
}

// selectionTotals adds up the counts that have arrived, and says how many are
// still outstanding so the screen can admit it is not finished.
func (m *model) selectionTotals() (files int, bytes int64, pending int) {
	for _, p := range m.browser.Selected() {
		stat, counted := m.sizes[p]
		switch {
		case !counted:
			pending++
		case stat.err == nil:
			files += stat.files
			bytes += stat.bytes
		}
	}
	return files, bytes, pending
}

// sourcesLayout divides the screen between the browser and the preview.
// A preview width of zero means the terminal is too narrow to carry both.
func (m *model) sourcesLayout() (browserWidth, previewWidth int) {
	avail := m.width - 4 // the padding View puts around every screen
	preview := minInt(previewMaxWidth, maxInt(previewMinWidth, avail/3))
	if avail-preview-previewGap < browserMinWidth {
		return maxInt(20, avail), 0
	}
	return avail - preview - previewGap, preview
}

// viewSelectionPreview draws the chosen paths and what they add up to.
//
// The totals are pinned to the bottom of the panel so they stay in one place
// while the list above them grows, and the list gives up its own rows before
// they do: what has been chosen is readable from the browser, but how much it
// comes to is not readable anywhere else.
func (m *model) viewSelectionPreview(width, rows int) string {
	sel := m.browser.Selected()
	if len(sel) == 0 {
		return stTitle.Render("Selection") + "\n\n" +
			stMuted.Render("Nothing chosen yet.") + "\n\n" +
			stCrumb.Render(wrap("space marks the highlighted file or directory, and what it holds appears here.", width))
	}

	// The title and the blank line under it, then the two footer lines and the
	// blank line above them.
	listRows := maxInt(1, rows-5)
	shown := len(sel)
	if shown > listRows {
		shown = maxInt(1, listRows-1)
	}

	lines := make([]string, 0, rows)
	for _, p := range sel[:shown] {
		label, style := m.statLabel(p)
		lines = append(lines, previewRow(contractHome(p), label, style, width))
	}
	if shown < len(sel) {
		lines = append(lines, stCrumb.Render(fmt.Sprintf("+%d more", len(sel)-shown)))
	}
	for len(lines) < listRows {
		lines = append(lines, "")
	}

	files, bytes, pending := m.selectionTotals()
	count := cli.Count(len(sel), "item", "items")
	if pending > 0 {
		count += " · counting…"
	} else {
		count += " selected"
	}
	lines = append(lines, "", stMuted.Render(count),
		stKey.Render(fmt.Sprintf("%s · %s", cli.Count(files, "file", "files"), humanBytes(bytes))))

	return stTitle.Render("Selection") + "\n\n" + strings.Join(lines, "\n")
}

// statLabel says what is known about one chosen path.
func (m *model) statLabel(path string) (string, lipgloss.Style) {
	stat, counted := m.sizes[path]
	switch {
	case !counted:
		return "counting…", stCrumb
	case errors.Is(stat.err, fs.ErrNotExist):
		return "missing", stWarn
	case stat.err != nil:
		return "unreadable", stWarn
	}
	return fmt.Sprintf("%s · %s", cli.Count(stat.files, "file", "files"), humanBytes(stat.bytes)), stMuted
}

// previewRow lays a path against its measurement, right-aligned, and gives the
// measurement the room it needs: the path can be recognised from its tail, the
// numbers cannot be recognised from half of themselves.
func previewRow(name, stat string, style lipgloss.Style, width int) string {
	const sep = 2 // enough space that the path and the numbers stay two things
	name = shorten(name, maxInt(8, width-lipgloss.Width(stat)-sep))
	gap := width - lipgloss.Width(name) - lipgloss.Width(stat)
	if gap < sep {
		gap = sep
	}
	return name + strings.Repeat(" ", gap) + style.Render(stat)
}
