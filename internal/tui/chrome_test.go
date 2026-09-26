package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/config"
)

// at builds a model sized to a terminal, which is what every layout assertion
// here needs before it can ask anything.
func at(width, height int) *model {
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m
}

// lines is the screen as the terminal would show it, styling removed.
func lines(m *model) []string {
	return strings.Split(strings.TrimRight(plain(m.View()), " \n"), "\n")
}

// footerOf is the last line with anything on it, which is where the key legend
// now lives on every screen.
func footerOf(m *model) string {
	ls := lines(m)
	for i := len(ls) - 1; i >= 0; i-- {
		if strings.TrimSpace(ls[i]) != "" {
			return strings.TrimSpace(ls[i])
		}
	}
	return ""
}

// everyScreen walks a model through each state so a rule can be asserted about
// all of them at once. The states are set directly: what is being checked is
// how a screen draws, not how it is reached.
func everyScreen(t *testing.T, fn func(t *testing.T, m *model, s state)) {
	t.Helper()
	all := []state{
		stateMenu, stateSources, stateMapping, stateCrypto, statePassphrase,
		stateRecipients, stateOutput, stateReview, stateSaveConfig,
		stateRunning, stateDone, statePickArchive, stateArchiveKey,
		stateArchiveInfo, stateRestoreTarget, stateRestoring, stateRestoreDone,
		stateGenerated,
	}
	for _, s := range all {
		m := at(100, 40)
		m.cfg = config.Default()
		m.cfg.Groups = []config.Group{{Name: "g", Dest: "d", Sources: []config.Source{{Path: "/tmp"}}}}
		m.mapping = []mapEntry{{path: "/tmp", dest: "d"}}
		m.plan = &archive.PlanResult{}
		m.generated = "age1xyz"
		m.state = s
		fn(t, m, s)
	}
}

// ---------- the footer ----------

// The footer used to wrap: the source screen listed twelve keys and spilled
// onto a second line, which is a line the body does not get. Whatever the
// terminal's width, the legend is one line now and the keys that do not fit
// are in the overlay instead.
func TestTheFooterIsOneLineAtEveryWidth(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		m := at(width, 40)
		m.state = stateSources
		m.browser.selected["/tmp"] = true

		ls := lines(m)
		var legend []string
		for _, l := range ls {
			if strings.Contains(l, "? keys") {
				legend = append(legend, l)
			}
		}
		if len(legend) != 1 {
			t.Errorf("width %d: the legend covers %d lines, want one:\n%s",
				width, len(legend), strings.Join(legend, "\n"))
		}
	}
}

// The legend is the last thing on the screen. It used to be followed by the
// error, which put the keys between the body and the thing that had just gone
// wrong.
func TestTheFooterIsTheLastLineAndErrorsSitAboveIt(t *testing.T) {
	m := at(100, 40)
	m.state = stateMapping
	m.mapping = []mapEntry{{path: "/tmp/one", dest: ""}}
	m.err = errors.New("something specific went wrong")

	ls := lines(m)
	footer, errLine := -1, -1
	for i, l := range ls {
		if strings.Contains(l, "? keys") {
			footer = i
		}
		if strings.Contains(l, "something specific went wrong") {
			errLine = i
		}
	}
	if footer < 0 || errLine < 0 {
		t.Fatalf("footer at %d, error at %d, in:\n%s", footer, errLine, strings.Join(ls, "\n"))
	}
	if errLine > footer {
		t.Errorf("the error is drawn below the key legend:\n%s", strings.Join(ls, "\n"))
	}
	if footer != len(ls)-1 {
		t.Errorf("the legend is not the last line; %d lines follow it", len(ls)-1-footer)
	}
}

// Every screen names the overlay and the quit key, because the whole point of
// building the footer once is that no screen can forget them. The two screens
// that are mid-run are the exception: they answer to nothing but ctrl+c, and
// offering keys they ignore would be the same defect pointed the other way.
func TestEveryScreenNamesTheKeysItOffers(t *testing.T) {
	everyScreen(t, func(t *testing.T, m *model, s state) {
		foot := footerOf(m)
		if s == stateRunning || s == stateRestoring {
			if !strings.Contains(foot, "ctrl+c abort") || strings.Contains(foot, "? keys") {
				t.Errorf("state %v: a running screen's footer is %q", s, foot)
			}
			return
		}
		if !strings.Contains(foot, "? keys") {
			t.Errorf("state %v: no overlay offered: %q", s, foot)
		}
		if !strings.Contains(foot, "q quit") {
			t.Errorf("state %v: q is bound but unnamed: %q", s, foot)
		}
	})
}

// Enter's verb differs from screen to screen — it is the primary action, and
// that is not the same thing twice — but it stays short enough to leave the
// legend room for the rest. "enter choose a target and restore" did not.
func TestEnterVerbsStayShort(t *testing.T) {
	everyScreen(t, func(t *testing.T, m *model, s state) {
		c := m.chrome()
		if !c.primary.set() {
			return
		}
		if n := len(strings.Fields(c.primary.verb)); n > 2 {
			t.Errorf("state %v: enter is %q, %d words", s, c.primary.verb, n)
		}
	})
}

// ---------- the overlay ----------

// The aliases every list has answered to all along were named nowhere. ?
// is where they live now.
func TestTheOverlayNamesTheKeysTheFooterHasNoRoomFor(t *testing.T) {
	m := at(80, 40)
	m.state = stateSources

	if strings.Contains(plain(m.View()), "first/last") {
		t.Fatal("g/G is on the footer; this test is checking the overlay carries it")
	}
	send(m, "?")
	if !m.showKeys {
		t.Fatal("? did not open the overlay")
	}

	got := plain(m.View())
	for _, want := range []string{"g G", "j k", "pgup", "~  h", "ctrl+c"} {
		if !strings.Contains(got, want) {
			t.Errorf("the overlay does not name %q:\n%s", want, got)
		}
	}

	send(m, "x")
	if m.showKeys {
		t.Error("a key did not close the overlay")
	}
}

// The key that closes the overlay does nothing else: it would be a surprising
// way to exclude a file.
func TestClosingTheOverlayDoesNotActOnTheScreenUnderneath(t *testing.T) {
	m := at(80, 40)
	m.state = stateMapping
	m.mapping = []mapEntry{{path: "/tmp/one"}, {path: "/tmp/two"}}

	send(m, "?", "d")
	if len(m.mapping) != 2 {
		t.Errorf("the keystroke that closed the overlay also removed a row: %d left", len(m.mapping))
	}
}

// ? is a character in a text field, not a command.
func TestTheOverlayDoesNotOpenWhileTyping(t *testing.T) {
	m := at(80, 40)
	m.state = stateRecipients
	m.rcptInput.Focus()

	send(m, "?")
	if m.showKeys {
		t.Error("? opened the overlay while a field had the keyboard")
	}
	if !strings.Contains(m.rcptInput.Value(), "?") {
		t.Errorf("? was swallowed instead of typed: %q", m.rcptInput.Value())
	}
}
