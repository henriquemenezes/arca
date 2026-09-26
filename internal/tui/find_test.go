package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// search opens the finder on the source screen, types a query, and lets any
// index it asked for finish, so a test looks at the screen the user would once
// the scan has settled.
func search(t *testing.T, m *model, query string) {
	t.Helper()
	m.Update(key("/"))
	typed(m, query)
	drainIndex(t, m)
}

// drainIndex reads the scan's batches straight off the channel rather than
// through run(). Focusing the query field returns a blink command, and calling
// that synchronously would make every one of these tests sleep.
//
// A query that never needed an index has no channel to drain, which is the
// normal case for a path.
func drainIndex(t *testing.T, m *model) {
	t.Helper()
	f := m.browser.filter
	if f == nil {
		t.Fatal("no search is open")
	}
	if f.ch == nil {
		return
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg, ok := <-f.ch:
			if !ok {
				return
			}
			m.Update(msg)
			if msg.done || msg.truncated {
				return
			}
		case <-deadline:
			t.Fatal("the index never finished")
		}
	}
}

// typed sends a query one key at a time, the way it is really entered.
func typed(m *model, query string) {
	for _, r := range query {
		m.Update(key(string(r)))
	}
}

// rowNames is what the browser is currently listing.
func rowNames(m *model) []string {
	var out []string
	for _, e := range m.browser.list() {
		out = append(out, e.name)
	}
	return out
}

// deepTree is a home-shaped tree: the same basename in three places, one of
// them hidden, so both ranking and the hidden-files rule have something to say.
func deepTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		".config/nvim",
		".local/share/nvim",
		"Work/notes",
		"Work/nvim-plugins",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, ".config", "nvim", "init.lua"), []byte("vim"), 0o644)
	return root
}

func TestSearchReachesAPathSeveralDirectoriesDown(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	search(t, m, "nvim")

	names := rowNames(m)
	for _, want := range []string{".config/nvim", ".local/share/nvim", "Work/nvim-plugins"} {
		if !contains(names, want) {
			t.Errorf("the search did not find %q; it listed %v", want, names)
		}
	}
	if contains(names, "Work/notes") {
		t.Errorf("the search listed %q, which does not match the query", "Work/notes")
	}
}

func TestSearchMatchesASubstringSpanningASeparator(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	search(t, m, "config/nvim")

	if names := rowNames(m); !contains(names, ".config/nvim") {
		t.Errorf("a substring spanning a directory boundary found %v, want .config/nvim", names)
	}
}

// Matching is exact. Scattered letters used to match almost everything, which
// is what made the list untrustworthy.
func TestSearchDoesNotMatchScatteredLetters(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	search(t, m, "cfgnv")

	if names := rowNames(m); len(names) != 0 {
		t.Errorf("a subsequence still matched %v, want nothing", names)
	}
}

// The letters have to be there, but the case does not.
func TestSearchIgnoresCase(t *testing.T) {
	home := noisyHome(t)
	for _, query := range []string{"Down", "down", "DOWN", "dOwNlOaDs"} {
		m := sourcesModel(t, home, 100, 40)
		search(t, m, query)

		rows := m.browser.list()
		if len(rows) == 0 {
			t.Errorf("%q matched nothing", query)
			continue
		}
		if want := filepath.Join(home, "Downloads"); rows[0].path != want {
			t.Errorf("%q put %q first, want %q", query, rows[0].path, want)
		}
	}
}

func TestSearchLeavesHiddenPathsOutOnceTheyAreHidden(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	m.browser.hidden = false
	search(t, m, "nvim")

	names := rowNames(m)
	if contains(names, ".config/nvim") {
		t.Errorf("a hidden path was offered while hidden files are off: %v", names)
	}
	if !contains(names, "Work/nvim-plugins") {
		t.Errorf("the visible match went missing: %v", names)
	}
}

