package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hamsa/arca/internal/cli"
)

// The source screen's finder.
//
// Walking down to ~/.config/nvim one directory listing at a time is the thing
// the browser is worst at, and also the thing people do most. "/" answers it,
// and it answers in two different ways, because "find me a path" is two
// different questions:
//
//   - A query spelled as a path — "/", "~/", "./" — is completed as one: the
//     directory it names is listed, and its last segment narrows that listing.
//     Typing "/" shows what is in the root and "/home/" what is in /home.
//     Nothing is guessed at, so nothing unasked-for can turn up.
//
//   - Anything else is a name, and is searched for in the subtree below where
//     the browser is standing. That search is ranked name first: typing
//     "Downloads" puts the directory actually called Downloads at the top, above
//     everything that merely carries those letters somewhere in its path.
//
// The split is the whole design. One mechanism cannot do both: completing a bare
// name finds nothing at all, and matching a path that was spelled out in full
// against anything looser than itself ranks directories nobody asked about above
// the one that was named.
//
// Matching is exact throughout — a case-insensitive substring, nothing more.
// "Down" finds Downloads because the letters are really there, in that order and
// next to each other. Subsequence matching was tried and taken back out: over a
// home directory it answers almost every query with almost everything, and the
// few typos it forgives are not worth a list you cannot trust.
const (
	// maxIndexEntries caps one search. A home directory with a few checkouts in
	// it goes past this, so the cap is reported rather than applied quietly: a
	// search that stopped looking without saying so is worse than one that
	// admits it.
	//
	// What the cap cuts is decided by the scan order, not by this number, which
	// is why scanTree walks in breadth. Cut from the bottom the missing paths
	// are the deep ones nobody names; cut from the side they are whatever
	// happened to sort late, and ~/Downloads goes missing because ~/.cache is
	// alphabetically earlier.
	maxIndexEntries = 100_000

	// maxIndexDepth is how far below the starting directory the scan goes.
	//
	// This is the main thing keeping the results readable. Almost everything
	// worth backing up sits within a few levels of home — ~/.config/nvim/lua is
	// four — while the tens of thousands of paths that would bury it live
	// deeper, in caches and package directories nobody names on purpose.
	// Anything past the cap is still reachable: walk into it and search from
	// there, or spell it out as a path.
	maxIndexDepth = 6

	// indexBatch is how many paths a scan hands over at once: few enough that
	// the first results appear at once, many enough that a deep tree does not
	// flood the interface with messages.
	indexBatch = 2048

	// maxResults is how many matches are turned into rows. The list is there to
	// be scrolled a little and narrowed a lot, so materialising more than this
	// would be work nobody ever looks at.
	maxResults = 200

	// searchPrompt labels the query field. It has to be a word no path starts
	// with, so that what is typed is never mistaken for what was already there.
	searchPrompt = "find "
)

// How a candidate is ranked against a typed name. The tiers are what make the
// order explainable: the thing actually called Downloads comes first, then the
// things whose name begins that way, and only then the ones that happen to
// carry the letters somewhere along their path.
//
// Depth and hiddenness are subtractions rather than tiers, so they settle ties
// without ever letting a deep near-miss outrank an exact name.
const (
	scoreNameExact     = 1000
	scoreNamePrefix    = 800
	scoreNameSubstring = 600
	scorePathSubstring = 200

	// depthPenalty is small against the gap between two tiers: ~/Downloads must
	// beat ~/x/y/Downloads, but no amount of depth may push an exact name below
	// something that merely contains the letters.
	depthPenalty = 12

	// hiddenPenalty pushes dot directories down without hiding them. A cache
	// under ~/.local is not what "Downloads" meant, while ~/.ssh is exactly what
	// "ssh" meant, and one penalty short of a whole tier gets both right.
	hiddenPenalty = 60
)

// indexEntry is one path the scan found, spelled relative to the directory the
// search started in. depth and hidden are worked out once here rather than on
// every keystroke.
type indexEntry struct {
	rel    string
	isDir  bool
	depth  int
	hidden bool
}

// indexMsg is one batch of scanned paths on its way back to the interface.
type indexMsg struct {
	gen       int
	entries   []indexEntry
	done      bool
	truncated bool
}

