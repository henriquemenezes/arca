package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// press sends one key and hands back the command it produced, which is what
// the preview needs: the counting happens in a command.
func press(m *model, k string) tea.Cmd {
	_, cmd := m.Update(key(k))
	return cmd
}

// run drains a command the way the Bubble Tea runtime would, fanning batches
// out and feeding every message back, so a test sees the screen as it looks
// once the background counting has finished.
func run(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case nil:
	default:
		_, next := m.Update(msg)
		if _, ok := msg.(sizeMsg); ok {
			run(m, next)
		}
	}
}

func sourcesModel(t *testing.T, root string, width, height int) *model {
	t.Helper()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m.state = stateSources
	m.browser = newBrowser(root)
	return m
}

func TestSelectionPreviewShowsWhatEachChoiceHolds(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".ssh"), 0o755)
	os.WriteFile(filepath.Join(root, ".ssh", "config"), []byte("host"), 0o600)
	os.WriteFile(filepath.Join(root, ".ssh", "id_ed25519"), []byte("key"), 0o600)

	m := sourcesModel(t, root, 100, 40)
	run(m, press(m, " ")) // mark .ssh, which sorts first

	view := m.View()
	if !strings.Contains(view, ".ssh") {
		t.Errorf("the preview does not name the chosen path:\n%s", view)
	}
	if !strings.Contains(view, "2 files") {
		t.Errorf("the preview does not count the chosen path:\n%s", view)
	}
	if !strings.Contains(view, "7 B") {
		t.Errorf("the preview does not size the chosen path:\n%s", view)
	}
}

func TestSelectionPreviewTotalsEveryChoice(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)
	os.WriteFile(filepath.Join(root, "alpha", "a"), []byte("aa"), 0o644)
	os.MkdirAll(filepath.Join(root, "beta"), 0o755)
	os.WriteFile(filepath.Join(root, "beta", "b"), []byte("bbb"), 0o644)

	m := sourcesModel(t, root, 100, 40)
	run(m, press(m, " ")) // alpha
	run(m, press(m, "down"))
	run(m, press(m, " ")) // beta

	files, bytes, pending := m.selectionTotals()
	if files != 2 || bytes != 5 || pending != 0 {
		t.Fatalf("totals = %d files, %d bytes, %d pending; want 2, 5, 0", files, bytes, pending)
	}
	if view := m.View(); !strings.Contains(view, "2 items") {
		t.Errorf("the preview does not say how much was chosen:\n%s", view)
	}
}

// A path counted once is not counted again: walking a large tree is the one
// expensive thing this screen does.
func TestSelectionPreviewCountsEachPathOnce(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)
	os.WriteFile(filepath.Join(root, "alpha", "a"), []byte("aa"), 0o644)

	m := sourcesModel(t, root, 100, 40)
	run(m, press(m, " "))
	if cmd := m.measureSelection(); cmd != nil {
		t.Error("a path already counted was queued for counting again")
	}
}

// A configuration is shared across machines, so a source it names may simply
// not be here. The preview has to say so rather than report it as empty.
func TestSelectionPreviewReportsAPathThatIsGone(t *testing.T) {
	root := t.TempDir()
	m := sourcesModel(t, root, 100, 40)
	m.browser.selected[filepath.Join(root, "gone")] = true

	run(m, m.measureSelection())
	if view := m.View(); !strings.Contains(view, "missing") {
		t.Errorf("a source that is not there should be flagged:\n%s", view)
	}
}

func TestSelectionPreviewSaysWhenNothingIsChosen(t *testing.T) {
	m := sourcesModel(t, t.TempDir(), 100, 40)
	if view := m.View(); !strings.Contains(view, "Nothing chosen yet") {
		t.Errorf("an empty selection should be stated:\n%s", view)
	}
}

// The preview may never squeeze the browser into uselessness: a narrow
// terminal keeps the single column it has always had.
func TestSourcesLayoutGivesUpThePreviewWhenNarrow(t *testing.T) {
	m := sourcesModel(t, t.TempDir(), 60, 40)
	if _, preview := m.sourcesLayout(); preview != 0 {
		t.Errorf("preview width = %d on a 60 column terminal, want none", preview)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	browser, preview := m.sourcesLayout()
	if preview < previewMinWidth {
		t.Errorf("preview width = %d, want at least %d", preview, previewMinWidth)
	}
	if browser < browserMinWidth {
		t.Errorf("browser width = %d, want at least %d", browser, browserMinWidth)
	}
	if browser+preview+previewGap > 100-4 {
		t.Errorf("the two columns (%d + %d) overflow the terminal", browser, preview)
	}
}

// Nothing the browser draws may spill into the column beside it.
func TestBrowserKeepsToItsColumn(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, strings.Repeat("long-name-", 8)), []byte("x"), 0o644)

	b := newBrowser(root)
	for _, line := range strings.Split(strings.TrimRight(b.View(30), "\n"), "\n") {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line is %d columns wide, want at most 30: %q", w, line)
		}
	}
}