// Everything printable belongs to the query. These two letters are the ones
// that would otherwise quit the screen and move the cursor.
func TestSearchTypesOrdinaryLettersInsteadOfActingOnThem(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "qk-directory"), 0o755)

	m := sourcesModel(t, root, 100, 40)
	search(t, m, "qk")

	if m.state != stateSources {
		t.Fatalf("typing into the search left the screen: state %v", m.state)
	}
	if got := m.browser.filter.input.Value(); got != "qk" {
		t.Errorf("query is %q, want %q", got, "qk")
	}
}

func TestLeavingTheSearchRestoresTheListing(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	before := rowNames(m)

	search(t, m, "nvim")
	send(m, "esc")

	if m.mode != srcBrowse {
		t.Errorf("mode is %v, want srcBrowse", m.mode)
	}
	if m.browser.filter != nil {
		t.Error("the filter outlived the search")
	}
	if got := rowNames(m); !reflect.DeepEqual(got, before) {
		t.Errorf("the listing came back as %v, want %v", got, before)
	}
}

func TestOpeningAMatchWalksThereAndEndsTheSearch(t *testing.T) {
	root := deepTree(t)
	m := sourcesModel(t, root, 100, 40)
	search(t, m, "Work/nvim-plugins")
	send(m, "enter")

	if want := filepath.Join(root, "Work", "nvim-plugins"); m.browser.cwd != want {
		t.Errorf("the browser is at %q, want %q", m.browser.cwd, want)
	}
	if m.mode != srcBrowse || m.browser.filter != nil {
		t.Error("opening a match left the search running")
	}
}

func TestSelectingFromTheSearchChoosesTheWholePath(t *testing.T) {
	root := deepTree(t)
	m := sourcesModel(t, root, 100, 40)
	search(t, m, "config/nvim")
	send(m, " ")

	want := filepath.Join(root, ".config", "nvim")
	if !m.browser.selected[want] {
		t.Errorf("space selected %v, want %q", m.browser.Selected(), want)
	}
}

// A symlink back up the tree is the classic way to make a walker run forever.
// The scan records links and never descends them, so this simply has to end.
func TestTheScanDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "real", "deeper"), 0o755)
	if err := os.Symlink(root, filepath.Join(root, "real", "loop")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	m := sourcesModel(t, root, 100, 40)
	search(t, m, "deeper") // fails the test by timeout if the scan loops

	if names := rowNames(m); !contains(names, "real/deeper") {
		t.Errorf("the index missed the real tree: %v", names)
	}
}

func TestABatchFromAnAbandonedSearchChangesNothing(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	search(t, m, "nvim")

	stale := indexMsg{
		gen:     m.browser.filter.gen - 1,
		entries: []indexEntry{{rel: "ghost", isDir: true}},
		done:    true,
	}
	before := len(m.browser.filter.entries)
	m.Update(stale)

	if got := len(m.browser.filter.entries); got != before {
		t.Errorf("the index grew from %d to %d on a stale batch", before, got)
	}
	if contains(rowNames(m), "ghost") {
		t.Error("a stale batch reached the screen")
	}
}

func TestClosingTheFilterTwiceIsHarmless(t *testing.T) {
	m := sourcesModel(t, deepTree(t), 100, 40)
	f := m.browser.filter
	f.close() // nil receiver
	search(t, m, "nvim")
	m.browser.filter.close()
	m.browser.filter.close()
}

func TestTheSearchKeepsToItsColumn(t *testing.T) {
	root := deepTree(t)
	os.MkdirAll(filepath.Join(root, "Work", strings.Repeat("long-name-", 9)), 0o755)

	m := sourcesModel(t, root, 100, 40)
	search(t, m, "long")

	for _, line := range strings.Split(strings.TrimRight(m.browser.View(30, nil), "\n"), "\n") {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line is %d columns wide, want at most 30: %q", w, line)
		}
	}
}

