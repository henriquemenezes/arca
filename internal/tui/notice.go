package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A notice is something the interface has to say that is not an error: a file
// was written, a choice was risky, a run was not saved.
//
// The severity travels with the text because the text travels between screens.
// One string field used to carry all of them, and each screen decided for
// itself how to draw it — green tick on the menu, amber bang on the review,
// plain muted on the generated screen. So "Only one recipient. Add a recovery
// key kept offline, or losing this key loses the archive." was set on the
// recipients screen and rendered as a green success on the menu, which is the
// opposite of what it says.
type level int

const (
	levelInfo level = iota // a statement of fact
	levelOK                // something succeeded
	levelWarn              // something the user should act on
)

type notice struct {
	text  string
	level level
}

func (n notice) empty() bool { return n.text == "" }

func (n notice) mark() (string, lipgloss.Style) {
	switch n.level {
	case levelOK:
		return "✓", stOK
	case levelWarn:
		return "!", stWarn
	}
	return "·", stMuted
}

// render draws the notice at the body's indent, wrapping onto further lines
// under its own mark rather than back under it.
func (n notice) render(width int) string {
	if n.empty() {
		return ""
	}
	mark, style := n.mark()

	var b strings.Builder
	for i, line := range strings.Split(wrap(n.text, maxInt(20, width-4)), "\n") {
		if i > 0 {
			b.WriteString("\n    ")
			b.WriteString(style.Render(line))
			continue
		}
		b.WriteString("  " + style.Render(mark+" "+line))
	}
	return b.String()
}