// filter is the state of one search: the query, the index it may have built,
// and the rows the two produce together.
type filter struct {
	// gen identifies this search. A scan that outlives the filter that started
	// it — the user pressed esc, or walked into another directory — keeps
	// sending batches, and gen is what makes them ignorable.
	gen  int
	root string

	input textinput.Model

	entries []indexEntry
	results []browserEntry
	total   int

	// listing is the directory whose contents are on screen, and is set only
	// while the query is a path. An empty listing means the rows came from the
	// index instead, and are spelled relative to root.
	//
	// leaf is how much of the next name has been typed. Together the two are
	// exactly what every row on screen begins with, which is what the highlight
	// needs: a completed path is answered at its head, not somewhere in its
	// middle.
	listing string
	leaf    string

	// started says the index has been asked for. It is built lazily: a query
	// that is a path never needs one, and walking a home directory for somebody
	// who typed "/etc/" would be work spent on nothing.
	started   bool
	scanning  bool
	truncated bool

	// ch carries the scan's batches; done is closed when the filter is
	// discarded, which is what stops the scan goroutine rather than letting it
	// walk a home directory to the end for nobody.
	ch   chan indexMsg
	done chan struct{}
}

// openFilter starts a search from dir. No scan runs yet; the first query that
// needs one asks for it.
func openFilter(gen int, dir string) (*filter, tea.Cmd) {
	ti := textinput.New()
	ti.Placeholder = "a name, or a path starting with / or ~/"
	ti.CharLimit = 4096
	// The prompt is a word rather than a symbol. "/ " read as the beginning of
	// a path — as though the root were already being listed — and typing the
	// "/" that really does list it then put two of them on the line.
	ti.Prompt = searchPrompt
	ti.PromptStyle = stEmph

	f := &filter{
		gen:   gen,
		root:  dir,
		input: ti,
		done:  make(chan struct{}),
	}
	return f, f.input.Focus()
}

// ensureScan builds the index, once.
func (f *filter) ensureScan() tea.Cmd {
	if f.started {
		return nil
	}
	f.started, f.scanning = true, true
	f.ch = make(chan indexMsg, 4)
	go scanTree(f.gen, f.root, f.ch, f.done)
	return waitForIndex(f.gen, f.ch)
}

// close stops the scan behind this filter. It is safe to call twice, which
// matters because a filter is closed both by leaving it and by replacing it.
func (f *filter) close() {
	if f == nil {
		return
	}
	select {
	case <-f.done:
	default:
		close(f.done)
	}
}

// waitForIndex turns the scan's channel into a stream of messages, re-arming
// itself after each batch the way waitFor does for a running backup. The two use
// separate channels on purpose: indexing a tree must never compete with the
// progress of an archive being written.
func waitForIndex(gen int, ch chan indexMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return indexMsg{gen: gen, done: true}
		}
		return msg
	}
}

// scanTree lists everything below root, breadth first: one whole level before
// any of the next, and within a level in the order ReadDir gives — which is
// sorted, so equally ranked matches come out in a stable order.
//
// Breadth is what makes the entry cap survivable. Walking in depth spends the
// budget on whichever subtree happens to sort first: under a real home
// directory ~/.antigravity and ~/.cache alone use all 100k, the scan never
// reaches ~/Downloads, and the finder answers "Down" with three files that
// merely carry the letters. A name that was never indexed cannot be found, and
// nothing about the ranking can rescue it.
//
// Walking in breadth spends the budget from the top down, so the cap falls on
// the deepest level rather than on every level but one — and it falls on
// exactly what the ranking already puts last.
func scanTree(gen int, root string, ch chan indexMsg, done chan struct{}) {
	defer close(ch)

	batch := make([]indexEntry, 0, indexBatch)
	total := 0
	truncated := false

	// send hands over what has accumulated. It reports false once nobody is
	// listening any more, which is the signal to stop walking.
	send := func(final bool) bool {
		select {
		case ch <- indexMsg{gen: gen, entries: batch, done: final, truncated: truncated}:
			batch = make([]indexEntry, 0, indexBatch)
			return true
		case <-done:
			return false
		}
	}

	// level is a directory still to be listed, and what its contents will be
	// called. Holding the queue rather than recursing is the whole difference
	// between this and a depth-first walk.
	type level struct {
		dir    string
		prefix string
		depth  int
		hidden bool
	}

	walk := func() {
		queue := []level{{dir: root, depth: 1}}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			select {
			case <-done:
				return
			default:
			}
			items, err := os.ReadDir(cur.dir)
			if err != nil {
				// A directory that cannot be listed is not worth abandoning the
				// rest of the tree for; it simply contributes nothing.
				continue
			}
			for _, it := range items {
				if total >= maxIndexEntries {
					truncated = true
					return
				}
				name := it.Name()
				// A symlink is recorded but never queued: that is what keeps the
				// walk finite without a loop guard.
				isDir := it.IsDir()
				rel := filepath.Join(cur.prefix, name)
				hidden := cur.hidden || strings.HasPrefix(name, ".")

				batch = append(batch, indexEntry{
					rel: rel, isDir: isDir, depth: cur.depth, hidden: hidden,
				})
				total++

				if len(batch) >= indexBatch && !send(false) {
					return
				}
				if isDir && cur.depth < maxIndexDepth {
					queue = append(queue, level{
						dir:    filepath.Join(cur.dir, name),
						prefix: rel,
						depth:  cur.depth + 1,
						hidden: hidden,
					})
				}
			}
		}
	}

	walk()
	send(true)
}

