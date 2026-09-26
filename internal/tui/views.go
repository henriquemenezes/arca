package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/henriquemenezes/arca/internal/cli"
	"github.com/henriquemenezes/arca/internal/secret"
)

func timeNow() time.Time { return time.Now() }

func writeIdentity(path string) (string, error) { return cli.WriteIdentity(path) }

// Every screen answers with a chrome rather than a string. What it fills in is
// what it knows — a title, a body, the keys it listens to — and everything
// that has to look the same on all of them is drawn once, in render.

// ---------- menu ----------

func (m *model) viewMenu() chrome {
	c := chrome{
		title:   "What would you like to do?",
		crumb:   "menu",
		keys:    []binding{kMove},
		primary: confirm("choose"),
		// Esc quits from the entry screen, as it always has, and so does q.
		// Both are named because a key that acts and says nothing is the
		// defect being removed here — but they are named once, together,
		// since they do the same thing.
		back:      binding{keys: []string{"esc"}, shown: "esc / q", verb: "quit"},
		backQuits: true,
	}

	switch opts := cli.AutoBannerOpts(""); {
	case m.height >= minHeightForMark:
		c.art = cli.Mark(opts)
	case m.height >= minHeightForArk:
		c.art = cli.Ark(opts)
	}

	switch {
	case m.fromDisk:
		c.subtitle = []string{"Using " + shorten(m.cfgPath, m.inner()-8) +
			" — a backup starts from its review."}
	case m.hasConfig():
		c.subtitle = []string{"Using the configuration built in this session, which is not saved."}
	default:
		c.subtitle = []string{"No arca.toml found; you will pick what to back up."}
	}

	m.fitMenu(m.bodyRows(c))
	c.body = m.menu.View()
	return c
}

// fitMenu hands the list the rows the rest of the screen leaves it. When they
// are too few to carry a blurb under every item the blurbs go, the same trade
// the artwork above makes; when even the bare titles do not fit, the list
// paginates rather than running off the bottom.
func (m *model) fitMenu(rows int) {
	n := len(m.menu.Items())
	d := menuDelegate(rows >= itemRows(menuDelegate(true), n))
	m.menu.SetDelegate(d)

	want := itemRows(d, n)
	m.menu.SetShowPagination(rows < want)
	m.menu.SetSize(maxInt(20, m.width-6), maxInt(d.Height()+d.Spacing(), minInt(rows, want)))
}

// ---------- backup flow ----------

// viewSources is two columns: the browser on the left, and on the right what
// the selection amounts to so far.
//
// The preview exists because the browser answers "where am I" but never "what
// have I chosen". A selection is built across several directories, and until
// this screen showed it, the only place the whole of it appeared was the next
// screen — too late to notice that a home directory had been marked by
// accident, or that the one thing being looked for is not in it.
func (m *model) viewSources() chrome {
	c := chrome{title: "Choose what to back up", crumb: "backup › sources"}
	m.sourceKeys(&c)

	browserWidth, previewWidth := m.sourcesLayout()
	// The file list gets the rows the chrome leaves it, less the two the
	// browser spends on its own directory line and the one it spends saying
	// how far down the list the cursor is.
	m.browser.height = maxInt(3, m.bodyRows(c)-3)
	rows := m.browser.height

	// A terminal too narrow for two columns still has to be able to edit the
	// patterns, so there the panel takes the whole width while it has focus and
	// the file list stands down.
	if previewWidth == 0 {
		if m.editingExcludes() {
			c.body = m.excludesBody(browserWidth-2, rows)
			return c
		}
		c.body = strings.TrimRight(m.browser.View(browserWidth, m.excluded), "\n") +
			"\n\n" + "  " + stMuted.Render(clip(m.selectionLine(), m.inner()-2))
		return c
	}

	// Squared off at the column it was given, so the panel starts in the same
	// place whatever the browser happens to be listing.
	left := padBlock(strings.TrimRight(m.browser.View(browserWidth, m.excluded), "\n"), browserWidth)

	// The panel spends the same rows the file list does, so the two columns
	// start and end together however tall the terminal is.
	// The panel's width is what it occupies inside its border, and its padding
	// comes out of that; the text is laid out against what is left.
	inner := previewWidth - 4
	body := m.viewSelectionPreview(inner, rows)
	border := colFaint
	if m.editingExcludes() {
		body = m.excludesBody(inner, rows)
		// The border is the only thing that says which column the keys are
		// going to, and on this screen that changes.
		border = colAccent
	}
	right := stPanel.MarginLeft(previewGap).BorderForeground(border).
		Width(previewWidth - 2).Height(rows).Render(body)

	c.body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return c
}