func TestMarkQueryMarksTheRunThatMatched(t *testing.T) {
	cases := []struct {
		text, query string
		want        []int
	}{
		{"nvim", "nv", []int{0, 1}},
		{"nvim", "NV", []int{0, 1}},
		// The name at the end is tried first, so "nv" lights up in "nvim" and
		// not in the ".config" above it.
		{".config/nvim", "nv", []int{8, 9}},
		{".config/nvim", "config", []int{1, 2, 3, 4, 5, 6}},
		{"…nfig/nvim", "config", nil}, // the head is gone, so nothing lights up
		{".config/nvim", "cfgnv", nil},
		{"nvim", "", nil},
	}
	for _, c := range cases {
		got := markQuery(c.text, c.query)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("markQuery(%q, %q) = %v, want %v", c.text, c.query, got, c.want)
		}
	}
}

// ---------- a query spelled as a path is completed, not searched for ----------

// noisyHome is the shape that made the old search useless: the thing you mean
// sits one level down, and thousands of paths that merely carry the same
// letters sit under a cache nobody names on purpose.
func noisyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, dir := range []string{
		"Downloads",
		"Work/site/downloads-archive",
		".cache/chromium/Default/downloads",
		".local/share/apps/downloader",
		".config/nvim",
	} {
		if err := os.MkdirAll(filepath.Join(home, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestATypedPathListsExactlyThatDirectory(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, home+string(filepath.Separator))

	if m.browser.filter.listing != home {
		t.Errorf("listing is %q, want %q", m.browser.filter.listing, home)
	}
	want := []string{".cache", ".config", ".local", "Downloads", "Work"}
	if got := rowNames(m); !reflect.DeepEqual(got, want) {
		t.Errorf("listed %v, want %v", got, want)
	}
}

func TestATypedPathNarrowsByItsLastSegment(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, filepath.Join(home, "Dow"))

	if got := rowNames(m); !reflect.DeepEqual(got, []string{"Downloads"}) {
		t.Errorf("listed %v, want [Downloads]", got)
	}
}

// Typing "/" is the plainest case there is: what is in the root, and nothing
// from anywhere else.
func TestASlashListsTheRoot(t *testing.T) {
	m := sourcesModel(t, t.TempDir(), 100, 40)
	search(t, m, "/")

	rows := m.browser.list()
	if len(rows) == 0 {
		t.Fatal("the root listed nothing")
	}
	for _, e := range rows {
		if got := filepath.Dir(e.path); got != "/" {
			t.Errorf("%q is not directly under the root (its parent is %q)", e.path, got)
		}
	}
}

// A path never starts a scan: the directory it names is read directly.
func TestATypedPathNeverBuildsAnIndex(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, home+string(filepath.Separator))

	if m.browser.filter.started {
		t.Error("completing a path built an index it had no use for")
	}
}

func TestAnEmptyQueryStaysOnTheCurrentDirectory(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	before := rowNames(m)
	search(t, m, "")

	if got := rowNames(m); !reflect.DeepEqual(got, before) {
		t.Errorf("opening the search changed the listing to %v, want %v", got, before)
	}
	if m.browser.filter.started {
		t.Error("opening the search built an index before anything was typed")
	}
}

// ---------- a bare name is ranked, name first ----------

func TestTheDirectoryActuallyCalledThatComesFirst(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, "Downloads")

	rows := m.browser.list()
	if len(rows) == 0 {
		t.Fatal("nothing matched")
	}
	if want := filepath.Join(home, "Downloads"); rows[0].path != want {
		t.Errorf("the first match is %q, want %q (all of them: %v)", rows[0].path, want, rowNames(m))
	}
}

func TestAShallowNameBeatsADeeperOneOfTheSameName(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "notes"), 0o755)
	os.MkdirAll(filepath.Join(home, "Work", "site", "notes"), 0o755)

	m := sourcesModel(t, home, 100, 40)
	search(t, m, "notes")

	if got := rowNames(m); len(got) < 2 || got[0] != "notes" {
		t.Errorf("ranked %v, want the shallow notes first", got)
	}
}