// absorb takes one batch into the index. It reports whether the batch belongs to
// this filter at all; a stale one changes nothing.
func (f *filter) absorb(msg indexMsg) bool {
	if f == nil || msg.gen != f.gen {
		return false
	}
	f.entries = append(f.entries, msg.entries...)
	if msg.truncated {
		f.truncated = true
	}
	if msg.done || msg.truncated {
		f.scanning = false
	}
	return true
}

// ---------- matching ----------

// match rebuilds the rows. listing is what the browser would show with no search
// open, which is what an empty query falls back to. The command it returns is the
// index starting up, on the first query that needs one.
func (f *filter) match(hidden bool, listing []browserEntry) tea.Cmd {
	query := strings.TrimSpace(f.input.Value())
	f.results = f.results[:0]
	f.listing, f.leaf = "", ""

	switch {
	case query == "":
		// Nothing typed yet: stay on the directory the search was opened in,
		// rather than opening with a dump of everything below it.
		f.listing = f.root
		f.total = len(listing)
		f.results = append(f.results, listing...)
		return nil

	case isPathQuery(query):
		f.listing, f.leaf, f.results = completePath(query, hidden)
		f.total = len(f.results)
		return nil
	}

	cmd := f.ensureScan()
	f.rank(query, hidden)
	return cmd
}

// isPathQuery says the query is spelled as a path, and so is to be completed
// rather than searched for.
func isPathQuery(q string) bool {
	return strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") ||
		strings.HasPrefix(q, "./") || strings.HasPrefix(q, "../")
}

// completePath lists the directory a path-shaped query names, narrowed by its
// last segment.
//
// This is pathCandidates' idea — the one the destination screens complete with —
// with the one difference this screen needs: files are offered too, because a
// single file is a perfectly ordinary thing to back up.
func completePath(query string, hidden bool) (listing, leaf string, out []browserEntry) {
	expanded := expandHome(query)

	dir := expanded
	if !strings.HasSuffix(expanded, string(filepath.Separator)) {
		dir, leaf = filepath.Dir(expanded), filepath.Base(expanded)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		// A path that names nothing yet is the normal state of a field being
		// typed into, not an error worth reporting.
		return dir, leaf, nil
	}

	lower := strings.ToLower(leaf)
	for _, it := range items {
		name := it.Name()
		// Hidden entries follow the rule the rest of the screen uses — the "."
		// toggle — with the shell's exception: asking for one by name brings it
		// back whatever the toggle says.
		if strings.HasPrefix(name, ".") && !hidden && !strings.HasPrefix(leaf, ".") {
			continue
		}
		// Case-insensitive, so completing fixes the case rather than preserving
		// a typo.
		if !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}
		out = append(out, browserEntry{
			name:  name,
			path:  filepath.Join(dir, name),
			isDir: it.IsDir(),
		})
	}

	// Directories first, then names: the order the browser lists in, and the one
	// people expect in a file list.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].isDir != out[j].isDir {
			return out[i].isDir
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return dir, leaf, out
}

// names says the query already spells out this exact path, so completing it
// would fill in nothing. It resolves the query the way the completion does —
// "~/" and a relative path name the same directory a spelled-out one does — so
// that how the path was typed cannot change what the key means.
//
// A query ending in a separator never names the row under the cursor: that row
// is one of the children it is listing.
func (f *filter) names(path string) bool {
	q := expandHome(strings.TrimSpace(f.input.Value()))
	if q == "" {
		return false
	}
	if abs, err := filepath.Abs(q); err == nil {
		q = abs
	}
	return q == path
}