// padBlock squares a column off at the width it was drawn for.
//
// JoinHorizontal measures the block it is handed, not the column that block
// belongs to, and the browser's rows are only as wide as the names in them. So
// the panel sat wherever the longest filename left it — while the search, whose
// input field is padded to the column, held it against the right edge. The
// panel moving sideways on a keystroke that has nothing to do with it is the
// defect; padding is what fixes it in place.
func padBlock(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = padTo(line, width)
	}
	return strings.Join(lines, "\n")
}

// sourceKeys fills in the keys for whichever of the screen's jobs has the
// keyboard. Each mode takes it whole, so naming all of them at once would name
// keys that do nothing.
func (m *model) sourceKeys(c *chrome) {
	switch m.mode {
	case srcPath:
		c.subtitle = []string{"Type where to go; the list follows as you type."}
		c.keys = []binding{kComplete, kPick, kGo}
		c.primary = confirm("go")
		c.back = leave("file list")

	case srcFilter:
		// What to type is on the line above, in the field itself, so the
		// subtitle would only repeat it.
		c.keys = []binding{kMove, kComplete, kSelect, kExclude, kClear, kGo}
		c.primary = confirm("open")
		c.back = leave("leave search")

	case srcExcludes:
		// A is named here as well as in the legend: at a hundred columns the
		// footer has room for three of this panel's keys, and A is the one
		// nobody guesses.
		c.subtitle = []string{"Patterns are what each chosen path leaves behind.",
			"A offers ready-made ones, one group per language."}
		c.keys = []binding{kMove, kAdd, kRemove, kPresets, kGo}
		c.primary = confirm("add")
		c.back = leave("file list")

	case srcPreset:
		c.subtitle = []string{"Ready-made patterns, one group per language."}
		c.keys = []binding{kMove, kSelect, kFold, kScope, kEdge}
		c.primary = confirm("add")
		c.back = leave("excludes")

	case srcExcludeInput:
		c.subtitle = []string{"A pattern is relative to the source it belongs to."}
		c.primary = confirm("add")
		c.back = leave("cancel")

	default:
		c.subtitle = []string{"Space marks a file or directory; the panel counts what it holds."}
		c.keys = []binding{kMove, kSelect, kExclude, kFind, kPath, kExcludes,
			kOpen, kUp, kHome, kHidden, kPage, kEdge}
		c.primary = confirm("continue")
		c.back = kBack
	}
}

// editingExcludes says the excludes panel — or the preset menu that opens on
// top of it — has the keyboard, which is what moves the border and hands the
// preview column over.
func (m *model) editingExcludes() bool {
	return m.mode == srcExcludes || m.mode == srcExcludeInput || m.mode == srcPreset
}

// excludesBody is whichever of the two panels is in front.
func (m *model) excludesBody(width, rows int) string {
	if m.mode == srcPreset {
		return m.viewPresets(width, rows)
	}
	return m.viewExcludes(width, rows)
}

