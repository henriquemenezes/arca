package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquemenezes/arca/internal/cli"
)

// The source screen's path bar: the line naming the directory on screen, made
// editable.
//
// The line was always there, and a path you can read but not correct is an odd
// thing to be shown. Reaching /etc/nginx meant walking out of home one arrow at
// a time, past every directory in between, when the user already knew the
// answer and could have typed it in less time than the first step took.
//
// It is a pathInput — the same field the destination screens complete paths
// with — with one key moved. Tab belongs to the focus here: it is what hands
// the keyboard to the file list. So completing is the right arrow at the end of
// the line, which is where a shell's suggestion is accepted and where there is
// no cursor left to move anyway.
//
// The list below follows the field. A directory that has been spelled out in
// full is one the user has arrived at, not one they are still describing, so it
// is opened as soon as it is named — which makes completing a path and walking
// into it the same gesture rather than two.

// attachPath gives the browser its editable directory line. The screens that
// only pick one file — the archive browser — leave it nil and keep the plain
// label.
func (b *browser) attachPath() {
	if b.path == nil {
		p := newPathInput("a directory, or ~/", 48)
		b.path = &p
	}
	b.syncPath()
}

// syncPath puts the current directory back in the field, and with it the field
// back to having nothing of its own to answer.
//
// It runs when the field takes the keyboard rather than on every move, because
// those are the only moments the two can disagree: blurred, the line is drawn
// from cwd and the value behind it is never read.
func (b *browser) syncPath() {
	if b.path == nil {
		return
	}
	b.path.SetValue(b.cwd)
	b.path.CursorEnd()
	b.pathTyped = false
	b.pathFrom = b.cwd
}

// revertPath is what esc undoes: the line goes back to naming the directory it
// was filled from, and the list goes back to that directory too.
//
// Both halves matter. The list follows the field as it is typed, so by the time
// esc arrives the browser may be several directories from where the detour
// started — taking back the text alone would leave the screen somewhere nobody
// chose to be, and no key would say where that was. Keeping where you got to is
// what enter is for.
func (b *browser) revertPath() {
	if b.path == nil {
		return
	}
	if b.pathFrom != "" && b.pathFrom != b.cwd {
		b.load(b.pathFrom)
	}
	b.syncPath()
}

// pathEdited says something has been typed into the field and not yet dealt
// with — which is what decides whether enter and esc belong to the field or to
// the screen.
//
// It is a record of the typing rather than a comparison against cwd, because
// the list follows the field: a directory spelled out in full is opened as it
// is typed, and by then the two agree again while the keystroke has plainly
// not been answered.
func (b *browser) pathEdited() bool { return b.pathTyped }

// followPath opens whatever directory the field now names, and does nothing at
// all while it names something that is not one — which is every keystroke of a
// name half typed.
//
// changed says the key that led here altered the line, which is how the field
// knows it is holding something the user has not yet pressed enter on.
func (b *browser) followPath(changed bool) {
	if b.path == nil {
		return
	}
	if changed {
		b.pathTyped = true
	}
	value := strings.TrimSpace(b.path.Value())
	if value == "" {
		return
	}
	abs, err := filepath.Abs(expandHome(value))
	if err != nil || abs == b.cwd {
		return
	}
	if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		return
	}
	b.load(abs)
}

// openPath is what enter means in the field: go to what has been typed.
//
// A file is not a directory to stand in, but naming one is a perfectly ordinary
// way to reach it — it may be the whole reason the path was typed — so its
// directory is opened with the cursor already on it. The bool says that
// happened, which is what moves the keyboard to the list so space can mark it.
func (b *browser) openPath() (onFile bool, err error) {
	if b.path == nil {
		return false, nil
	}
	value := strings.TrimSpace(b.path.Value())
	if value == "" {
		b.syncPath()
		return false, nil
	}

	abs, err := filepath.Abs(expandHome(value))
	if err != nil {
		return false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		// The field is the one place on this screen a path can be wrong, so it
		// says so in the words of what was typed rather than the wrapped
		// syscall error.
		return false, fmt.Errorf("%s: no such file or directory", contractHome(abs))
	}

	if info.IsDir() {
		b.load(abs)
		b.syncPath()
		return false, nil
	}
	b.load(filepath.Dir(abs))
	b.focus(abs)
	b.syncPath()
	return true, nil
}

// pathStatus is the line under the field while it has the keys: which directory
// the rows below came from, and how many of them there are.
//
// It is not the field repeated. The field holds what is being typed, which
// during a half-finished name is not a directory at all; this says what is
// actually on screen underneath it.
func (b *browser) pathStatus(width int) string {
	return shorten(contractHome(b.cwd), maxInt(8, width-16)) +
		" · " + cli.Count(len(b.entries), "entry", "entries")
}
