// Package tui is the interactive front-end.
//
// It is a front-end and nothing more: every screen ends up building the same
// config.Config and calling the same archive functions the CLI does, so there
// is one execution path and one place where behaviour can go wrong.
package tui

import "github.com/charmbracelet/lipgloss"

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

// The entry screen spends whatever rows are left over on the mark. The menu
// needs 15 rows at its tallest — the extra row is the notice left behind by
// generating a key — and the full mark adds 17 to that while the ark alone adds
// 10. Below the smaller sum the screen shows no artwork rather than scroll the
// menu out of view.
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