// selectionLine is the one-line form of the preview, for a terminal too narrow
// to carry the panel.
func (m *model) selectionLine() string {
	if len(m.browser.selected) == 0 {
		return "Nothing chosen yet; space marks the highlighted item."
	}
	files, bytes, pending := m.selectionTotals()
	line := fmt.Sprintf("%s selected · %s · %s",
		cli.Count(len(m.browser.selected), "item", "items"),
		cli.Count(files, "file", "files"), humanBytes(bytes))
	if n := m.excludeCount(); n > 0 {
		line += " · " + cli.Count(n, "exclude", "excludes")
	}
	if pending > 0 {
		line += " · counting…"
	}
	return line
}

func (m *model) viewMapping() chrome {
	c := chrome{
		title:    "Where should each one land inside the archive?",
		crumb:    "backup › mapping",
		subtitle: []string{"The destination is a directory in the archive; each source keeps its own name under it."},
		keys:     []binding{kMove, kRemove, kEditDest},
		primary:  confirm("continue"),
		back:     kBack,
	}
	if m.editing {
		c.keys = nil
		c.primary = confirm("accept")
		c.back = leave("cancel")
	}

	// Each entry is two rows, and the field takes three more when it is open.
	rows := m.bodyRows(c)
	if m.editing {
		rows -= 3
	}
	// Each entry costs two rows, and a list that does not fit spends up to two
	// more saying so — one marker at each end. They come out of the budget
	// rather than being drawn on top of it, which is the difference between
	// fitting and overflowing by exactly one line.
	fit := maxInt(1, rows/2)
	if len(m.mapping) > fit {
		fit = maxInt(1, (rows-2)/2)
	}
	start, end := window(m.mapIndex, len(m.mapping), fit)

	var b strings.Builder
	if start > 0 {
		b.WriteString("  " + stCrumb.Render(fmt.Sprintf("↑ %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		e := m.mapping[i]
		cursor := "  "
		if i == m.mapIndex {
			cursor = stSelected.Render("▸ ")
		}
		dest := e.dest
		if dest == "" {
			dest = stCrumb.Render("(archive root)")
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, shorten(e.path, m.inner()-4))
		fmt.Fprintf(&b, "      %s %s   %s %s\n",
			stMuted.Render("into"), dest, stMuted.Render("→"), stEmph.Render(e.member()))
	}
	if end < len(m.mapping) {
		b.WriteString("  " + stCrumb.Render(fmt.Sprintf("↓ %d more", len(m.mapping)-end)) + "\n")
	}

	if m.editing {
		b.WriteString("\n" + "  " + stMuted.Render("Destination directory:") + "\n  " +
			m.destInput.View() + "\n")
	}
	c.body = b.String()
	return c
}

// fitBody keeps as much of a body as the rows allow and says what it dropped.
//
// It is the blunt instrument, for the screens that have no cursor to keep in
// view: they simply stop when the terminal does. What makes it safe is that
// those screens are ordered by importance, so the part that goes is the part
// that was least worth the room.
func fitBody(body string, rows int) string {
	ls := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(ls) <= rows {
		return body
	}
	keep := maxInt(1, rows-1)
	return strings.Join(ls[:keep], "\n") + "\n  " +
		stCrumb.Render(fmt.Sprintf("+%d more", len(ls)-keep)) + "\n"
}

// window is the slice of a list that fits, kept around the cursor. It is what
// stops the mapping screen from drawing forty sources and pushing its own
// footer off the terminal.
func window(cursor, n, rows int) (start, end int) {
	if n <= rows {
		return 0, n
	}
	start = cursor - rows/2
	if start < 0 {
		start = 0
	}
	if start+rows > n {
		start = n - rows
	}
	return start, start + rows
}

func (m *model) viewCrypto() chrome {
	c := chrome{
		title: "How should the archive be encrypted?",
		crumb: "backup › encryption",
		subtitle: []string{
			"age does not allow both at once: a passphrase must be the only way in,",
			"otherwise the archive would fall back to the strength of that passphrase.",
		},
		keys:    []binding{kMove},
		primary: confirm("choose"),
		back:    kBack,
	}

	var b strings.Builder
	for i, item := range cryptoItems {
		cursor, title := "  ", item.title
		if i == m.cryptoIndex {
			cursor, title = stSelected.Render("▸ "), stSelected.Render(item.title)
		}
		b.WriteString(cursor + title + "\n")
		if i == m.cryptoIndex {
			b.WriteString("    " + stMuted.Render(item.blurb) + "\n")
		}
	}
	c.body = b.String()
	return c
}

func (m *model) viewPassphrase() chrome {
	c := chrome{
		title:   "Type a passphrase",
		crumb:   "backup › encryption › passphrase",
		primary: confirm("continue"),
		back:    kBack,
	}

	var b strings.Builder
	if m.passStage == 0 {
		c.subtitle = []string{"Checked for strength before anything is written."}
		b.WriteString("  " + m.passInput.View() + "\n\n")
		b.WriteString(strengthMeter(m.passInput.Value(), 30))
	} else {
		c.subtitle = []string{"Type it once more. A typo here is unrecoverable."}
		b.WriteString("  " + stMuted.Render("passphrase accepted") + "\n\n")
		b.WriteString("  " + m.confirmPass.View() + "\n")
	}
	c.body = b.String()
	return c
}

// strengthMeter gives immediate feedback, because a strength check that only
// fires on submit teaches nothing.
func strengthMeter(pass string, width int) string {
	if pass == "" {
		return stMuted.Render("  Strength appears as you type.") + "\n"
	}
	s := secret.Estimate(pass)
	filled := (s.Score + 1) * width / 5
	style := stErr
	switch {
	case s.Score >= secret.MinScore+1:
		style = stOK
	case s.Score >= secret.MinScore:
		style = stWarn
	}
	bar := style.Render(strings.Repeat("█", filled)) + stCrumb.Render(strings.Repeat("░", width-filled))
	line := fmt.Sprintf("  %s  %s", bar, style.Render(s.Summary))
	if s.Score < secret.MinScore {
		line += "\n\n" + stMuted.Render("  Too weak to accept. Press esc and choose \"Generate a passphrase\" instead.")
	}
	return line + "\n"
}

func (m *model) viewRecipients() chrome {
	return chrome{
		title: "Encrypt to age recipients",
		crumb: "backup › encryption › recipients",
		subtitle: []string{
			"Any one of these keys can open the archive.",
			"List two: the key you use day to day, and a recovery key kept offline.",
		},
		body:    "  " + m.rcptInput.View() + "\n",
		primary: confirm("continue"),
		back:    kBack,
	}
}

func (m *model) viewOutput() chrome {
	c := chrome{
		title:    "Where should the archive be written?",
		crumb:    "backup › destination",
		subtitle: []string{"A directory gets a generated name; a file path is used as given."},
		keys:     []binding{kComplete, kPick},
		primary:  confirm("continue"),
		back:     kBack,
	}

	var b strings.Builder
	b.WriteString("  " + m.outInput.View() + "\n\n")
	if list := m.outInput.ViewList(m.width, m.pathListRows()); list != "" {
		b.WriteString(list + "\n")
	}
	if m.cfg != nil {
		b.WriteString("  " + stMuted.Render("Name: "+m.archiveName()) + "\n")
	}
	c.body = b.String()
	return c
}

func (m *model) viewReview() chrome {
	c := chrome{
		title:   "Review",
		crumb:   "backup › review",
		keys:    []binding{kEditCfg, kNewCfg},
		primary: confirm("start"),
		back:    kBack,
	}

	// A plan that could not be built is the moment the configuration most needs
	// changing — a source was renamed, or deleted — so the two ways to change it
	// have to be reachable from here too.
	if m.plan == nil {
		c.primary = binding{}
		c.body = m.viewConfigSummary() + "  " + stMuted.Render("Nothing planned yet.") + "\n"
		return c
	}

	// The sections are in the order they are worth the room, not the order
	// they were written in. What this screen is for is the decision to press
	// enter, and that is made on the totals, the pipeline and the warnings;
	// the mapping only says in detail what the totals already say. So the
	// mapping is what a short terminal loses, and it goes last.
	var b strings.Builder
	b.WriteString(m.viewConfigSummary())

	b.WriteString(stTitle.Render("Totals") + "\n")
	fmt.Fprintf(&b, "  %s · %s · %s\n\n",
		cli.Count(m.plan.Stats.Files, "file", "files"),
		cli.Count(m.plan.Stats.Dirs, "directory", "directories"),
		humanBytes(m.plan.Stats.Bytes))

	b.WriteString(stTitle.Render("Pipeline") + "\n")
	enc := "passphrase"
	if n := len(m.cfg.Encryption.Recipients); n > 0 {
		enc = cli.Count(n, "recipient", "recipients")
	}
	fmt.Fprintf(&b, "  tar → %s (%s) → %s, %s\n",
		m.cfg.Settings.Compressor, m.cfg.Settings.Compression, m.cfg.Settings.Cipher, enc)
	if m.outPath != "" {
		b.WriteString("  " + stMuted.Render(shorten(m.outPath, m.inner()-2)) + "\n")
	}

	if w := m.viewWarnings(m.plan.Warnings); w != "" {
		b.WriteString(w)
	}

	b.WriteString("\n" + stTitle.Render("Mapping") + "\n")
	for _, src := range m.plan.Sources {
		fmt.Fprintf(&b, "  %s\n      %s %s  %s\n",
			shorten(src.Path, m.inner()-4),
			stMuted.Render("→"), stEmph.Render(src.Member),
			stMuted.Render(fmt.Sprintf("%s, %s",
				cli.Count(src.Stats.Files, "file", "files"), humanBytes(src.Stats.Bytes))))
	}

	c.body = fitBody(b.String(), m.bodyRows(c))
	return c
}

// viewWarnings draws at most five, which is enough to know what kind of
// trouble it is. Both screens that report warnings use it, so neither can go
// back to listing all of them and pushing its own footer off the terminal.
func (m *model) viewWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	const most = 5
	var b strings.Builder
	b.WriteString("\n" + stWarn.Render(cli.Count(len(warnings), "warning", "warnings")+":") + "\n")
	for i, w := range warnings {
		if i == most {
			b.WriteString("  " + stCrumb.Render(fmt.Sprintf("+%d more", len(warnings)-most)) + "\n")
			break
		}
		b.WriteString("  " + stWarn.Render("!") + " " + shorten(w, m.inner()-4) + "\n")
	}
	return b.String()
}

// viewConfigSummary names the configuration under review and what each of its
// groups holds. Without it the review shows what would happen but never which
// file said so, and "edit config" would be an offer to change something the
// screen never identified.
func (m *model) viewConfigSummary() string {
	if m.cfg == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(stTitle.Render("Configuration") + "\n")
	if m.fromDisk && m.cfgPath != "" {
		b.WriteString("  " + stEmph.Render(shorten(m.cfgPath, m.inner()-2)) + "\n")
	} else {
		b.WriteString("  " + stMuted.Render("built in this session, not saved") + "\n")
	}
	for _, g := range m.cfg.Groups {
		line := fmt.Sprintf("  %s %s", stEmph.Render(padRight(groupLabel(g.Name), 14)),
			stMuted.Render(cli.Count(len(g.Sources), "source", "sources")))
		if n := len(g.Exclude); n > 0 {
			line += stMuted.Render(" · " + cli.Count(n, "exclude pattern", "exclude patterns"))
		}
		b.WriteString(line + "\n")
	}
	return b.String() + "\n"
}

// groupLabel names a group that reached the archive root, where dest and, with
// it, the usual name are both empty.
func groupLabel(name string) string {
	if name == "" {
		return "(archive root)"
	}
	return name
}

func (m *model) viewSaveConfig() chrome {
	c := chrome{
		title:    "Save this configuration?",
		crumb:    "backup › configuration",
		subtitle: []string{"Saving it means the next `arca backup` starts from the review."},
		keys:     []binding{kComplete, kPick, kSkip},
		primary:  confirm("save"),
		back:     kBack,
	}

	var b strings.Builder
	b.WriteString("  " + m.cfgInput.View() + "\n\n")
	if list := m.cfgInput.ViewList(m.width, m.pathListRows()); list != "" {
		b.WriteString(list + "\n")
	}

	path, err := cli.ResolveConfigPath(strings.TrimSpace(m.cfgInput.Value()))
	if err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			if m.confirmSave == path {
				b.WriteString("  " + stWarn.Render("Press enter again to replace "+
					shorten(path, m.inner()-28)) + "\n")
			} else {
				b.WriteString("  " + stWarn.Render("A configuration is already there; saving replaces it.") + "\n")
			}
			// The shipped arca.toml is mostly comments, and they are the one
			// thing a round trip through this interface cannot carry.
			b.WriteString("  " + stMuted.Render("Comments in the existing file are not carried over.") + "\n")
		}
	}

	b.WriteString("\n" + stTitle.Render("What would be written") + "\n")
	b.WriteString(m.previewTOML(m.bodyRows(c) - lipgloss.Height(b.String())))
	c.body = b.String()
	return c
}

