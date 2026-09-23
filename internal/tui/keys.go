package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The interface's whole keyboard vocabulary, declared once.
//
// A binding carries the keys it answers to and the words the screen uses for
// them, together. Before this the two lived apart: a handler matched "d" and a
// footer somewhere else called it "directory", and nothing made them agree —
// which is how "d" came to open a directory on one screen and delete a row on
// the next, and how "q" came to quit on seven screens while five of them said
// nothing about it.
//
// A screen names bindings rather than key strings, so a key cannot be handled
// in one place and labelled differently in another, and cannot be answered
// without being documented.
type binding struct {
	// keys is what tea.KeyMsg.String() may read for this binding.
	keys []string

	// shown is how the footer prints the keys, and verb is what they do.
	// Split, because the footer wants them joined and the overlay wants them
	// in columns.
	shown string
	verb  string

	// hint is the overlay's fuller form of shown, naming the aliases the
	// footer has no room for. Empty means shown says it all.
	hint string
}

func (b binding) matches(msg tea.KeyMsg) bool {
	s := msg.String()
	for _, k := range b.keys {
		if k == s {
			return true
		}
	}
	return false
}

// label is the footer's form: the keys and what they do, as one phrase.
func (b binding) label() string { return b.shown + " " + b.verb }

// describe is the overlay's form of the keys alone, aliases included.
func (b binding) describe() string {
	if b.hint != "" {
		return b.hint
	}
	return b.shown
}

// set reports whether a screen filled this slot in.
func (b binding) set() bool { return b.verb != "" }

// Movement. Every list in the interface answers to all of these; the footer
// shows the arrows and the overlay names the rest.
var (
	kMove = binding{keys: []string{"up", "down", "k", "j"},
		shown: "↑↓", verb: "move", hint: "↑↓  j k"}
	kPage = binding{keys: []string{"pgup", "pgdown"},
		shown: "pgup/pgdn", verb: "page"}
	kEdge = binding{keys: []string{"g", "G", "home", "end"},
		shown: "g/G", verb: "first/last", hint: "g G  home end"}
	kOpen = binding{keys: []string{"right"}, shown: "→", verb: "open"}
	kUp   = binding{keys: []string{"left"}, shown: "←", verb: "up"}
	// h stays home rather than becoming vim's "left": browser.go explains the
	// choice, and the left arrow is the only key that goes up.
	kHome   = binding{keys: []string{"~", "h"}, shown: "~", verb: "home", hint: "~  h"}
	kHidden = binding{keys: []string{"."}, shown: ".", verb: "hidden"}
)

// Verbs. Each of these means one thing everywhere it is offered.
var (
	kSelect = binding{keys: []string{" "}, shown: "space", verb: "select"}

	// p opens the source screen's directory field. It was d, which is what put
	// "open a directory" one screen away from "delete this row".
	kPath = binding{keys: []string{"p"}, shown: "p", verb: "path"}

	kFind     = binding{keys: []string{"/"}, shown: "/", verb: "find"}
	kExclude  = binding{keys: []string{"x"}, shown: "x", verb: "exclude"}
	kExcludes = binding{keys: []string{"X"}, shown: "X", verb: "excludes"}
	kAdd      = binding{keys: []string{"a"}, shown: "a", verb: "add"}

	// d removes, and only removes, on both screens that have anything to
	// remove.
	kRemove = binding{keys: []string{"d"}, shown: "d", verb: "remove"}

	kEditDest = binding{keys: []string{"e"}, shown: "e", verb: "destination"}
	kEditCfg  = binding{keys: []string{"e"}, shown: "e", verb: "edit config"}
	kNewCfg   = binding{keys: []string{"n"}, shown: "n", verb: "new config"}
	kSave     = binding{keys: []string{"s"}, shown: "s", verb: "save"}

	kComplete = binding{keys: []string{"tab"}, shown: "tab", verb: "complete"}
	kPick     = binding{keys: []string{"up", "down"}, shown: "↑↓", verb: "pick"}
	kClear    = binding{keys: []string{"ctrl+u"}, shown: "ctrl+u", verb: "clear"}
	kSkip     = binding{keys: []string{"ctrl+d"}, shown: "ctrl+d", verb: "skip"}
	kGo       = binding{keys: []string{"ctrl+d"}, shown: "ctrl+d", verb: "continue"}
)

// The keys every screen offers. They are appended by the footer rather than
// declared by the screens, because a screen that has to remember them is a
// screen that can forget them.
var (
	kBack  = binding{keys: []string{"esc"}, shown: "esc", verb: "back"}
	kQuit  = binding{keys: []string{"q"}, shown: "q", verb: "quit"}
	kHelp  = binding{keys: []string{"?"}, shown: "?", verb: "keys"}
	kAbort = binding{keys: []string{"ctrl+c"}, shown: "ctrl+c", verb: "abort"}
)

// confirm is the enter key wearing this screen's verb. Enter is the one key
// that legitimately differs from screen to screen — it is always the primary
// action, and the primary action is not the same thing twice — so it is built
// rather than declared. The verb is kept to two words: the footer has to fit
// on one line, and a key legend is not the place to explain a screen.
func confirm(verb string) binding {
	return binding{keys: []string{"enter"}, shown: "enter", verb: verb}
}

// leave is esc wearing a verb other than "back", for the screens where going
// back is not what it does.
func leave(verb string) binding {
	return binding{keys: []string{"esc"}, shown: "esc", verb: verb}
}
