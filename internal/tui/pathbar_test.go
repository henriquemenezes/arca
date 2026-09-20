package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pathModel opens the source screen the way the flow does: on the path bar.
func pathModel(t *testing.T, root string) *model {
	t.Helper()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.state = stateSources
	m.browser = newBrowser(root)
	m.openSources()
	return m
}

// ---------- d opens the field, esc closes it ----------

func TestTheSourceScreenOpensOnTheFileList(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)
	m := pathModel(t, root)

	if m.mode != srcBrowse {
		t.Fatalf("mode = %v, want the file list to have the keys", m.mode)
	}
	if m.browser.path.input.Focused() {
		t.Error("the directory field has the cursor; the list is what this screen is for")
	}
	// The list really has them: space marks, rather than typing a space.
	send(m, " ")
	if len(m.browser.Selected()) != 1 {
		t.Errorf("space did not reach the list: selection = %v", m.browser.Selected())
	}
}

func TestDOpensTheDirectoryFieldAndEscCloses(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)
	m := pathModel(t, root)

	send(m, "d")
	if m.mode != srcPath || !m.browser.path.input.Focused() {
		t.Fatalf("d did not open the directory field: mode = %v", m.mode)
	}
	if got := m.browser.path.Value(); got != m.browser.cwd {
		t.Errorf("the field says %q, want the current directory %q", got, m.browser.cwd)
	}

	send(m, "esc")
	if m.mode != srcBrowse {
		t.Fatalf("esc did not give the keys back to the list: mode = %v", m.mode)
	}
	if m.state != stateSources {
		t.Fatalf("esc left the screen instead of the field: state = %v", m.state)
	}
	// Only from the list does esc mean leaving.
	send(m, "esc")
	if m.state == stateSources {
		t.Error("esc from the list did not leave the screen")
	}
}

// Esc undoes the smallest thing it can: a path typed goes before the focus
// does.
func TestEscTakesBackATypedPathBeforeTheFocus(t *testing.T) {
	root := t.TempDir()
	m := pathModel(t, root)

	send(m, "d", "ctrl+u")
	typed(m, "/et")
	send(m, "esc")
	if m.mode != srcPath {
		t.Fatalf("esc left the field with a path still in it: mode = %v", m.mode)
	}
	if got := m.browser.path.Value(); got != root {
		t.Errorf("the field says %q, want it back at %q", got, root)
	}
	// The list followed the "/" into the root directory as it was typed, and
	// taking the path back takes that walk back with it.
	if m.browser.cwd != root {
		t.Errorf("the list is showing %q, want it back at %q", m.browser.cwd, root)
	}
	send(m, "esc")
	if m.mode != srcBrowse {
		t.Errorf("a second esc did not give the keys back to the list: mode = %v", m.mode)
	}
}

// Enter is what continues on every other screen, and the file list is where a
// selection is finished.
func TestEnterContinuesFromTheFileList(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)
	m := pathModel(t, root)

	send(m, " ", "enter")
	if m.state != stateMapping {
		t.Fatalf("enter did not continue: state = %v (err %v)", m.state, m.err)
	}
}

// In the field enter is the field's: it goes to what was typed. Arriving is the
// end of what the field was for, so the list takes the keys back with it.
func TestEnterInTheFieldGoesAndHandsTheKeysBack(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Work")
	os.MkdirAll(target, 0o755)

	m := pathModel(t, root)
	send(m, "d", "ctrl+u")
	typed(m, target)
	send(m, "enter")

	if m.state != stateSources {
		t.Fatalf("enter left the screen: state = %v", m.state)
	}
	if m.browser.cwd != target {
		t.Fatalf("cwd = %q, want %q", m.browser.cwd, target)
	}
	if m.mode != srcBrowse {
		t.Fatalf("mode = %v, want the list to have the keys once it is showing the directory", m.mode)
	}
	if m.browser.path.input.Focused() {
		t.Error("the field kept the cursor")
	}
}

// ---------- typing a directory ----------

func TestTypingAPathChangesTheDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "Work", "site")
	os.MkdirAll(target, 0o755)

	m := pathModel(t, root)
	send(m, "d", "ctrl+u")
	typed(m, target)
	send(m, "enter")

	if m.browser.cwd != target {
		t.Fatalf("cwd = %q, want %q", m.browser.cwd, target)
	}
	if m.err != nil {
		t.Errorf("err = %v", m.err)
	}
	if got := m.browser.path.Value(); got != target {
		t.Errorf("the path bar says %q, want %q", got, target)
	}
}