// previewTOML shows as much of the rendered file as the terminal has room for.
// The point is to make "replaces it" concrete before the second enter, so what
// matters is the top of the file, not all of it.
func (m *model) previewTOML(rows int) string {
	if m.cfg == nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(m.renderTOML(m.cfg), "\n"), "\n")
	rows = maxInt(3, rows)

	var b strings.Builder
	for i, line := range lines {
		if i == rows {
			b.WriteString("    " + stCrumb.Render(fmt.Sprintf("+%d more", len(lines)-rows)) + "\n")
			break
		}
		b.WriteString("    " + stMuted.Render(shorten(line, m.inner()-4)) + "\n")
	}
	return b.String()
}

func (m *model) viewRunning() chrome {
	var ratio float64
	if m.plan != nil && m.plan.Stats.Bytes > 0 {
		ratio = float64(m.lastProg.Bytes) / float64(m.plan.Stats.Bytes)
	}
	return chrome{
		title:    "Writing the archive",
		crumb:    "backup › running",
		subtitle: []string{"Encrypted on the way out; nothing is written in the clear."},
		body:     m.progressBody(ratio, planBytes(m)),
		busy:     true,
	}
}

// progressBody is what a run in flight looks like, and both runs look the
// same. The restore screen used to be three lines of text beside the backup
// screen's bar, for no reason other than that they were written apart.
func (m *model) progressBody(ratio float64, total int64) string {
	var b strings.Builder
	if total > 0 {
		b.WriteString("  " + m.bar.ViewAs(minFloat(ratio, 1)) + "\n\n")
		fmt.Fprintf(&b, "  %s of %s · %s\n",
			humanBytes(m.lastProg.Bytes), humanBytes(total),
			cli.Count(m.lastProg.Files, "file", "files"))
	} else {
		// No total to measure against: an archive written before the manifest
		// recorded one. Saying how far along is better than a bar that lies.
		fmt.Fprintf(&b, "  %s · %s\n",
			cli.Count(m.lastProg.Files, "file", "files"), humanBytes(m.lastProg.Bytes))
	}
	b.WriteString("  " + stMuted.Render(shorten(m.lastProg.Member, m.inner()-2)) + "\n")
	return b.String()
}

