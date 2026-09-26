package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/config"
)

// A valid age recipient, so the recipients screen gets past its own
// validation and reaches the notice this is about.
const testRecipient = "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Groups = []config.Group{{
		Name: "dotfiles", Dest: "dotfiles",
		Sources: []config.Source{{Path: "/tmp"}},
	}}
	return cfg
}

// quits reports whether a batch of keys asked Bubble Tea to leave.
func quits(m *model, keys ...string) bool {
	for _, k := range keys {
		_, cmd := m.Update(key(k))
		if cmd == nil {
			continue
		}
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				return true
			}
		}
	}
	return false
}

// ---------- q ----------

// q used to quit outright from the source screen, discarding a selection that
// may have taken several directories to build, and the footer did not even
// name the key. It asks now, the way the save screen asks before replacing a
// file.
func TestQAsksBeforeDiscardingASelection(t *testing.T) {
	m := at(100, 40)
	m.state = stateSources
	m.browser.selected["/tmp/one"] = true
	m.browser.selected["/tmp/two"] = true

	if quits(m, "q") {
		t.Fatal("q quit with two items selected, without asking")
	}
	if !m.confirmQuit {
		t.Fatal("q did not arm the confirmation")
	}
	if got := plain(m.View()); !strings.Contains(got, "2 items chosen") ||
		!strings.Contains(got, "Press q again") {
		t.Errorf("the screen does not say what would be lost:\n%s", got)
	}
	if !quits(m, "q") {
		t.Error("a second q did not quit")
	}
}

// The confirmation is a question, not a mode: doing anything else answers it.
func TestAnyOtherKeyTakesBackAPendingQuit(t *testing.T) {
	m := at(100, 40)
	m.state = stateSources
	m.browser.selected["/tmp/one"] = true

	send(m, "q", "down")
	if m.confirmQuit {
		t.Fatal("moving the cursor left the quit armed")
	}
	if quits(m, "q") {
		t.Error("q quit at once after an intervening key; the confirmation did not reset")
	}
}

// Warning when there is nothing to lose is worse than not warning at all: it
// teaches the habit of pressing q twice, and then the confirmation on the
// screens that need one is no longer read.
func TestQDoesNotAskWhenThereIsNothingToLose(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*model)
	}{
		{"the menu", func(m *model) { m.state = stateMenu }},
		{"an empty selection", func(m *model) { m.state = stateSources }},
		{"a finished backup", func(m *model) { m.state = stateDone }},
		{"a read-only archive listing", func(m *model) { m.state = stateArchiveInfo }},
		{"a configuration already on disk", func(m *model) {
			m.state, m.fromDisk = stateReview, true
		}},
	}
	for _, tc := range cases {
		m := at(100, 40)
		tc.setup(m)
		if !quits(m, "q") {
			t.Errorf("%s: q did not quit, though nothing was at risk", tc.name)
		}
	}
}

// In a field, q is a letter. It used to quit from the excludes panel and type
// one keystroke later in that panel's own input.
func TestQIsACharacterWhileTyping(t *testing.T) {
	m := at(100, 40)
	m.state = stateRecipients
	m.rcptInput.Focus()

	if quits(m, "q") {
		t.Fatal("q quit while a field had the keyboard")
	}
	if m.rcptInput.Value() != "q" {
		t.Errorf("the field reads %q, want the letter to have been typed", m.rcptInput.Value())
	}
}

// The excludes panel is not a field, so q is a command there — but the input
// it opens is, so one keystroke later the same key is a letter. The two used
// to disagree about that for no reason a user could see.
func TestTheExcludesPanelAndItsInputAgreeAboutQ(t *testing.T) {
	m := at(100, 40)
	m.state = stateSources
	m.mode = srcExcludes
	if m.typing() {
		t.Error("the excludes panel claims to be a text field")
	}
	m.mode = srcExcludeInput
	if !m.typing() {
		t.Error("the excludes input does not claim the keyboard")
	}
}

// ---------- one key, one verb ----------

// d opened a directory on the source screen and deleted a row on the next one,
// with no confirmation and no undo. It only removes now, and p is the path.
func TestDNoLongerOpensTheDirectoryField(t *testing.T) {
	m := at(100, 40)
	m.state = stateSources
	m.openSources()

	send(m, "d")
	if m.mode == srcPath {
		t.Error("d still opens the directory field")
	}
	send(m, "p")
	if m.mode != srcPath {
		t.Errorf("p did not open the directory field: mode = %v", m.mode)
	}
}

