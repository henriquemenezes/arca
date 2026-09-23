package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// chrome is what every screen fills in, and render is the only thing that
// turns one into a screen.
//
// The screens used to build themselves: each appended a header, its own body,
// its own description line and its own key legend to a strings.Builder. That
// is why the descriptions ended up at two different indents, why the errors
// ended up underneath the key legend, and why the review screen listed its
// keys in a different order from everything else. A screen that does not draw
// its own footer cannot put it in the wrong place.
type chrome struct {
	// art is drawn above the header line. Only the entry screen has any.
	art string

	title    string
	crumb    string
	subtitle []string
	body     string

	// keys are the screen's own, most useful first. The footer draws as many
	// as fit on one line and the overlay draws them all, so the order is a
	// statement about which matter most — not about which will be shown.
	keys []binding

	primary binding // enter, wearing this screen's verb
	back    binding // esc, on the screens that have somewhere to go

	// backQuits says the back slot already covers leaving, so the footer does
	// not add a second entry saying the same thing. The entry screen is the
	// one place this is true: there is nowhere behind it, so esc quits, and
	// "esc quit  ·  q quit" says one thing twice.
	backQuits bool

	// busy is a screen that is not listening: a backup or a restore in
	// flight, where m.key returns before reading anything. Its footer is
	// ctrl+c alone, because offering keys a screen ignores is the same defect
	// as hiding keys it answers to.
	busy bool
}

// inner is the width a screen may draw into: the terminal, less the padding
// View puts around every one of them.
func (m *model) inner() int { return maxInt(20, m.width-4) }

// bodyRows is what the body may spend: the terminal less the padding, the
// header, the subtitle, the message and the footer. It reads everything on the
// chrome except the body, so a view can call it while still deciding what the
// body is.
func (m *model) bodyRows(c chrome) int {
	rows := m.height - 2 // View's vertical padding
	if c.art != "" {
		rows -= lipgloss.Height(c.art) + 1
	}
	rows -= 2                 // the header's two lines
	rows -= m.subtitleRows(c) // and its description, once wrapped
	rows--                    // the blank line closing the header block
	rows -= 2                 // the blank line before the footer, and the footer
	if !m.message().empty() {
		rows -= 2
	}
	return maxInt(3, rows)
}

// subtitleRows is how many lines the description actually occupies, which is
// not len(subtitle) once a narrow terminal has wrapped it.
func (m *model) subtitleRows(c chrome) int {
	n := 0
	for _, s := range c.subtitle {
		n += strings.Count(wrap(s, m.inner()-2), "\n") + 1
	}
	return n
}

// message is the one thing the screen has to say right now.
//
// A pending quit beats an error beats a notice, which is the order in which
// they are about to matter: the quit is asking a question, the error belongs
// to the last keystroke, and the notice has been true for a while.
func (m *model) message() notice {
	if m.confirmQuit {
		return notice{m.quitWarning(), levelWarn}
	}
	if m.err != nil {
		return notice{"error: " + m.err.Error(), levelWarn}
	}
	return m.notice
}

func (m *model) render(c chrome) string {
	if m.showKeys {
		c = m.keysChrome(c)
	}

	var b strings.Builder
	if c.art != "" {
		b.WriteString(c.art + "\n\n")
	}
	b.WriteString(stTitle.Render("arca") + "  " + stCrumb.Render(c.crumb) + "\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(clip(c.title, m.inner())) + "\n")
	for _, s := range c.subtitle {
		// A description is written for a comfortable terminal and has to
		// survive a narrow one. Wrapping rather than clipping, because unlike
		// a path a sentence is not recognisable from one end of it.
		for _, line := range strings.Split(wrap(s, m.inner()-2), "\n") {
			b.WriteString("  " + stMuted.Render(line) + "\n")
		}
	}
	b.WriteString("\n")

	b.WriteString(strings.TrimRight(c.body, "\n"))

	if msg := m.message(); !msg.empty() {
		b.WriteString("\n\n" + msg.render(m.inner()))
	}
	return b.String() + "\n\n" + m.footer(c)
}

// footer draws the key legend, and never wraps.
//
// The screen's own keys come first in the order it gave them, then the four
// that are always there. What does not fit is dropped rather than folded onto
// a second line: the overlay has all of them, so a key pushed off the footer
// is still a key the user can find, while a footer that grows a line pushes
// the body off the top of the terminal.
func (m *model) footer(c chrome) string {
	if c.busy {
		return stHelp.Render(kAbort.label())
	}

	// The tail is reserved before anything is measured, not appended after.
	// Appending it is how a first attempt at this dropped "enter continue" and
	// "q quit" off the source screen: the twelve keys in front of them ate the
	// line, and the two that every screen must name were the ones to go.
	var tail []binding
	if c.primary.set() {
		tail = append(tail, c.primary)
	}
	if c.back.set() {
		tail = append(tail, c.back)
	}
	if !c.backQuits {
		tail = append(tail, kQuit)
	}
	tail = append(tail, kHelp)

	const sep = "  ·  "
	used := 0
	for _, b := range tail {
		used += lipgloss.Width(b.label()) + lipgloss.Width(sep)
	}
	used -= lipgloss.Width(sep) // the last one is not followed by a separator

	// Whatever room is left goes to the screen's own keys, in the order it
	// ranked them.
	var head []binding
	for _, b := range c.keys {
		cost := lipgloss.Width(b.label()) + lipgloss.Width(sep)
		if used+cost > m.inner() {
			break
		}
		used += cost
		head = append(head, b)
	}

	parts := make([]string, 0, len(head)+len(tail))
	for _, b := range append(head, tail...) {
		parts = append(parts, stHelp.Render(b.label()))
	}
	return strings.Join(parts, stCrumb.Render(sep))
}

// padTo pads to a display width rather than a byte count, which is the
// difference between a column of keys that lines up and one that does not: an
// arrow is three bytes and one column wide.
func padTo(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// keysChrome replaces a screen with the full list of what it answers to.
//
// It is the footer's other half. The footer can only carry the keys that fit
// on one line, which on the source screen is about half of them, and the
// aliases every list has answered to all along — j k, g G, the page keys —
// were named nowhere at all.
func (m *model) keysChrome(c chrome) chrome {
	bs := append([]binding(nil), c.keys...)
	if c.primary.set() {
		bs = append(bs, c.primary)
	}
	if c.back.set() {
		bs = append(bs, c.back)
	}
	if !c.backQuits {
		bs = append(bs, kQuit)
	}
	bs = append(bs, kHelp, kAbort)

	// The keys column is as wide as its widest entry, so the verbs line up.
	width := 0
	for _, b := range bs {
		width = maxInt(width, lipgloss.Width(b.describe()))
	}

	var body strings.Builder
	for _, b := range bs {
		body.WriteString("  " + stEmph.Render(padTo(b.describe(), width)) +
			"   " + stMuted.Render(b.verb) + "\n")
	}

	return chrome{
		title:    "Keys",
		crumb:    strings.TrimSpace(c.crumb) + " › keys",
		subtitle: []string{"Everything this screen answers to."},
		body:     body.String(),
		back:     leave("close"),
	}
}