func planBytes(m *model) int64 {
	if m.plan == nil {
		return 0
	}
	return m.plan.Stats.Bytes
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (m *model) viewDone() chrome {
	c := chrome{
		title:   "Backup complete",
		crumb:   "backup › done",
		primary: confirm("menu"),
	}
	if m.result == nil {
		return c
	}
	if !m.fromDisk {
		c.keys = []binding{kSave}
		c.subtitle = []string{"s saves this selection to " + m.saveTarget() + "."}
	}

	var b strings.Builder
	b.WriteString("  " + stOK.Render(shorten(m.result.Path, m.inner()-2)) + "\n\n")
	fmt.Fprintf(&b, "  %s · %s from %s of sources\n",
		cli.Count(m.result.Stats.Files, "file", "files"),
		humanBytes(m.result.ArchiveBytes), humanBytes(m.result.Stats.Bytes))

	if len(m.result.Warnings) > 0 {
		b.WriteString("  " + stWarn.Render(cli.Count(len(m.result.Warnings), "warning", "warnings")+
			" recorded in the archive summary") + "\n")
	}

	b.WriteString("\n" + stTitle.Render("Restore without arca, on any Unix machine") + "\n")
	b.WriteString("  " + stPayload.Render(cli.ManualRestoreCommand(m.result.Path, m.cfg)) + "\n\n")

	text := cli.SecretNotice
	if m.genPass != "" {
		text = "Your passphrase — save it now, it is shown once:\n\n    " + m.genPass + "\n\n" + cli.SecretNotice
	}
	b.WriteString(stNotice.Width(minInt(m.inner()-4, 84)).Render(text) + "\n")

	c.body = b.String()
	return c
}

// ---------- reading an archive ----------

// readCrumb prefixes the screens that inspect and restore share. They are one
// set of screens doing one job for two reasons, and a restore that reads
// "archive › key" halfway through says nothing about where it is going.
func (m *model) readCrumb(leaf string) string {
	if m.intent == intentRestore {
		return "restore › " + leaf
	}
	return "inspect › " + leaf
}

func (m *model) viewPickArchive() chrome {
	c := chrome{
		title: "Choose an archive to inspect",
		crumb: m.readCrumb("archive"),
		// The browser here picks one file, so enter is what chooses it rather
		// than what moves on.
		primary: confirm("choose"),
		back:    kBack,
	}
	if m.intent == intentRestore {
		c.title = "Choose an archive to restore"
	}
	m.pickArchiveKeys(&c)

	m.browser.height = maxInt(3, m.bodyRows(c)-3)
	c.body = m.browser.View(m.browserWidth(), nil)
	return c
}

// pickArchiveKeys names the keys for whichever of the screen's two jobs has
// the keyboard. The search takes it whole — every printable key is a character
// of the query — so naming the file list's keys while it is open would name
// keys that do nothing.
func (m *model) pickArchiveKeys(c *chrome) {
	if m.browser.filter == nil {
		c.keys = []binding{kMove, kFind, kOpen, kUp, kHome, kHidden, kPage, kEdge}
		return
	}
	// Enter does both things a row can be for, and which one it does depends on
	// the row rather than on a key, so it is worth a line saying so.
	c.subtitle = []string{"Enter opens a directory, or chooses the archive under the cursor."}
	c.keys = []binding{kMove, kComplete, kClear}
	c.back = leave("leave search")
}

func (m *model) viewArchiveKey() chrome {
	return chrome{
		title: "Unlock the archive",
		crumb: m.readCrumb("key"),
		// The hint sits above the field now, where every other screen's does.
		subtitle: []string{"A passphrase, or the path to an age identity file."},
		body: "  " + stMuted.Render(shorten(m.archivePath, m.inner()-2)) + "\n\n" +
			"  " + m.passInput.View() + "\n",
		primary: confirm("unlock"),
		back:    kBack,
	}
}

func (m *model) viewArchiveInfo() chrome {
	c := chrome{
		title: "Archive contents",
		crumb: m.readCrumb("contents"),
		back:  kBack,
	}
	if m.intent == intentRestore {
		c.primary = confirm("restore")
	}
	if m.info == nil {
		return c
	}
	mf := m.info.Manifest

	var b strings.Builder
	rows := [][2]string{
		{"created", mf.CreatedAt.Local().Format("2006-01-02 15:04:05 MST")},
		{"host", fmt.Sprintf("%s (%s/%s)", mf.Host.Hostname, mf.Host.OS, mf.Host.Arch)},
		{"written by", mf.Tool + " " + mf.ToolVersion},
		{"pipeline", fmt.Sprintf("tar → %s (%s) → %s", m.info.Compressor, mf.Pipeline.Compression, m.info.Cipher)},
		{"encryption", mf.Pipeline.EncryptionMode},
	}
	if mf.Planned != nil {
		rows = append(rows, [2]string{"contents", fmt.Sprintf("%s, %s",
			cli.Count(mf.Planned.Files, "file", "files"), humanBytes(mf.Planned.Bytes))})
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %s %s\n", stMuted.Render(padRight(r[0]+":", 12)), r[1])
	}

	b.WriteString("\n" + stTitle.Render("Mapping") + "\n")
	for _, g := range mf.Groups {
		b.WriteString("  " + stEmph.Render(g.Name) + "\n")
		for _, src := range g.Sources {
			fmt.Fprintf(&b, "      %s %s %s\n",
				shorten(src.Path, m.inner()-20), stMuted.Render("→"), src.Member)
		}
	}

	// The header rows say what the archive is, which is the question this
	// screen exists to answer; the mapping is the part that can run to any
	// length, and so the part a short terminal gives up.
	c.body = fitBody(b.String(), m.bodyRows(c))
	return c
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func (m *model) viewRestoreTarget() chrome {
	c := chrome{
		title: "Where should it be restored?",
		crumb: "restore › target",
		subtitle: []string{
			"The archive already holds the mapped layout, so it is recreated under this directory.",
			"Nothing outside it is ever written, and existing files are never replaced.",
		},
		keys:    []binding{kComplete, kPick},
		primary: confirm("restore"),
		back:    kBack,
	}
	c.body = "  " + m.targetInput.View() + "\n\n" +
		m.targetInput.ViewList(m.width, m.pathListRows())
	return c
}

func (m *model) viewRestoring() chrome {
	// The manifest records what was planned, so a restore can measure itself
	// against the same kind of total a backup does.
	var ratio float64
	var total int64
	if m.info != nil && m.info.Manifest.Planned != nil {
		total = m.info.Manifest.Planned.Bytes
		if total > 0 {
			ratio = float64(m.lastProg.Bytes) / float64(total)
		}
	}
	return chrome{
		title:    "Restoring",
		crumb:    "restore › running",
		subtitle: []string{"Nothing outside the target is written, and existing files are kept."},
		body:     m.progressBody(ratio, total),
		busy:     true,
	}
}

func (m *model) viewRestoreDone() chrome {
	c := chrome{
		title:   "Restore complete",
		crumb:   "restore › done",
		primary: confirm("menu"),
	}
	if m.restored == nil {
		return c
	}

	var b strings.Builder
	b.WriteString("  " + stOK.Render(shorten(m.restored.Target, m.inner()-2)) + "\n\n")
	fmt.Fprintf(&b, "  %s · %s · %s\n",
		cli.Count(m.restored.Files, "file", "files"),
		cli.Count(m.restored.Dirs, "directory", "directories"),
		humanBytes(m.restored.Bytes))
	b.WriteString(m.viewWarnings(m.restored.Warnings))

	c.body = fitBody(b.String(), m.bodyRows(c))
	return c
}

func (m *model) viewGenerated() chrome {
	c := chrome{
		title:   "Generated",
		crumb:   "identity",
		body:    "  " + stPayload.Render(m.generated) + "\n\n",
		primary: confirm("menu"),
	}
	if m.genKind == genPassphrase {
		c.title = "Your new passphrase"
		c.crumb = "passphrase"
	} else {
		c.title = "Your new identity"
	}
	c.body += stNotice.Width(minInt(m.inner()-4, 84)).Render(cli.SecretNotice) + "\n"
	return c
}