// Dot directories are pushed down, not hidden: ~/.ssh is exactly what "ssh"
// meant, while a cache three levels down is not.
func TestAHiddenPathIsRankedDownButStillReachable(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o755)
	os.MkdirAll(filepath.Join(home, ".cache", "x", "y", "sshfs-stuff"), 0o755)

	m := sourcesModel(t, home, 100, 40)
	search(t, m, "ssh")

	rows := rowNames(m)
	if len(rows) == 0 || rows[0] != ".ssh" {
		t.Errorf("ranked %v, want .ssh first", rows)
	}
	if !contains(rows, ".cache/x/y/sshfs-stuff") {
		t.Errorf("the deeper hidden match was dropped rather than ranked down: %v", rows)
	}
}

func TestAnExactNameOutranksALongerOneThatContainsIt(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, "downloads")

	rows := m.browser.list()
	if want := filepath.Join(home, "Downloads"); rows[0].path != want {
		t.Errorf("the first match is %q, want %q", rows[0].path, want)
	}
	// The near-misses are still offered, just not first.
	if !contains(rowNames(m), "Work/site/downloads-archive") {
		t.Errorf("a real near-miss was dropped: %v", rowNames(m))
	}
}

// The depth cap is what keeps a cache from burying everything else. Anything
// past it stays reachable by walking there or spelling it out.
func TestTheScanStopsAtTheDepthCap(t *testing.T) {
	home := t.TempDir()
	deep := home
	for i := 0; i < maxIndexDepth+2; i++ {
		deep = filepath.Join(deep, "level")
	}
	if err := os.MkdirAll(filepath.Join(deep, "needle"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := sourcesModel(t, home, 100, 40)
	search(t, m, "needle")

	if len(m.browser.list()) != 0 {
		t.Errorf("a path below the depth cap was indexed: %v", rowNames(m))
	}
	for _, e := range m.browser.filter.entries {
		if e.depth > maxIndexDepth {
			t.Fatalf("%q was indexed at depth %d, past the cap of %d", e.rel, e.depth, maxIndexDepth)
		}
	}
}

func TestScoringPutsTheTiersInOrder(t *testing.T) {
	at := func(rel string, depth int, hidden bool) *indexEntry {
		return &indexEntry{rel: rel, depth: depth, hidden: hidden}
	}
	ranked := []struct {
		name  string
		entry *indexEntry
	}{
		{"exact name", at("Downloads", 1, false)},
		{"name prefix", at("Downloads-old", 1, false)},
		{"name substring", at("my-downloads", 1, false)},
		{"path substring", at("downloads/inner", 2, false)},
	}
	last := scoreNameExact + 1
	for _, r := range ranked {
		score, ok := scoreEntry(r.entry, "downloads")
		if !ok {
			t.Fatalf("%s did not match at all", r.name)
		}
		if score >= last {
			t.Errorf("%s scored %d, which is not below the tier above it (%d)", r.name, score, last)
		}
		last = score
	}
	for _, miss := range []string{"unrelated", "d-o-w-n-l-o-a-d-s", "dwnlds"} {
		if _, ok := scoreEntry(at(miss, 1, false), "downloads"); ok {
			t.Errorf("%q matched, but the letters are not in it next to each other", miss)
		}
	}
}

// A typed-out path is longer than anything in the list beside it. Left alone
// the field would widen the browser's whole block and shove the panel off the
// screen, so the column has to hold in both modes.
func TestTheSearchFieldStaysInsideItsColumn(t *testing.T) {
	home := noisyHome(t)
	for _, query := range []string{
		"nvim",
		filepath.Join(home, "Work", "site", "downloads-archive") + string(filepath.Separator),
	} {
		m := sourcesModel(t, home, 100, 40)
		search(t, m, query)

		browserWidth, _ := m.sourcesLayout()
		for _, line := range strings.Split(m.browser.View(browserWidth, nil), "\n") {
			if w := lipgloss.Width(line); w > browserWidth {
				t.Errorf("query %q: line is %d columns wide, want at most %d: %q",
					query, w, browserWidth, line)
			}
		}
		for _, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > 100 {
				t.Errorf("query %q: the screen is %d columns wide, want at most 100", query, w)
			}
		}
	}
}

// ---------- tab completes ----------

// The cursor starts on the first row, so tab with nothing moved takes the first
// suggestion. Having moved, it takes what is highlighted: a tab that jumped back
// to the top would undo the arrows that just chose.
func TestTabTakesTheHighlightedSuggestion(t *testing.T) {
	home := noisyHome(t)

	m := sourcesModel(t, home, 100, 40)
	search(t, m, "Down")
	send(m, "tab")

	want := filepath.Join(home, "Downloads")
	if got := m.browser.filter.input.Value(); got != want {
		t.Errorf("tab filled in %q, want %q", got, want)
	}

	m = sourcesModel(t, home, 100, 40)
	search(t, m, home+string(filepath.Separator))
	send(m, "down") // off the first row
	second := m.browser.list()[1].path
	send(m, "tab")

	if got := m.browser.filter.input.Value(); !strings.HasPrefix(got, second) {
		t.Errorf("tab filled in %q, want the highlighted %q", got, second)
	}
}

// A completed directory stops at its own name. What is chosen on this screen is
// usually the directory itself, so it has to stay a row the cursor is on and
// space can mark — which listing the inside of it would take away.
func TestTabOnADirectoryStopsAtTheDirectory(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, filepath.Join(home, "Wo"))
	send(m, "tab")

	want := filepath.Join(home, "Work")
	if got := m.browser.filter.input.Value(); got != want {
		t.Errorf("tab filled in %q, want %q", got, want)
	}
	if got := rowNames(m); !reflect.DeepEqual(got, []string{"Work"}) {
		t.Errorf("listed %v, want [Work]", got)
	}
	if got, _ := m.browser.current(); got.path != want {
		t.Errorf("the cursor is on %q, want the completed %q", got.path, want)
	}
}