// scored is one indexed path and how well it answers the query.
type scored struct {
	entry int
	score int
}

// rank scores the index against a typed name and keeps the best of it.
func (f *filter) rank(query string, hidden bool) {
	lower := strings.ToLower(strings.TrimSpace(query))

	found := make([]scored, 0, 64)
	for i := range f.entries {
		e := &f.entries[i]
		if !hidden && e.hidden {
			continue
		}
		if score, ok := scoreEntry(e, lower); ok {
			found = append(found, scored{entry: i, score: score})
		}
	}
	f.total = len(found)

	sort.SliceStable(found, func(i, j int) bool {
		a, b := &f.entries[found[i].entry], &f.entries[found[j].entry]
		switch {
		case found[i].score != found[j].score:
			return found[i].score > found[j].score
		case a.depth != b.depth:
			return a.depth < b.depth
		default:
			return a.rel < b.rel
		}
	})

	for i, s := range found {
		if i == maxResults {
			break
		}
		e := f.entries[s.entry]
		f.results = append(f.results, browserEntry{
			name:  filepath.ToSlash(e.rel),
			path:  filepath.Join(f.root, e.rel),
			isDir: e.isDir,
		})
	}
}

// scoreEntry places one path in a tier and then settles it by depth. query is
// already trimmed and lowercased.
func scoreEntry(e *indexEntry, query string) (int, bool) {
	name := strings.ToLower(filepath.Base(e.rel))
	path := strings.ToLower(filepath.ToSlash(e.rel))

	score := nameScore(name, query)
	// A leading dot marks a thing as hidden; it is not part of what the thing is
	// called. Someone typing "ssh" means ~/.ssh, and without this that exact
	// name scores as a mere substring — losing to any deeper directory that
	// merely starts with the letters.
	if bare := strings.TrimPrefix(name, "."); bare != name {
		if s := nameScore(bare, query); s > score {
			score = s
		}
	}
	if score == 0 {
		if !strings.Contains(path, query) {
			return 0, false
		}
		score = scorePathSubstring
	}

	score -= e.depth * depthPenalty
	if e.hidden {
		score -= hiddenPenalty
	}
	return score, true
}

// nameScore ranks the query against a single name, or returns zero when it does
// not appear in it at all.
func nameScore(name, query string) int {
	switch {
	case name == query:
		return scoreNameExact
	case strings.HasPrefix(name, query):
		return scoreNamePrefix
	case strings.Contains(name, query):
		return scoreNameSubstring
	}
	return 0
}

// ---------- what the screen says about the search ----------

// status is the line the search puts where the browser normally prints its
// working directory: which directory is being listed, or what the query found
// and whether the index behind it has finished.
func (f *filter) status(width int) string {
	if f.listing != "" {
		return shorten(contractHome(f.listing), maxInt(8, width-16)) +
			" · " + cli.Count(f.total, "entry", "entries")
	}

	found := cli.Count(f.total, "match", "matches")
	switch {
	case f.truncated:
		return found + " · index stopped at " + compactCount(maxIndexEntries)
	case f.scanning:
		return found + " · indexing " + compactCount(len(f.entries)) + "…"
	case f.total > len(f.results):
		return found + " · showing the best " + compactCount(len(f.results))
	}
	return found
}