// x toggles an exclude, and that is all it does. It used to double as a delete
// key inside the excludes panel.
func TestXDoesNotRemoveAnExcludePattern(t *testing.T) {
	m := at(100, 40)
	m.state = stateSources
	m.browser.selected["/tmp/src"] = true
	m.srcExclude["/tmp/src"] = []string{"node_modules"}
	m.mode = srcExcludes
	m.excIndex = 1 // the pattern row

	send(m, "x")
	if len(m.srcExclude["/tmp/src"]) != 1 {
		t.Error("x removed the pattern")
	}
	send(m, "d")
	if len(m.srcExclude["/tmp/src"]) != 0 {
		t.Errorf("d did not remove the pattern: %v", m.srcExclude["/tmp/src"])
	}
}

// ---------- errors ----------

// An error belongs to the keystroke that caused it and the one that answers
// it. It used to survive the arrows on the mapping screen, sitting there while
// the user worked on something else.
func TestAnErrorLastsOneKeystroke(t *testing.T) {
	m := at(100, 40)
	m.state = stateMapping
	m.mapping = []mapEntry{{path: "/tmp/one"}, {path: "/tmp/two"}}
	m.err = errors.New("stale complaint")

	send(m, "down")
	if m.err != nil {
		t.Errorf("the error survived a keystroke that had nothing to do with it: %v", m.err)
	}
}

// ---------- notices ----------

// One field carried every notice and each screen drew it however it liked, so
// the single-recipient warning — which is about losing an archive forever —
// came out as a green tick on the entry screen.
func TestAWarningLooksLikeAWarningOnEveryScreen(t *testing.T) {
	warned := notice{"Only one recipient.", levelWarn}
	for _, s := range []state{stateMenu, stateReview, stateDone, stateGenerated} {
		m := at(100, 40)
		m.state = s
		m.notice = warned

		got := plain(m.View())
		if !strings.Contains(got, "! Only one recipient.") {
			t.Errorf("state %v does not draw the warning as one:\n%s", s, got)
		}
		if strings.Contains(got, "✓ Only one recipient.") {
			t.Errorf("state %v draws the warning as a success", s)
		}
	}
}

// The severity is set where the message is written, which is the only place
// that knows it.
func TestTheRecipientWarningIsWrittenAsAWarning(t *testing.T) {
	m := at(100, 40)
	m.cfg = testConfig()
	m.rcptInput.SetValue(testRecipient)
	m.state = stateRecipients

	m.acceptRecipients()
	if m.notice.level != levelWarn {
		t.Errorf("a single recipient was recorded at level %v, want a warning", m.notice.level)
	}
}

// A success still looks like one; the point was severity, not sobriety.
func TestASavedConfigurationIsRecordedAsASuccess(t *testing.T) {
	m := at(100, 40)
	m.cfg = testConfig()

	if err := m.writeConfig(t.TempDir()+"/arca.toml", true); err != nil {
		t.Fatal(err)
	}
	if m.notice.level != levelOK {
		t.Errorf("saving was recorded at level %v, want a success", m.notice.level)
	}
	if got := plain(m.render(m.viewMenu())); !strings.Contains(got, "✓ Saved") {
		t.Errorf("the entry screen does not draw it as a success:\n%s", got)
	}
}

// The done screen prints where s would write and s writes there. They used to
// be resolved separately, so a configuration read from disk and then edited
// had the screen naming one path and the key writing another.
func TestTheDoneScreenSavesWhereItSaysItWill(t *testing.T) {
	m := at(100, 40)
	m.state = stateDone
	m.cfg = testConfig()
	m.cfgPath = t.TempDir() + "/from-disk.toml"
	m.result = &archive.BackupResult{Path: "/tmp/archive.tar.zst.age"}

	said := m.saveTarget()
	if got := plain(m.View()); !strings.Contains(got, said) {
		t.Fatalf("the screen does not name %q:\n%s", said, got)
	}

	send(m, "s")
	if m.err != nil {
		t.Fatalf("saving failed: %v", m.err)
	}
	if m.cfgPath != said {
		t.Errorf("s wrote to %q after the screen named %q", m.cfgPath, said)
	}
}
