package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// pathListRows is the most candidates a screen will draw at once, before the
// terminal's own height cuts it down further.
const pathListRows = 8

// pathInput is a text field that completes filesystem paths the way a shell
// does: tab fills in as much as every candidate agrees on, and the ones it
// could not choose between are listed underneath for the arrows to pick from.
//
// bubbles/textinput ships a completion mechanism of its own and this type
// deliberately uses only half of it. Its matcher gives up on an empty field —
// updateSuggestions returns early when the value is empty — which is the exact
// moment a path field is least useful, so the candidates and the selection
// live here instead. What is kept is the inline ghost text, fed one suggestion
// at a time so the index inside textinput is always 0 and cannot drift away
// from the one below.
type pathInput struct {
	input textinput.Model

	// candidates are absolute (or, for a path typed relative, relative) and
	// always end in a separator, since only directories are ever offered.
	candidates []string

	// listing is the directory the candidates came from, or "" when they are
	// the shortcuts offered to an empty field and share no parent.
	listing string

	index int

	// browsing records that the arrows moved the selection. While it is set
	// the list is left alone, so cycling does not walk into the directory it
	// just put in the field.
	browsing bool
}

func newPathInput(placeholder string, width int) pathInput {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 4096
	ti.Width = width
	ti.ShowSuggestions = true

	p := pathInput{input: ti}
	p.refresh()
	return p
}

func (p *pathInput) Value() string  { return p.input.Value() }
func (p *pathInput) Focus() tea.Cmd { return p.input.Focus() }
func (p *pathInput) Blur()          { p.input.Blur() }
func (p *pathInput) CursorEnd()     { p.input.CursorEnd() }
func (p *pathInput) View() string   { return p.input.View() }

func (p *pathInput) SetValue(s string) {
	p.input.SetValue(s)
	p.refresh()
}

func (p *pathInput) Update(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		p.complete()
		return nil
	case "up", "ctrl+p":
		p.pick(p.index - 1)
		return nil
	case "down", "ctrl+n":
		p.pick(p.index + 1)
		return nil
	}

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.refresh()
	return cmd
}

// complete is the tab key: fill in as much as every candidate agrees on, and
// no more. Guessing past that is what the list is for, and the list is already
// on screen.
func (p *pathInput) complete() {
	if len(p.candidates) == 0 {
		return
	}
	want := commonPrefix(p.candidates)
	if len(p.candidates) == 1 {
		want = p.candidates[0]
	}
	// The shortcuts are unrelated places, so the prefix they agree on is the
	// root directory and completing to it would be worse than useless. With
	// nothing typed there is nothing to extend, so tab takes the selection —
	// the same thing the arrows put in the field.
	if p.listing == "" {
		want = p.candidates[p.index]
	}
	// Nothing left to add. The value may still be shorter than what it
	// resolves to — a leading ~ — and completing is the moment to settle that.
	if want == "" || want == p.input.Value() {
		return
	}
	p.input.SetValue(want)
	p.input.CursorEnd()
	p.refresh()
}

// pick moves the selection without recomputing the list. Recomputing would
// replace the candidates with the children of whatever was just selected, and
// the arrows would have nothing left to move through.
func (p *pathInput) pick(i int) {
	if len(p.candidates) == 0 {
		return
	}
	n := len(p.candidates)
	p.index = ((i % n) + n) % n
	p.browsing = true
	p.input.SetValue(p.candidates[p.index])
	p.input.CursorEnd()
	p.ghost()
}

func (p *pathInput) refresh() {
	p.listing, p.candidates = pathCandidates(p.input.Value())
	p.index = 0
	p.browsing = false
	p.ghost()
}

// ghost hands textinput the one suggestion it is allowed to know about: the
// selected candidate. A single-element list pins its index at 0, which is what
// keeps its ghost text and this type's selection from ever disagreeing.
func (p *pathInput) ghost() {
	if p.index < len(p.candidates) {
		p.input.SetSuggestions([]string{p.candidates[p.index]})
		return
	}
	p.input.SetSuggestions(nil)
}

