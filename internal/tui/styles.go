// Package tui is the interactive front-end.
//
// It is a front-end and nothing more: every screen ends up building the same
// config.Config and calling the same archive functions the CLI does, so there
// is one execution path and one place where behaviour can go wrong.
package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

var (
	colAccent = lipgloss.Color("39")
	colOK     = lipgloss.Color("42")
	colWarn   = lipgloss.Color("214")
	colDanger = lipgloss.Color("203")
	colMuted  = lipgloss.Color("245")
	colFaint  = lipgloss.Color("240")

	stTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	stCrumb = lipgloss.NewStyle().Foreground(colFaint)
	stHelp  = lipgloss.NewStyle().Foreground(colFaint)
	stMuted = lipgloss.NewStyle().Foreground(colMuted)
	stOK    = lipgloss.NewStyle().Foreground(colOK)
	stWarn  = lipgloss.NewStyle().Foreground(colWarn)
	stErr   = lipgloss.NewStyle().Foreground(colDanger).Bold(true)
	stKey   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	stSelected = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	stDir      = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	stMark     = lipgloss.NewStyle().Foreground(colOK).Bold(true)

	stPanel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colFaint).
		Padding(0, 1)

	stNotice = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colWarn).
			Padding(0, 2)
)

// header renders the consistent title line every screen starts with.
func header(title, crumb string) string {
	line := stTitle.Render("arca") + "  " + stCrumb.Render(crumb)
	return line + "\n" + lipgloss.NewStyle().Bold(true).Render(title) + "\n\n"
}

// newMenuList builds the entry screen's list. The screen draws its own header
// and help line, as every other screen does, so the list contributes only the
// items themselves.
func newMenuList() list.Model {
	l := list.New(menuItems, menuDelegate(true), 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings() // the screen decides what quitting means
	l.Styles.PaginationStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(colFaint)
	return l
}

// menuDelegate draws the items in arca's palette. Without blurbs it collapses
// to one line per item, which is what a short terminal gets.
func menuDelegate(blurbs bool) list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = blurbs
	if !blurbs {
		d.SetSpacing(0)
	}
	// A heavier stroke than the list's default, so the selected row is easy to
	// find without having to read the colour.
	bar := lipgloss.OuterHalfBlockBorder()
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Border(bar, false, false, false, true).
		BorderForeground(colAccent).Foreground(colAccent).Bold(true)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Border(bar, false, false, false, true).
		BorderForeground(colAccent).Foreground(colMuted).Bold(false)
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(lipgloss.NoColor{})
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(colFaint)
	return d
}

// itemRows is how many rows the list needs to show every item at once: each
// item plus the gap that follows it, including a trailing gap the list pads in.
func itemRows(d list.DefaultDelegate, n int) int {
	return n * (d.Height() + d.Spacing())
}

// The entry screen spends whatever rows are left over on the mark. The menu
// needs 26 rows at its tallest — a blurb under every item, plus the notice left
// behind by generating a key — and the full mark adds 17 to that while the ark
// alone adds 10. The thresholds below are where the artwork still leaves the
// menu enough room to stay complete, one line per item.
const (
	minHeightForMark = 32
	minHeightForArk  = 25
)

func help(keys ...string) string {
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += stCrumb.Render("  ·  ")
		}
		out += stHelp.Render(k)
	}
	return "\n" + out
}