// The bar is the current directory, so whatever moves the browser moves it.
func TestThePathBarFollowsTheBrowser(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "alpha"), 0o755)

	m := pathModel(t, root)
	send(m, "right")
	if m.browser.cwd != filepath.Join(root, "alpha") {
		t.Fatalf("cwd = %q", m.browser.cwd)
	}
	send(m, "d")
	if got := m.browser.path.Value(); got != filepath.Join(root, "alpha") {
		t.Errorf("the path bar says %q, want the directory the arrows opened", got)
	}
}

func TestTheFieldCompletesWithTab(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Downloads"), 0o755)

	m := pathModel(t, root)
	send(m, "d", "ctrl+u")
	typed(m, filepath.Join(root, "Down"))
	send(m, "tab")

	want := filepath.Join(root, "Downloads") + string(filepath.Separator)
	if got := m.browser.path.Value(); got != want {
		t.Fatalf("completion gave %q, want %q", got, want)
	}
	// A completed directory is one the browser is already listing: the whole
	// point of typing it was to get there.
	if m.browser.cwd != filepath.Join(root, "Downloads") {
		t.Errorf("cwd = %q, want the completed directory", m.browser.cwd)
	}
}

// Completing stops where the candidates stop agreeing, the way a shell does.
func TestCompletionStopsAtWhatTheCandidatesShare(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Documents"), 0o755)
	os.MkdirAll(filepath.Join(root, "Downloads"), 0o755)

	m := pathModel(t, root)
	send(m, "d", "ctrl+u")
	typed(m, filepath.Join(root, "D"))
	send(m, "tab")

	if got := m.browser.path.Value(); got != filepath.Join(root, "Do") {
		t.Errorf("completion gave %q, want it to stop at the shared %q", got, "Do")
	}
}

// Completing is tab and only tab, so the arrows in the field are the text
// cursor and nothing else — including at the end of the line, where a fish-like
// "accept the suggestion" would otherwise be tempting.
func TestTheArrowsInTheFieldAreOnlyTheTextCursor(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Downloads"), 0o755)

	m := pathModel(t, root)
	send(m, "d", "ctrl+u")
	typed(m, filepath.Join(root, "Down"))
	before := m.browser.path.Value()

	send(m, "right") // at the end of the line
	if got := m.browser.path.Value(); got != before {
		t.Errorf("right completed the line: %q", got)
	}
	send(m, "left")
	if pos := m.browser.path.input.Position(); pos != len([]rune(before))-1 {
		t.Errorf("cursor = %d, want it one place back from the end", pos)
	}
}

func TestAPathThatNamesNothingSaysSo(t *testing.T) {
	root := t.TempDir()
	m := pathModel(t, root)

	send(m, "d", "ctrl+u")
	typed(m, filepath.Join(root, "nowhere"))
	send(m, "enter")

	if m.err == nil {
		t.Fatal("a path that names nothing was accepted")
	}
	if m.browser.cwd != root {
		t.Errorf("the browser moved to %q; it should have stayed put", m.browser.cwd)
	}
}

// A file is a perfectly ordinary thing to back up, so naming one puts the
// cursor on it with the list ready to mark it.
func TestTypingAFileLandsOnItInItsDirectory(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Work"), 0o755)
	file := filepath.Join(root, "Work", "notes.txt")
	os.WriteFile(file, []byte("x"), 0o644)

	m := pathModel(t, root)
	send(m, "d", "ctrl+u")
	typed(m, file)
	send(m, "enter")

	if m.browser.cwd != filepath.Dir(file) {
		t.Fatalf("cwd = %q, want %q", m.browser.cwd, filepath.Dir(file))
	}
	if got, _ := m.browser.current(); got.path != file {
		t.Errorf("the cursor is on %q, want %q", got.path, file)
	}
	if m.mode != srcBrowse {
		t.Errorf("mode = %v, want the list to have the keys so space can mark it", m.mode)
	}
}

// "~" is what a person types for their home directory; the shell that would
// normally expand it never sees this field.
func TestThePathBarExpandsATilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	m := pathModel(t, t.TempDir())
	send(m, "d", "ctrl+u")
	typed(m, "~")
	send(m, "enter")

	if m.browser.cwd != home {
		t.Errorf("cwd = %q, want %q", m.browser.cwd, home)
	}
}

// The line the directory was printed on is the line the field replaced, so the
// screen still says where the list came from — twice over, in fact: the field
// holds what is being typed, and the line under it what is being listed.
func TestThePathBarIsDrawnWhereTheDirectoryLineWas(t *testing.T) {
	root := filepath.Join(t.TempDir(), "zebra")
	os.MkdirAll(root, 0o755)
	m := pathModel(t, root)
	send(m, "d")

	view := m.View()
	if !strings.Contains(view, "zebra") {
		t.Errorf("the screen does not show the current directory:\n%s", view)
	}
	if !strings.Contains(view, "0 entries") {
		t.Errorf("the screen does not say what is being listed:\n%s", view)
	}
}