// The directory a tab just completed is the one thing the next space has to be
// able to mark: completing it and choosing it is the whole gesture.
func TestTabLeavesTheCompletedDirectorySelectable(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, "Down")
	send(m, "tab")
	send(m, " ")

	want := filepath.Join(home, "Downloads")
	if !m.browser.selected[want] {
		t.Errorf("space after tab chose %v, want %q", m.browser.Selected(), want)
	}
}

// Going inside is the second tab: once the query spells the directory out,
// there is nothing left to complete, so tab adds the separator that lists it.
func TestASecondTabListsTheInsideOfTheDirectory(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, filepath.Join(home, "Wo"))
	send(m, "tab")
	send(m, "tab")

	if want := filepath.Join(home, "Work"); m.browser.filter.listing != want {
		t.Errorf("listing is %q, want %q", m.browser.filter.listing, want)
	}
	if got := rowNames(m); !reflect.DeepEqual(got, []string{"site"}) {
		t.Errorf("listed %v, want [site]", got)
	}
}

// Completing a name hands back a path, so searching for something and then
// walking around inside it are one gesture rather than two.
func TestTabTurnsAFoundNameIntoAPath(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, "nvim")
	send(m, "tab")

	want := filepath.Join(home, ".config", "nvim")
	if got := m.browser.filter.input.Value(); got != want {
		t.Errorf("tab filled in %q, want %q", got, want)
	}
	// The path query that results lists the directory above, narrowed to the
	// name that was found.
	if got := m.browser.filter.listing; got != filepath.Join(home, ".config") {
		t.Errorf("listing is %q, want %q", got, filepath.Join(home, ".config"))
	}
	send(m, "tab")
	if got := m.browser.filter.listing; got != want {
		t.Errorf("a second tab listed %q, want the inside of %q", got, want)
	}
}

