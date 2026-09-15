package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The entry screen carries the same mark as `arca --version`, so the two
// front-ends read as one tool. It gives way as the terminal shrinks: the menu
// and its help line matter more than the artwork.
func TestEntryScreenShowsTheFullMarkWhenThereIsRoom(t *testing.T) {
	view := plain(menuAt(t, 92, 44))

	if !strings.Contains(view, "____________________") {
		t.Errorf("a tall terminal should show the ark:\n%s", view)
	}
	if !strings.Contains(view, "▄▀▀▀█") && !strings.Contains(view, `\__,_|_|`) {
		t.Errorf("a tall terminal should show the lettering:\n%s", view)
	}
}

func TestEntryScreenDropsTheLetteringOnAStandardTerminal(t *testing.T) {
	view := plain(menuAt(t, 80, 25))

	if !strings.Contains(view, "____________________") {
		t.Errorf("25 rows still has room for the ark:\n%s", view)
	}
	if strings.Contains(view, "▄▀▀▀█") || strings.Contains(view, `\__,_|_|`) {
		t.Errorf("25 rows does not have room for the lettering too:\n%s", view)
	}
	if !strings.Contains(view, "Quit") {
		t.Errorf("the whole menu must still fit:\n%s", view)
	}
}

func TestEntryScreenDropsTheMarkOnAShortTerminal(t *testing.T) {
	view := plain(menuAt(t, 80, 18))

	if strings.Contains(view, "____________________") {
		t.Errorf("a short terminal needs its rows for the menu:\n%s", view)
	}
	if !strings.Contains(view, "arca") || !strings.Contains(view, "Quit") {
		t.Errorf("the tool must still be named and the menu complete:\n%s", view)
	}
}

// The mark must never push the menu off the screen. The notice a key
// generation leaves behind is the menu's tallest state, so it is the one the
// thresholds have to clear.
func TestEntryScreenFitsTheTerminal(t *testing.T) {
	for _, withNotice := range []bool{false, true} {
		// The menu alone has a floor height; below it nothing fits and the
		// screen shows no artwork anyway. The invariant starts where it fits.
		floor := menuLines(t, withNotice, 1)
		for height := floor; height <= 60; height++ {
			if n := menuLines(t, withNotice, height); n > height {
				t.Fatalf("at %d rows (notice=%v) the entry screen renders %d lines",
					height, withNotice, n)
			}
		}
	}
}

func menuLines(t *testing.T, withNotice bool, height int) int {
	t.Helper()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 92, Height: height})
	if withNotice {
		m.notice = "wrote /home/u/.config/arca/identity.age"
	}
	return strings.Count(plain(m.viewMenu()), "\n") + 1
}

func TestScreenHeaderIsJustTheName(t *testing.T) {
	got := plain(header("Anything", "crumb"))
	if strings.Contains(got, `[~\_/~]`) {
		t.Errorf("the one-line mark was removed from the header, got:\n%s", got)
	}
	if !strings.HasPrefix(got, "arca  crumb") {
		t.Errorf("the header should start with the plain name, got:\n%s", got)
	}
}

func menuAt(t *testing.T, width, height int) string {
	t.Helper()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return m.viewMenu()
}

// plain drops styling so assertions are about layout, not colour.
func plain(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
