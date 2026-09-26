package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henriquemenezes/arca/internal/config"
)

// tree builds a directory and returns it. Every test here works inside its own
// t.TempDir(), so nothing is read from or written to the real filesystem.
func tree(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		if strings.HasSuffix(n, "/") {
			if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(root, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// bases reduces candidates to the names a reader would recognise.
func bases(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A destination is a place. Offering the archives already sitting in it would
// be offering to overwrite one.
func TestPathCandidatesOffersDirectoriesOnly(t *testing.T) {
	root := tree(t, "Documents/", "Downloads/", "notes.txt", "arca-backup.tar.zst.age")

	_, got := pathCandidates(root + "/")
	if want := []string{"Documents", "Downloads"}; !eq(bases(got), want) {
		t.Errorf("candidates = %v, want %v", bases(got), want)
	}
	for _, c := range got {
		if !strings.HasSuffix(c, string(filepath.Separator)) {
			t.Errorf("%q does not end in a separator; completing into it would need one", c)
		}
	}
}

func TestPathCandidatesHidesDotDirectoriesUntilAskedFor(t *testing.T) {
	root := tree(t, ".ssh/", ".config/", "Documents/")

	if _, got := pathCandidates(root + "/"); !eq(bases(got), []string{"Documents"}) {
		t.Errorf("an unprefixed listing showed %v, want only Documents", bases(got))
	}
	_, got := pathCandidates(root + string(filepath.Separator) + ".")
	if want := []string{".config", ".ssh"}; !eq(bases(got), want) {
		t.Errorf("typing a dot showed %v, want %v", bases(got), want)
	}
}

// The listing is case-insensitive to match textinput's own matcher, so the
// ghost text can never disagree with the list underneath it.
func TestPathCandidatesIgnoresCase(t *testing.T) {
	root := tree(t, "Downloads/")

	_, got := pathCandidates(filepath.Join(root, "down"))
	if !eq(bases(got), []string{"Downloads"}) {
		t.Fatalf("candidates = %v, want Downloads", bases(got))
	}
	// Completion works off the real name, so it corrects the case rather than
	// preserving the typo.
	p := newPathInput("", 40)
	p.SetValue(filepath.Join(root, "down"))
	p.complete()
	if want := filepath.Join(root, "Downloads") + "/"; p.Value() != want {
		t.Errorf("tab produced %q, want %q", p.Value(), want)
	}
}

// ReadDir describes the link, not its target. A home directory whose Downloads
// is a symlink onto another filesystem is an ordinary place to write a backup.
func TestPathCandidatesFollowsSymlinkedDirectories(t *testing.T) {
	root := tree(t, "real/")
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "nowhere"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}

	_, got := pathCandidates(root + "/")
	if want := []string{"link", "real"}; !eq(bases(got), want) {
		t.Errorf("candidates = %v, want %v (a broken link points at no directory)", bases(got), want)
	}
}

// Tab fills in what is certain and stops. Choosing between Documents and
// Downloads is the list's job, not tab's.
func TestTabStopsAtWhatTheCandidatesAgreeOn(t *testing.T) {
	root := tree(t, "Documents/", "Downloads/")

	p := newPathInput("", 40)
	p.SetValue(filepath.Join(root, "Do"))
	p.complete()

	if want := filepath.Join(root, "Do"); p.Value() != want {
		t.Errorf("tab produced %q, want it to stay at %q", p.Value(), want)
	}
	if len(p.candidates) != 2 {
		t.Errorf("the list holds %d candidates, want the 2 tab could not choose between", len(p.candidates))
	}
}

func TestTabTakesAUniqueCandidateWhole(t *testing.T) {
	root := tree(t, "Documents/", "Downloads/", "Downloads/2026/")

	p := newPathInput("", 40)
	p.SetValue(filepath.Join(root, "Dow"))
	p.complete()

	want := filepath.Join(root, "Downloads") + string(filepath.Separator)
	if p.Value() != want {
		t.Fatalf("tab produced %q, want %q", p.Value(), want)
	}
	// Having entered it, the list is what is inside it.
	if !eq(bases(p.candidates), []string{"2026"}) {
		t.Errorf("after completing, the list shows %v, want the contents of Downloads", bases(p.candidates))
	}
}

// The arrows are how the list underneath is chosen from. If moving through it
// recomputed the candidates, the second press would be walking a different
// directory than the first.
func TestArrowsPickWithoutCollapsingTheList(t *testing.T) {
	root := tree(t, "Documents/", "Downloads/", "Downloads/2026/")

	p := newPathInput("", 40)
	p.SetValue(filepath.Join(root, "Do"))
	before := append([]string(nil), p.candidates...)

	p.pick(0)
	if want := filepath.Join(root, "Documents") + "/"; p.Value() != want {
		t.Errorf("first candidate put %q in the field, want %q", p.Value(), want)
	}
	p.pick(p.index + 1)
	if want := filepath.Join(root, "Downloads") + "/"; p.Value() != want {
		t.Errorf("second candidate put %q in the field, want %q", p.Value(), want)
	}
	if !eq(p.candidates, before) {
		t.Errorf("the list changed under the cursor: %v became %v", bases(before), bases(p.candidates))
	}

	// And it wraps, so the arrows never dead-end.
	p.pick(p.index + 1)
	if want := filepath.Join(root, "Documents") + "/"; p.Value() != want {
		t.Errorf("wrapping put %q in the field, want %q", p.Value(), want)
	}
}

// The empty field is the case this exists for: the user cleared the default
// and has nothing to complete from.
func TestEmptyFieldOffersPlacesABackupGoes(t *testing.T) {
	_, got := pathCandidates("")
	if len(got) == 0 {
		t.Fatal("an empty field offered nothing at all")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{defaultOutputDir(), home} {
		if !contains(got, want+string(filepath.Separator)) {
			t.Errorf("the shortcuts %v do not include %q", got, want)
		}
	}
	// Nothing is offered on faith: every shortcut is a directory that exists.
	for _, c := range got {
		if info, err := os.Stat(c); err != nil || !info.IsDir() {
			t.Errorf("%q is offered but is not a directory that exists", c)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{"~", home},
		{"~/backups", filepath.Join(home, "backups")},
		// The trailing separator is what says "list what is inside this", so
		// expanding must not clean it away.
		{"~/backups/", filepath.Join(home, "backups") + "/"},
		{"~other/backups", "~other/backups"},
		{"/tmp/x", "/tmp/x"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := expandHome(tc.in); got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"/a/Downloads/"}, "/a/Downloads/"},
		{[]string{"/a/Documents/", "/a/Downloads/"}, "/a/Do"},
		{[]string{"/a/x/", "/b/y/"}, "/"},
		// Case-insensitive matching can put both cases in the list; the prefix
		// must then stop before them rather than pick one.
		{[]string{"/a/Downloads/", "/a/downloads-old/"}, "/a/"},
	}
	for _, tc := range cases {
		if got := commonPrefix(tc.in); got != tc.want {
			t.Errorf("commonPrefix(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// End to end through the model, which is the only way to prove the keys are
// actually wired to the screen.
func TestOutputScreenCompletesAndResolvesATildePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A real group, so accepting the destination runs the planning pass the
	// screen actually triggers instead of failing before it.
	src := tree(t, "notes.txt")
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	m.cfg = config.Default()
	m.cfg.Groups = []config.Group{{
		Name:    "g",
		Sources: []config.Source{{Path: filepath.Join(src, "notes.txt")}},
	}}
	m.toOutput() // the real way in: it focuses the field

	// Clearing the field and typing a fresh path is the reported case.
	m.outInput.SetValue("")
	send(m, "~", "/", "b", "a", "c")
	send(m, "tab")

	if want := filepath.Join(home, "backups") + "/"; m.outInput.Value() != want {
		t.Fatalf("tab produced %q, want %q", m.outInput.Value(), want)
	}

	send(m, "enter")
	if m.err != nil {
		t.Fatalf("accepting the destination failed: %v", m.err)
	}
	if strings.Contains(m.outPath, "~") {
		t.Fatalf("the archive path %q still carries a tilde; that is a directory named \"~\"", m.outPath)
	}
	if !strings.HasPrefix(m.outPath, filepath.Join(home, "backups")+string(filepath.Separator)) {
		t.Errorf("the archive lands at %q, want it inside %s", m.outPath, filepath.Join(home, "backups"))
	}
}

// The list must never push the screen past the terminal it is drawn in.
func TestPathScreensFitTheTerminal(t *testing.T) {
	root := tree(t, "a/", "b/", "c/", "d/", "e/", "f/", "g/", "h/", "i/", "j/", "k/", "l/")

	for _, s := range []state{stateOutput, stateRestoreTarget} {
		for height := 18; height <= 44; height++ {
			m := newModel()
			m.Update(tea.WindowSizeMsg{Width: 90, Height: height})
			m.cfg = config.Default()
			m.state = s
			m.outInput.SetValue(root + "/")
			m.targetInput.SetValue(root + "/")

			if n := strings.Count(m.View(), "\n") + 1; n > height {
				t.Errorf("state %v at %d rows renders %d lines", s, height, n)
			}
		}
	}
}

// The ghost text is the half of bubbles/textinput's completion that is kept.
// It must show the selected candidate and nothing else, or it would be telling
// a different story than the list underneath.
func TestGhostTextShowsTheSelectedCandidate(t *testing.T) {
	root := tree(t, "Documents/", "Downloads/")

	p := newPathInput("", 60)
	p.Focus() // an unfocused field draws no completion at all
	p.SetValue(root + string(filepath.Separator))

	if got := p.input.AvailableSuggestions(); len(got) != 1 || got[0] != p.candidates[p.index] {
		t.Fatalf("textinput was given %v, want only the selected candidate %q", got, p.candidates[p.index])
	}
	if want := "ocuments/"; !strings.Contains(p.View(), want) {
		t.Errorf("the field does not ghost %q:\n%s", want, p.View())
	}

	p.pick(p.index + 1)
	if got := p.input.AvailableSuggestions(); len(got) != 1 || got[0] != p.candidates[p.index] {
		t.Errorf("after moving, textinput holds %v, want %q", got, p.candidates[p.index])
	}
}