// tab used to leave the screen. It completes now, so ctrl+d is what continues.
func TestTabNoLongerLeavesTheSearch(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	m.browser.selected[filepath.Join(home, "Downloads")] = true
	search(t, m, "Down")

	send(m, "tab")
	if m.state != stateSources || m.mode != srcFilter {
		t.Fatalf("tab left the search: state %v, mode %v", m.state, m.mode)
	}
	send(m, "ctrl+d")
	if m.state != stateMapping {
		t.Errorf("ctrl+d did not continue: state %v", m.state)
	}
}

// Nothing to complete is not an error, and must not clear what was typed.
func TestTabOnNoMatchesLeavesTheQueryAlone(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, "nothing-is-called-this")
	send(m, "tab")

	if got := m.browser.filter.input.Value(); got != "nothing-is-called-this" {
		t.Errorf("the query became %q", got)
	}
	if m.err != nil {
		t.Errorf("tab with nothing to complete reported %v", m.err)
	}
}

// ---------- what the rows say ----------

// The prompt used to be "/ ", which read as a path already being listed, and
// doubled the moment the user typed the "/" that really does list the root.
func TestTheSearchPromptIsNotMistakenForAPath(t *testing.T) {
	m := sourcesModel(t, noisyHome(t), 100, 40)
	m.Update(key("/"))

	field := func() string {
		return strings.SplitN(m.browser.View(100, nil), "\n", 2)[0]
	}
	if got := strings.TrimSpace(field()); strings.HasPrefix(got, "/") {
		t.Errorf("the empty field reads %q, which starts as a path does", got)
	}

	typed(m, "/")
	line := field()
	if strings.Contains(line, "/ /") {
		t.Errorf("typing a slash doubled it: %q", line)
	}
	if !strings.Contains(line, searchPrompt+"/") {
		t.Errorf("the field reads %q, want the typed slash after %q", line, searchPrompt)
	}
}

// A completed path is answered at its head. Typing "/" asks about the root, so
// it is the leading separator that lights up, not the one at the end of a name.
func TestATypedPathIsMarkedAtItsHead(t *testing.T) {
	m := sourcesModel(t, t.TempDir(), 100, 40)
	search(t, m, "/")

	rows := m.browser.list()
	if len(rows) == 0 {
		t.Fatal("the root listed nothing")
	}
	// A directory is drawn with its separator, which is where the old
	// behaviour put the mark.
	if got := m.browser.filter.mark(rows[0].path + "/"); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("marked %v in %q, want the leading separator alone", got, rows[0].path)
	}
}

// The mark is worked out from what the completion resolved, not from how the
// query happened to be spelled, so a "~/" lights up the home directory it
// stands for rather than nothing at all.
func TestAPathSpelledWithATildeIsMarkedAllTheSame(t *testing.T) {
	f, _ := openFilter(1, t.TempDir())
	defer f.close()
	f.input.SetValue("~/Dow")
	f.listing, f.leaf = "/home/u", "Dow"

	want := make([]int, len("/home/u/Dow"))
	for i := range want {
		want[i] = i
	}
	if got := f.mark("/home/u/Downloads/"); !reflect.DeepEqual(got, want) {
		t.Errorf("marked %v, want the %d characters the query stands for", got, len(want))
	}
}

// Every row says where it is, because the rows come from all over the tree.
func TestTheSearchDrawsWholePaths(t *testing.T) {
	home := noisyHome(t)
	m := sourcesModel(t, home, 100, 40)
	search(t, m, "downloads")

	view := m.browser.View(200, nil)
	for _, want := range []string{
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Work", "site", "downloads-archive"),
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the list does not spell out %q:\n%s", want, view)
		}
	}
}

