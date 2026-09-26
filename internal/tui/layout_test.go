package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/manifest"
)

// fitsIn says the screen drew inside the terminal it was given.
func fitsIn(t *testing.T, m *model, height int) {
	t.Helper()
	if n := lipgloss.Height(m.View()); n > height {
		t.Errorf("%d rows drawn into a %d-row terminal:\n%s", n, height, plain(m.View()))
	}
}

// The menu, the selection panel and the excludes panel all budgeted their rows
// carefully. These four listed everything they had at two rows an entry, and a
// long enough list pushed the key legend off the bottom of the terminal — so
// the screen stopped saying how to leave it.

func TestTheMappingScreenFitsALongMapping(t *testing.T) {
	for _, height := range []int{16, 24, 40} {
		m := at(100, height)
		m.state = stateMapping
		for i := 0; i < 40; i++ {
			m.mapping = append(m.mapping, mapEntry{
				path: fmt.Sprintf("/home/u/project-%02d", i), dest: "code",
			})
		}
		fitsIn(t, m, height)

		if !strings.Contains(plain(m.View()), "? keys") {
			t.Errorf("height %d: forty sources pushed the key legend off the screen", height)
		}
	}
}

// Scrolling, not truncating: the mapping screen has a cursor, and a cursor you
// cannot see is a cursor you cannot use.
func TestTheMappingCursorStaysOnScreen(t *testing.T) {
	m := at(100, 20)
	m.state = stateMapping
	for i := 0; i < 40; i++ {
		m.mapping = append(m.mapping, mapEntry{
			path: fmt.Sprintf("/home/u/project-%02d", i), dest: "code",
		})
	}
	for i := 0; i < 39; i++ {
		send(m, "down")
	}

	got := plain(m.View())
	if !strings.Contains(got, "project-39") {
		t.Errorf("the cursor ran off the bottom; the screen shows:\n%s", got)
	}
	fitsIn(t, m, 20)
}

func TestTheReviewScreenFitsALongPlan(t *testing.T) {
	for _, height := range []int{16, 24, 40} {
		m := at(100, height)
		m.state = stateReview
		m.cfg = testConfig()
		m.plan = &archive.PlanResult{}
		for i := 0; i < 40; i++ {
			m.plan.Sources = append(m.plan.Sources, archive.PlanSource{
				Path: fmt.Sprintf("/home/u/project-%02d", i), Member: "code",
			})
		}
		fitsIn(t, m, height)
	}
}

func TestTheArchiveListingFitsALongManifest(t *testing.T) {
	for _, height := range []int{16, 24, 40} {
		m := at(100, height)
		m.state = stateArchiveInfo
		m.info = &archive.Info{Manifest: &manifest.Manifest{}}
		for i := 0; i < 20; i++ {
			m.info.Manifest.Groups = append(m.info.Manifest.Groups, manifest.Group{
				Name: fmt.Sprintf("group-%02d", i),
				Sources: []manifest.Source{
					{Path: "/home/u/a", Member: "a"},
					{Path: "/home/u/b", Member: "b"},
				},
			})
		}
		fitsIn(t, m, height)
	}
}

// The review capped its warnings at five and the restore screen did not, for
// no reason beyond the two having been written apart. One helper draws both.
func TestBothScreensCapTheirWarnings(t *testing.T) {
	var many []string
	for i := 0; i < 40; i++ {
		many = append(many, fmt.Sprintf("could not read /home/u/file-%02d", i))
	}

	restore := at(100, 24)
	restore.state = stateRestoreDone
	restore.restored = &archive.RestoreResult{Target: "/tmp/out", Warnings: many}
	fitsIn(t, restore, 24)
	if !strings.Contains(plain(restore.View()), "more") {
		t.Error("the restore screen listed every warning instead of capping them")
	}

	review := at(100, 24)
	review.state = stateReview
	review.cfg = testConfig()
	review.plan = &archive.PlanResult{Warnings: many}
	fitsIn(t, review, 24)
}

// ---------- the two running screens ----------

// A backup drew a progress bar and a restore drew three lines of text, for the
// same kind of operation. The manifest records what was planned, so a restore
// can measure itself against a total the same way.
func TestRestoringShowsProgressAgainstThePlannedTotal(t *testing.T) {
	m := at(100, 24)
	m.state = stateRestoring
	m.info = &archive.Info{Manifest: &manifest.Manifest{
		Planned: &manifest.Stats{Files: 100, Bytes: 1 << 20},
	}}
	m.lastProg = archive.Progress{Files: 50, Bytes: 1 << 19, Member: "dotfiles/.ssh/config"}

	got := plain(m.View())
	if !strings.Contains(got, "of 1.0 MiB") {
		t.Errorf("no total to measure against:\n%s", got)
	}
	if !strings.Contains(got, "█") {
		t.Errorf("no progress bar on the restore screen:\n%s", got)
	}
	if !strings.Contains(got, "ctrl+c abort") {
		t.Errorf("a running screen must still say how to stop:\n%s", got)
	}
}

// An archive written before the manifest recorded a total has none, and a bar
// with nothing behind it would be a bar that lies.
func TestRestoringWithoutATotalShowsNoBar(t *testing.T) {
	m := at(100, 24)
	m.state = stateRestoring
	m.info = &archive.Info{Manifest: &manifest.Manifest{}}
	m.lastProg = archive.Progress{Files: 50, Bytes: 1 << 19}

	got := plain(m.View())
	if strings.Contains(got, "█") {
		t.Errorf("a bar was drawn with no total behind it:\n%s", got)
	}
	if !strings.Contains(got, "50 files") {
		t.Errorf("the screen stopped saying how far along it is:\n%s", got)
	}
}

// ---------- alignment ----------

// Descriptions sat at column 0 on five screens and column 2 on two others, and
// on the archive-key screen the description was under the field instead of
// above it. They are drawn in one place now, so they cannot disagree.
func TestDescriptionsShareOneIndent(t *testing.T) {
	everyScreen(t, func(t *testing.T, m *model, s state) {
		c := m.chrome()
		if len(c.subtitle) == 0 {
			return
		}
		// The description sits two columns in from the title, and View indents
		// the whole screen by two more.
		head := strings.Split(wrap(c.subtitle[0], m.inner()-2), "\n")[0]
		found := false
		for _, l := range lines(m) {
			if !strings.Contains(l, head) {
				continue
			}
			found = true
			if !strings.HasPrefix(l, "    "+head) {
				t.Errorf("state %v: the description is not at the shared indent.\ngot  %q\nwant %q",
					s, l, "    "+head)
			}
			break
		}
		if !found {
			t.Errorf("state %v: the description %q was not drawn at all", s, head)
		}
	})
}
