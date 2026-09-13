package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Styles are shared with the TUI so both front-ends look like one tool.
var (
	StyleTitle   = lipgloss.NewStyle().Bold(true)
	StyleKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	StyleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	StyleDanger  = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	StyleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	StyleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	StyleNoticeB = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("214")).
			Padding(0, 2)
)

// HumanBytes formats a size for people rather than for machines.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// SecretNotice is printed after every operation that creates key material or an
// archive. Losing the secret is the single failure this tool cannot help with,
// so it is stated every time rather than buried in documentation.
const SecretNotice = `KEEP THE SECRET — without it this archive is indistinguishable from noise.

  · A password manager (1Password, Bitwarden, pass) is the minimum.
  · Keep an offline copy on paper, stored apart from the backup itself.
  · NEVER store the secret next to the archive.
  · Key mode: use several recipients — your everyday key and a recovery key
    that stays offline. That is the protection against locking yourself out.
  · There is no recovery. Lose the secret and the backup is gone.`

func PrintSecretNotice(w io.Writer) {
	fmt.Fprintln(w, StyleNoticeB.Render(SecretNotice))
}

// PrintWarnings surfaces everything that was skipped or adjusted. A backup that
// quietly leaves things out is worse than one that complains.
func PrintWarnings(w io.Writer, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w, StyleWarn.Render(Count(len(warnings), "warning", "warnings")+":"))
	const maxShown = 20
	for i, msg := range warnings {
		if i == maxShown {
			fmt.Fprintln(w, StyleMuted.Render(fmt.Sprintf("  … and %d more (see the archive summary)", len(warnings)-maxShown)))
			break
		}
		fmt.Fprintf(w, "  %s %s\n", StyleWarn.Render("!"), msg)
	}
}

// Count renders a quantity with the right singular or plural noun.
func Count(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// Field renders one aligned label/value line.
func Field(w io.Writer, label, value string) {
	fmt.Fprintf(w, "  %s %s\n", StyleMuted.Render(pad(label+":", 14)), value)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
