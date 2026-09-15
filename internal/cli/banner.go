package cli

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Tagline is the one-line description that travels with the mark.
//
// The artwork ships with a Portuguese tagline; the rest of this program speaks
// English, so the English wording is used and the artwork itself is untouched.
// Changing this constant is all it takes to switch.
const Tagline = "encrypted, compressed backups"

// The ark is reproduced exactly as delivered, in both banner variants. Do not
// reindent: the drawing is aligned to a 32-column grid so it fits a 40-column
// terminal.
const ark = `            ________
           /        \
          /__________\
          |  ______  |
          |  | () |  |
    ______|__|____|__|______
    \                      /
     \____________________/
   ~~~~~~~~~~~~~~~~~~~~~~~~~~`

// wordmarkUnicode is the banner lettering for terminals that can render box
// drawing characters.
const wordmarkUnicode = `  ▄▄▄    ▄ ▄▄   ▄▄▄    ▄▄▄
 ▀   █   █▀  ▀ █▀  ▀  ▀   █
 ▄▀▀▀█   █     █      ▄▀▀▀█
 ▀▄▄▀█   █     ▀█▄▄▀  ▀▄▄▀█`

// wordmarkASCII is the same lettering for a terminal without UTF-8, for
// TERM=dumb and for log files. Written as an interpreted string because the
// artwork contains a backtick.
const wordmarkASCII = "  __ _ _ __ ___ __ _\n" +
	" / _` | '__/ __/ _` |\n" +
	"| (_| | | | (_| (_| |\n" +
	" \\__,_|_|  \\___\\__,_|"

// Brand palette, from the project's visual identity. The hull, roof, water and
// lock colours read correctly on a light or a dark terminal, so they are fixed;
// the lettering is the one element that has to follow the background, exactly
// as the README logo does.
var (
	brandHull = lipgloss.NewStyle().Foreground(lipgloss.Color("#56D364"))
	brandWave = lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF"))
	brandLock = lipgloss.NewStyle().Foreground(lipgloss.Color("#E3B341"))
	brandInk  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{
		Light: "#161B22",
		Dark:  "#E6EDF3",
	})
)

// BannerOpts selects a rendering of the mark. The zero value is the safe one:
// plain ASCII, no colour.
type BannerOpts struct {
	Version string
	Unicode bool
	Color   bool
}

// AutoBannerOpts picks the rendering this terminal can actually display.
func AutoBannerOpts(version string) BannerOpts {
	unicode := UnicodeOK()
	return BannerOpts{Version: version, Unicode: unicode, Color: unicode}
}

// UnicodeOK reports whether the terminal can be trusted with characters
// outside ASCII. Locale precedence is the POSIX one: the first variable that
// is set decides, so LC_ALL=C silences a UTF-8 LANG rather than losing to it.
func UnicodeOK() bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			up := strings.ToUpper(v)
			return strings.Contains(up, "UTF-8") || strings.Contains(up, "UTF8")
		}
	}
	return false
}

// Mark renders the artwork alone: the ark and the lettering, with no version
// or tagline. It is what the interactive interface shows on its entry screen.
func Mark(o BannerOpts) string {
	wordmark := wordmarkASCII
	if o.Unicode {
		wordmark = wordmarkUnicode
	}
	return paintArk(ark, o.Color) + "\n\n" + paint(wordmark, brandInk, o.Color)
}

// Ark renders the drawing without the lettering, for screens that have room
// for the mark but not for all of it.
func Ark(o BannerOpts) string {
	return paintArk(ark, o.Color)
}

// Banner renders the full mark plus the version and tagline. It is what
// `arca --version` shows on a terminal.
func Banner(o BannerOpts) string {
	var b strings.Builder
	b.WriteString(Mark(o))
	b.WriteString("\n\n")

	if o.Version != "" {
		b.WriteString(center("arca "+o.Version, o.Color) + "\n")
	}
	b.WriteString(center(Tagline, o.Color) + "\n")
	return b.String()
}

// CompactBanner is the four-line variant that heads `arca --help`.
func CompactBanner(version string) string {
	color := UnicodeOK()
	name := "arca"
	if version != "" {
		name += " " + version
	}
	lines := []string{
		brandHullIf("     ___", color),
		brandHullIf("  __|_o_|__", color) + "     " + paint(name, brandInk, color),
		brandHullIf(`  \_______/`, color) + "     " + paint(Tagline, brandInk, color),
		brandWaveIf("   ~~~~~~~", color),
	}
	return strings.Join(lines, "\n")
}

// paintArk colours a drawing: hull green, water blue, and the lock amber
// because it is the one part that stands for the encryption.
func paintArk(art string, color bool) string {
	if !color {
		return art
	}
	lines := strings.Split(art, "\n")
	for i, line := range lines {
		switch {
		case strings.Contains(line, "~"):
			lines[i] = brandWave.Render(line)
		case strings.Contains(line, "()"):
			lines[i] = paintAround(line, "()", brandHull, brandLock)
		case strings.Contains(line, "_o_"):
			lines[i] = paintAround(line, "o", brandHull, brandLock)
		default:
			lines[i] = brandHull.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}

// paintAround renders a line in one style with a single run picked out in
// another.
func paintAround(line, mark string, around, highlight lipgloss.Style) string {
	i := strings.Index(line, mark)
	return around.Render(line[:i]) + highlight.Render(mark) + around.Render(line[i+len(mark):])
}

func paint(s string, style lipgloss.Style, color bool) string {
	if !color {
		return s
	}
	// Style each line separately: lipgloss pads a multi-line block to a common
	// width, which would leave trailing spaces on the shorter rows.
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}
	return strings.Join(lines, "\n")
}

func brandHullIf(s string, color bool) string { return paint(s, brandHull, color) }
func brandWaveIf(s string, color bool) string { return paint(s, brandWave, color) }

// center places a line under the 32-column artwork.
func center(s string, color bool) string {
	const width = 32
	pad := 0
	if n := utf8.RuneCountInString(s); n < width {
		pad = (width - n) / 2
	}
	return strings.Repeat(" ", pad) + paint(s, brandInk, color)
}

// fancyOutput reports whether stdout is a terminal, and therefore whether the
// artwork is welcome. Piped output stays plain so `arca --version` remains
// something a script can read. Swapped in tests.
var fancyOutput = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// VersionText is what `arca --version` prints.
func VersionText(version string, fancy bool) string {
	if !fancy {
		return "arca version " + version + "\n"
	}
	return "\n" + Banner(AutoBannerOpts(version)) + "\n"
}