// ViewList draws the candidates under the field: the directory being listed,
// then the names inside it. An empty field has no single parent to name, so
// its shortcuts are shown whole instead.
func (p *pathInput) ViewList(width, rows int) string {
	if len(p.candidates) == 0 || rows < 1 {
		return ""
	}

	var b strings.Builder
	if p.listing != "" {
		b.WriteString("  " + stMuted.Render(shorten(p.listing, width-4)) + "\n")
	}

	shown := minInt(rows, len(p.candidates))
	// Keep the selection on screen once the list outgrows the room for it.
	start := 0
	if p.index >= shown {
		start = p.index - shown + 1
	}
	for i := start; i < start+shown; i++ {
		label := p.candidates[i]
		if p.listing != "" {
			label = filepath.Base(label) + string(filepath.Separator)
		}
		label = shorten(label, width-6)
		if i == p.index {
			b.WriteString(stSelected.Render("  ▸ "+label) + "\n")
		} else {
			b.WriteString("    " + stDir.Render(label) + "\n")
		}
	}
	if n := len(p.candidates) - shown; n > 0 {
		b.WriteString("    " + stCrumb.Render(fmt.Sprintf("+%d more", n)) + "\n")
	}
	return b.String()
}

// pathCandidates lists the directories that could continue a half-typed path.
//
// Only directories: a destination is a place, and offering the archives
// already sitting there would be offering to overwrite one.
func pathCandidates(value string) (listing string, out []string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", shortcuts()
	}

	expanded := expandHome(value)
	dir, leaf := expanded, ""
	if !strings.HasSuffix(expanded, string(filepath.Separator)) {
		dir, leaf = filepath.Dir(expanded), filepath.Base(expanded)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// A path that names nothing yet is the normal state of a field being
		// typed into, not an error worth reporting.
		return "", nil
	}

	lower := strings.ToLower(leaf)
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && !isDirLink(dir, e) {
			continue
		}
		// Hidden directories stay out of the way until asked for by name,
		// which is the rule every shell uses.
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(leaf, ".") {
			continue
		}
		// Case-insensitive, matching what textinput's own matcher does, so the
		// ghost text never disagrees with the list. Completion still works off
		// the real names, so it fixes the case rather than preserving a typo.
		if !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}
		out = append(out, filepath.Join(dir, name)+string(filepath.Separator))
	}
	sort.Strings(out)

	if dir == "." {
		// "." is where a relative path is anchored; say where that is.
		dir = defaultOutputDir()
	}
	return dir, out
}

// isDirLink reports whether a symlink points at a directory. ReadDir describes
// the link itself, and a home directory whose Downloads is a symlink onto
// another filesystem is an ordinary place to want to write a backup.
func isDirLink(dir string, e os.DirEntry) bool {
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// shortcuts are what an empty field offers. The shell has no answer for an
// empty line either; this one does, because the set of places a backup goes is
// small and known.
func shortcuts() []string {
	seen := map[string]bool{}
	var out []string

	add := func(dir string) {
		if dir == "" {
			return
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
			return
		}
		abs += string(filepath.Separator)
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, abs)
	}

	add(defaultOutputDir())
	if home, err := os.UserHomeDir(); err == nil {
		add(home)
	}
	// Removable media is where a backup usually wants to go, and the mount
	// point is the part nobody remembers. Each is offered only if it exists,
	// so this is not a list of guesses.
	add("/mnt")
	add("/media")
	if user := os.Getenv("USER"); user != "" {
		add(filepath.Join("/run/media", user))
	}
	return out
}

// expandHome resolves a leading ~, which the interface has to do itself: the
// shell that would normally do it never sees these paths. Without it a
// destination of ~/backups writes into a directory literally named "~".
//
// ~other is left alone. Looking up another user's home is a different feature,
// and guessing at it would be worse than leaving the path as it was typed.
func expandHome(p string) string {
	sep := string(filepath.Separator)
	if p != "~" && !strings.HasPrefix(p, "~"+sep) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	out := filepath.Join(home, p[2:])
	// Join cleans the trailing separator away, and that separator is what
	// says "list what is inside this" rather than "complete this name".
	if strings.HasSuffix(p, sep) && !strings.HasSuffix(out, sep) {
		out += sep
	}
	return out
}

// contractHome is expandHome read backwards: it puts the "~/" back.
//
// The browser deals in absolute paths, so a source picked there comes out as
// /home/u/.ssh even when the configuration being edited spelled it ~/.ssh.
// Writing that back would pin the file to one machine and one user name, which
// is precisely what a backup config should not be.
func contractHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !filepath.IsAbs(p) {
		return p
	}
	sep := string(filepath.Separator)
	home = strings.TrimSuffix(home, sep)
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+sep); ok {
		return "~" + sep + rest
	}
	return p
}

// commonPrefix is how far the candidates agree, in whole runes.
func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	prefix := []rune(ss[0])
	for _, s := range ss[1:] {
		r := []rune(s)
		if len(r) < len(prefix) {
			prefix = prefix[:len(r)]
		}
		for i := range prefix {
			if prefix[i] != r[i] {
				prefix = prefix[:i]
				break
			}
		}
	}
	return string(prefix)
}