// A path too long for the column loses whole directories off its front, which
// is the part every row on screen has in common, and keeps the end, which is
// the part that tells one row from another.
func TestAPathTooLongForTheColumnKeepsItsEnd(t *testing.T) {
	sep := string(filepath.Separator)
	p := filepath.Join(sep, "home", "user", "Work", "site", "downloads-archive")

	if got := fitPath(p, 80); got != p {
		t.Errorf("a path that fits was shortened to %q", got)
	}

	got := fitPath(p, 30)
	if w := len([]rune(got)); w > 30 {
		t.Errorf("%q is %d columns wide, want at most 30", got, w)
	}
	if !strings.HasSuffix(got, filepath.Join("site", "downloads-archive")) {
		t.Errorf("%q kept less of the end than the width allows", got)
	}
	if !strings.HasPrefix(got, "…"+sep) {
		t.Errorf("%q does not say that anything was left out", got)
	}

	// Two matches under one root have to stay two different lines.
	a := fitPath(filepath.Join(sep, "home", "user", ".config", "nvim"), 20)
	b := fitPath(filepath.Join(sep, "home", "user", ".local", "share", "nvim"), 20)
	if a == b {
		t.Errorf("two different paths both drew as %q", a)
	}
}

// The scan hands back one whole level before any of the next.
//
// This is the property that makes the entry cap safe, and the reason it is
// worth a test of its own: the cap applies at the tail of the scan, so an
// index whose depths never decrease can only ever lose its deepest paths.
// Walking in depth broke exactly this — under a real home directory the cap
// fell inside ~/.cache, the eleventh entry of the first level, and ~/Downloads
// was never indexed at all. A name that is not in the index cannot be found,
// and no amount of ranking rescues it.
func TestTheScanFinishesALevelBeforeStartingTheNext(t *testing.T) {
	home := t.TempDir()
	// A subtree that sorts first and is deep, beside the shallow name that a
	// depth-first walk would have reached only after all of it.
	deep := filepath.Join(home, ".noise")
	for i := 0; i < maxIndexDepth; i++ {
		deep = filepath.Join(deep, "down")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := sourcesModel(t, home, 100, 40)
	search(t, m, "down")

	entries := m.browser.filter.entries
	for i := 1; i < len(entries); i++ {
		if entries[i].depth < entries[i-1].depth {
			t.Fatalf("the index goes back up a level at %d: %q (depth %d) after %q (depth %d)",
				i, entries[i].rel, entries[i].depth, entries[i-1].rel, entries[i-1].depth)
		}
	}

	// Concretely: the whole first level is indexed before anything below it,
	// so the shallow name is in hand however early the scan stops.
	at := func(rel string) int {
		for i, e := range entries {
			if e.rel == rel {
				return i
			}
		}
		t.Fatalf("%q was never indexed: %v", rel, entries)
		return -1
	}
	if a, b := at("Downloads"), at(filepath.Join(".noise", "down")); a > b {
		t.Errorf("Downloads was indexed at %d, after a second-level path at %d", a, b)
	}
}

// And the shallow name is what the query answers with, above the deep ones
// that merely carry the same letters.
func TestAShallowNameOutranksADeepOneThatSortsEarlier(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".noise", "extensions", "markdown"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".noise", "download_get_pip.py"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := sourcesModel(t, home, 100, 40)
	search(t, m, "Down")

	rows := m.browser.list()
	if len(rows) == 0 {
		t.Fatal("the search found nothing")
	}
	if want := filepath.Join(home, "Downloads"); rows[0].path != want {
		t.Errorf("the first match is %q, want %q\nall: %v", rows[0].path, want, rowNames(m))
	}
}

// ---------- the archive browser's search ----------

// archiveModel puts the model on the screen both reading flows open on, with
// the file list holding the keyboard.
func archiveModel(t *testing.T, root string) *model {
	t.Helper()
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.intent = intentRestore
	m.state = statePickArchive
	m.back = []state{stateMenu}
	m.browser = newBrowser(root)
	return m
}

// archiveTree is where archives really end up: in a directory of their own, a
// couple of levels from wherever the interface happened to open.
func archiveTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backups", "2026"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"laptop-2026-01-01.arca", "notes.txt"} {
		path := filepath.Join(root, "backups", "2026", name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The whole point of the key on this screen: the archive is three directories
// down, and naming it is quicker than walking to it.
func TestTheArchiveBrowserFindsAnArchiveSeveralDirectoriesDown(t *testing.T) {
	root := archiveTree(t)
	m := archiveModel(t, root)
	search(t, m, "laptop")

	if names := rowNames(m); !contains(names, "backups/2026/laptop-2026-01-01.arca") {
		t.Fatalf("the search listed %v", names)
	}

	m.Update(key("enter"))
	if m.state != stateArchiveKey {
		t.Fatalf("state = %v, want the key screen", m.state)
	}
	want := filepath.Join(root, "backups", "2026", "laptop-2026-01-01.arca")
	if m.archivePath != want {
		t.Errorf("archivePath = %q, want %q", m.archivePath, want)
	}
	if m.browser.filter != nil {
		t.Error("choosing an archive left the scan behind the search running")
	}
}

// Enter answers the row it is on. A directory is somewhere to look inside, and
// looking inside ends the search, whose index is of the tree just left.
func TestTheArchiveSearchOpensADirectoryAndEndsThere(t *testing.T) {
	root := archiveTree(t)
	m := archiveModel(t, root)
	search(t, m, "2026")

	e, ok := m.browser.current()
	if !ok || !e.isDir {
		t.Fatalf("the exact directory name did not rank first: %v", rowNames(m))
	}
	m.Update(key("enter"))

	if m.state != statePickArchive {
		t.Fatalf("state = %v, want the archive browser", m.state)
	}
	if m.browser.filter != nil {
		t.Error("walking into a match left the search open")
	}
	if want := filepath.Join(root, "backups", "2026"); m.browser.cwd != want {
		t.Errorf("cwd = %q, want %q", m.browser.cwd, want)
	}
}

// While the field has the keyboard every printable key is a character of the
// query, including the two the interface answers to everywhere else.
func TestTheArchiveSearchTakesQAndQuestionMarkAsCharacters(t *testing.T) {
	m := archiveModel(t, archiveTree(t))
	m.Update(key("/"))
	if !m.typing() {
		t.Fatal("the open search does not claim the keyboard")
	}

	typed(m, "q?")
	if got := m.browser.filter.input.Value(); got != "q?" {
		t.Errorf("the query reads %q; the keys were taken as commands", got)
	}
	if m.showKeys {
		t.Error("typing a question mark opened the keys overlay")
	}
}

// Esc undoes the smallest thing it can, which is the search before the screen.
func TestEscTakesTheArchiveSearchBackBeforeTheScreen(t *testing.T) {
	m := archiveModel(t, archiveTree(t))
	m.Update(key("/"))

	m.Update(key("esc"))
	if m.browser.filter != nil {
		t.Fatal("esc did not close the search")
	}
	if m.state != statePickArchive {
		t.Fatalf("esc left the screen as well, state = %v", m.state)
	}

	m.Update(key("esc"))
	if m.state != stateMenu {
		t.Errorf("a second esc did not leave the screen, state = %v", m.state)
	}
}

// Choosing with no search open must not disturb the list, so esc from the key
// screen comes back to the file that was named rather than to the top.
func TestChoosingFromTheListingKeepsTheCursor(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.arca", "b.arca", "c.arca"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := archiveModel(t, root)

	m.Update(key("down"))
	m.Update(key("enter"))
	if m.state != stateArchiveKey {
		t.Fatalf("state = %v, want the key screen", m.state)
	}

	m.Update(key("esc"))
	e, ok := m.browser.current()
	if !ok || filepath.Base(e.path) != "b.arca" {
		t.Errorf("the cursor came back on %q, want b.arca", e.path)
	}
}

// The archive browser is the whole width of the screen, not the source
// screen's left column, and the field has to be sized to the one it is in.
func TestTheArchiveSearchFieldIsSizedToTheWholeScreen(t *testing.T) {
	m := archiveModel(t, archiveTree(t))
	m.Update(key("/"))

	sources, _ := m.sourcesLayout()
	if got := m.browser.filter.input.Width; got <= sources-2-len(searchPrompt) {
		t.Errorf("the field is %d wide, no wider than the source screen's column", got)
	}
}