// compactCount keeps a running total short enough to sit in a status line that
// redraws on every keystroke.
func compactCount(n int) string {
	if n < 1000 {
		return itoa(n)
	}
	return itoa(n/1000) + "k"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

// mark says which positions of a row, as it will actually be drawn, the query
// accounts for.
//
// Which end of the row that is depends on which question was asked. A path is
// answered at its head — every row shares the directory that was typed, and the
// answer is what follows it — while a name is answered wherever the name turns
// up. Marking a completed path anywhere but its beginning was the old
// behaviour, and it pointed at the wrong thing: typing "/" lit up the separator
// at the end of "/home/" rather than the one that had just been asked for.
func (f *filter) mark(text string) []int {
	query := strings.TrimSpace(f.input.Value())
	if query == "" {
		return nil
	}
	if f.listing == "" {
		return markQuery(text, query)
	}
	return markPrefix(text, f.pathPrefix())
}

// pathPrefix is the head every row of a completed path has in common with what
// was typed: the directory being listed, and however much of the next name has
// been spelled out.
//
// It is built from what the completion resolved rather than from the query, so
// a "~/" or a "./" marks the same letters a spelled-out path would.
func (f *filter) pathPrefix() string {
	dir := f.listing
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return dir + f.leaf
}

// markPrefix marks the head of a row, and only a whole one. A row whose head
// the width had to drop keeps its leading "…" unlit rather than lighting up
// letters that are no longer the ones that matched.
func markPrefix(text, prefix string) []int {
	want, have := fold(prefix), fold(text)
	if len(want) == 0 || len(want) > len(have) {
		return nil
	}
	for i, r := range want {
		if have[i] != r {
			return nil
		}
	}
	hits := make([]int, len(want))
	for i := range hits {
		hits[i] = i
	}
	return hits
}

// fitPath shortens a whole path to the width it has, dropping whole directories
// off its front and saying so.
//
// The finder draws paths in full, because its rows come from all over the tree
// and a bare name does not say which part of it. When one is too long for the
// column, what has to survive is the end: everything on screen sits under the
// one directory being searched, so the front of a path is the part every row
// has in common and the end is the part that tells them apart. Keeping the
// front instead was tried, and turned three different matches into three
// identical lines.
//
// The cut falls on a separator, so what is left is a real path as far as it
// goes rather than a name with its head bitten off.
func fitPath(p string, max int) string {
	if max <= 1 {
		return ""
	}
	r := []rune(p)
	if len(r) <= max {
		return p
	}

	sep := string(filepath.Separator)
	segs := strings.Split(p, sep)
	best := ""
	for i := len(segs) - 1; i >= 0; i-- {
		grown := "…" + sep + strings.Join(segs[i:], sep)
		if len([]rune(grown)) > max {
			break
		}
		best = grown
	}
	if best == "" {
		// Not even the last name fits on its own; keep the end of it.
		return "…" + string(r[len(r)-max+1:])
	}
	return best
}

// markQuery finds the query inside the text as it will actually be drawn, so the
// highlight cannot disagree with the row. Matching against the untruncated path
// instead would put the marks in the wrong places the moment a long path loses
// its head to shorten.
//
// The name at the end is tried first. It is what the query usually named, and
// lighting up letters in the directories above it would explain the wrong thing.
func markQuery(text, query string) []int {
	want := fold(strings.TrimSpace(query))
	if len(want) == 0 {
		return nil
	}
	have := fold(text)

	// Everything after the last separator is the name.
	from := 0
	for i, r := range have {
		if r == '/' {
			from = i + 1
		}
	}

	start := indexRunes(have[from:], want)
	if start >= 0 {
		start += from
	} else if start = indexRunes(have, want); start < 0 {
		return nil
	}

	hits := make([]int, len(want))
	for i := range hits {
		hits[i] = start + i
	}
	return hits
}

// fold lowercases rune by rune rather than through strings.ToLower, which may
// hand back a different number of runes than it was given. These positions have
// to line up with the text being drawn.
func fold(s string) []rune {
	out := []rune(s)
	for i, r := range out {
		out[i] = unicode.ToLower(r)
	}
	return out
}

// indexRunes is strings.Index over runes: where want starts inside have, or -1.
func indexRunes(have, want []rune) int {
	if len(want) == 0 || len(want) > len(have) {
		return -1
	}
	for i := 0; i+len(want) <= len(have); i++ {
		match := true
		for j, r := range want {
			if have[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// highlight paints the marked positions of an already-styled row.
func highlight(text string, hits []int, base lipgloss.Style) string {
	if len(hits) == 0 {
		return base.Render(text)
	}
	mark := make(map[int]bool, len(hits))
	for _, h := range hits {
		mark[h] = true
	}

	var b strings.Builder
	var run []rune
	flush := func(hit bool) {
		if len(run) == 0 {
			return
		}
		if hit {
			b.WriteString(stHit.Render(string(run)))
		} else {
			b.WriteString(base.Render(string(run)))
		}
		run = run[:0]
	}

	inHit := false
	for i, r := range []rune(text) {
		if mark[i] != inHit {
			flush(inHit)
			inHit = mark[i]
		}
		run = append(run, r)
	}
	flush(inHit)
	return b.String()
}
