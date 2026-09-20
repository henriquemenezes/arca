package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/secret"
)

func timeNow() time.Time { return time.Now() }

func writeIdentity(path string) (string, error) { return cli.WriteIdentity(path) }

// ---------- menu ----------

func (m *model) viewMenu() string {
	var head strings.Builder
	switch opts := cli.AutoBannerOpts(""); {
	case m.height >= minHeightForMark:
		head.WriteString(cli.Mark(opts) + "\n\n")
	case m.height >= minHeightForArk:
		head.WriteString(cli.Ark(opts) + "\n\n")
	}
	head.WriteString(header("What would you like to do?", "menu"))

	var foot strings.Builder
	switch {
	case m.fromDisk:
		foot.WriteString("\n" + stMuted.Render("Using "+m.cfgPath+" — backup will start from its review screen.") + "\n")
	case m.hasConfig():
		foot.WriteString("\n" + stMuted.Render("Using the configuration built in this session, which is not saved.") + "\n")
	default:
		foot.WriteString("\n" + stMuted.Render("No arca.toml found; you will pick what to back up.") + "\n")
	}
	if m.notice != "" {
		foot.WriteString(stOK.Render("✓ "+m.notice) + "\n")
	}
	foot.WriteString(m.help("↑↓ move", "enter choose", "q quit"))

	m.fitMenu(head.String(), foot.String())
	return head.String() + m.menu.View() + foot.String()
}

// fitMenu hands the list the rows the rest of the screen leaves it. When they
// are too few to carry a blurb under every item the blurbs go, the same trade
// the artwork above makes; when even the bare titles do not fit, the list
// paginates rather than running off the bottom.
func (m *model) fitMenu(head, foot string) {
	// The screen is padded by one row top and bottom; the rest of the budget
	// is whatever head and foot already spend.
	rows := m.height - 2 - strings.Count(head, "\n") - strings.Count(foot, "\n")

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
func (m *model) viewSources() string {
	var b strings.Builder
	b.WriteString(header("Choose what to back up", "backup › sources"))

	browserWidth, previewWidth := m.sourcesLayout()
	rows := maxInt(3, m.browser.height)

	// A terminal too narrow for two columns still has to be able to edit the
	// patterns, so there the panel takes the whole width while it has focus and
	// the file list stands down.
	if previewWidth == 0 {
		if m.editingExcludes() {
			b.WriteString(m.viewExcludes(browserWidth-2, rows) + "\n")
			return b.String() + m.sourcesHelp()
		}
		b.WriteString(strings.TrimRight(m.browser.View(browserWidth, m.excluded), "\n") + "\n")
		b.WriteString("\n" + stMuted.Render(m.selectionLine()) + "\n")
		return b.String() + m.sourcesHelp()
	}

	left := strings.TrimRight(m.browser.View(browserWidth, m.excluded), "\n")

	// The panel spends the same rows the file list does, so the two columns
	// start and end together however tall the terminal is.
	// The panel's width is what it occupies inside its border, and its padding
	// comes out of that; the text is laid out against what is left.
	inner := previewWidth - 4
	body := m.viewSelectionPreview(inner, rows)
	border := colFaint
	if m.editingExcludes() {
		body = m.viewExcludes(inner, rows)
		// The border is the only thing that says which column the keys are
		// going to, and on this screen that changes.
		border = colAccent
	}
	right := stPanel.MarginLeft(previewGap).BorderForeground(border).
		Width(previewWidth - 2).Height(rows).Render(body)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n")
	return b.String() + m.sourcesHelp()
}

// editingExcludes says the excludes panel has the keyboard.
func (m *model) editingExcludes() bool {
	return m.mode == srcExcludes || m.mode == srcExcludeInput
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

// sourcesHelp lists the keys the screen is currently listening for. Each mode
// takes the keyboard whole, so showing all of them at once would name keys that
// do nothing.
func (m *model) sourcesHelp() string {
	switch m.mode {
	case srcPath:
		return m.help("type a directory", "tab complete", "↑↓ candidates", "enter go",
			"esc file list")
	case srcFilter:
		// What to type is on the line above, in the field itself, so the help
		// is only keys here.
		return m.help("↑↓ move", "tab complete", "space select", "ctrl+x exclude",
			"enter open", "ctrl+u clear", "ctrl+d continue", "esc leave search")
	case srcExcludes:
		return m.help("↑↓ move", "a add pattern", "d remove", "esc back")
	case srcExcludeInput:
		return m.help("enter add", "esc cancel")
	}
	return m.help("↑↓ move", "→ open", "← up", "space select", "d directory",
		"h home", "x exclude", "X excludes", "/ find", ". hidden", "enter continue",
		"esc back")
}

func (m *model) viewMapping() string {
	var b strings.Builder
	b.WriteString(header("Where should each one land inside the archive?", "backup › mapping"))
	b.WriteString(stMuted.Render("The destination is a directory in the archive; each source keeps its own name under it.") + "\n\n")

	for i, e := range m.mapping {
		cursor := "  "
		if i == m.mapIndex {
			cursor = stSelected.Render("▸ ")
		}
		dest := e.dest
		if dest == "" {
			dest = stCrumb.Render("(archive root)")
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, shorten(e.path, m.width-8)))
		b.WriteString(fmt.Sprintf("      %s %s   %s %s\n",
			stMuted.Render("into"), dest, stMuted.Render("→"), stKey.Render(e.member())))
	}

	if m.editing {
		b.WriteString("\n" + stMuted.Render("Destination directory:") + "\n  " + m.destInput.View() + "\n")
		return b.String() + m.help("enter accept", "esc cancel")
	}
	return b.String() + m.help("↑↓ move", "e edit destination", "d remove",
		"enter continue", "esc back")
}

func (m *model) viewCrypto() string {
	var b strings.Builder
	b.WriteString(header("How should the archive be encrypted?", "backup › encryption"))
	b.WriteString(stMuted.Render("age does not allow both at once: a passphrase must be the only way in,") + "\n")
	b.WriteString(stMuted.Render("otherwise the archive would fall back to the strength of that passphrase.") + "\n\n")

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
	return b.String() + m.help("↑↓ move", "enter choose", "esc back")
}

func (m *model) viewPassphrase() string {
	var b strings.Builder
	b.WriteString(header("Type a passphrase", "backup › encryption › passphrase"))

	if m.passStage == 0 {
		b.WriteString("  " + m.passInput.View() + "\n\n")
		b.WriteString(strengthMeter(m.passInput.Value(), 30))
	} else {
		b.WriteString("  " + stMuted.Render("passphrase accepted") + "\n\n")
		b.WriteString("  " + m.confirmPass.View() + "\n")
		b.WriteString("\n" + stMuted.Render("Type it once more. A typo here is unrecoverable.") + "\n")
	}
	return b.String() + m.help("enter continue", "esc back")
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

func (m *model) viewRecipients() string {
	var b strings.Builder
	b.WriteString(header("Encrypt to age recipients", "backup › encryption › recipients"))
	b.WriteString(stMuted.Render("Any one of these keys can open the archive.") + "\n")
	b.WriteString(stMuted.Render("List two: the key you use day to day, and a recovery key kept offline.") + "\n\n")
	b.WriteString("  " + m.rcptInput.View() + "\n")
	return b.String() + m.help("enter continue", "esc back")
}

func (m *model) viewOutput() string {
	var b strings.Builder
	b.WriteString(header("Where should the archive be written?", "backup › destination"))
	b.WriteString(stMuted.Render("A directory gets a generated name; a file path is used as given.") + "\n\n")
	b.WriteString("  " + m.outInput.View() + "\n\n")
	if list := m.outInput.ViewList(m.width, m.pathListRows()); list != "" {
		b.WriteString(list + "\n")
	}
	if m.cfg != nil {
		b.WriteString(stMuted.Render("  Name: "+m.archiveName()) + "\n")
	}
	return b.String() + m.help("tab complete", "↑↓ pick", "enter continue", "esc back")
}

func (m *model) viewReview() string {
	var b strings.Builder
	b.WriteString(header("Review", "backup › review"))

	// A plan that could not be built is the moment the configuration most needs
	// changing — a source was renamed, or deleted — so the two ways to change it
	// have to be reachable from here too.
	if m.plan == nil {
		b.WriteString(m.viewConfigSummary())
		b.WriteString(stMuted.Render("  Nothing planned yet.") + "\n")
		return b.String() + m.help("e edit this configuration", "n new configuration",
			"esc back", "q quit")
	}

	b.WriteString(m.viewConfigSummary())

	b.WriteString(stTitle.Render("Mapping") + "\n")
	for _, s := range m.plan.Sources {
		b.WriteString(fmt.Sprintf("  %s\n      %s %s  %s\n",
			shorten(s.Path, m.width-8),
			stMuted.Render("→"), stKey.Render(s.Member),
			stMuted.Render(fmt.Sprintf("%s, %s", cli.Count(s.Stats.Files, "file", "files"), humanBytes(s.Stats.Bytes)))))
	}

	b.WriteString("\n" + stTitle.Render("Totals") + "\n")
	b.WriteString(fmt.Sprintf("  %s · %s · %s\n",
		cli.Count(m.plan.Stats.Files, "file", "files"),
		cli.Count(m.plan.Stats.Dirs, "directory", "directories"),
		humanBytes(m.plan.Stats.Bytes)))

	b.WriteString("\n" + stTitle.Render("Pipeline") + "\n")
	enc := "passphrase"
	if n := len(m.cfg.Encryption.Recipients); n > 0 {
		enc = cli.Count(n, "recipient", "recipients")
	}
	b.WriteString(fmt.Sprintf("  tar → %s (%s) → %s, %s\n",
		m.cfg.Settings.Compressor, m.cfg.Settings.Compression, m.cfg.Settings.Cipher, enc))
	if m.outPath != "" {
		b.WriteString("  " + stMuted.Render(shorten(m.outPath, m.width-6)) + "\n")
	}

	if len(m.plan.Warnings) > 0 {
		b.WriteString("\n" + stWarn.Render(cli.Count(len(m.plan.Warnings), "warning", "warnings")+":") + "\n")
		for i, w := range m.plan.Warnings {
			if i == 5 {
				b.WriteString(stMuted.Render(fmt.Sprintf("  … and %d more", len(m.plan.Warnings)-5)) + "\n")
				break
			}
			b.WriteString("  " + stWarn.Render("!") + " " + shorten(w, m.width-6) + "\n")
		}
	}
	if m.notice != "" {
		b.WriteString("\n" + stWarn.Render("! "+m.notice) + "\n")
	}
	return b.String() + m.help("enter start the backup", "e edit this configuration",
		"n new configuration", "esc back", "q quit")
}

// viewConfigSummary names the configuration under review and what each of its
// groups holds. Without it the review shows what would happen but never which
// file said so, and "edit this configuration" would be an offer to change
// something the screen never identified.
func (m *model) viewConfigSummary() string {
	if m.cfg == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(stTitle.Render("Configuration") + "\n")
	if m.fromDisk && m.cfgPath != "" {
		b.WriteString("  " + stKey.Render(shorten(m.cfgPath, m.width-6)) + "\n")
	} else {
		b.WriteString("  " + stMuted.Render("built in this session, not saved") + "\n")
	}
	for _, g := range m.cfg.Groups {
		line := fmt.Sprintf("  %s %s", stKey.Render(padRight(groupLabel(g.Name), 14)),
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

func (m *model) viewSaveConfig() string {
	var b strings.Builder
	b.WriteString(header("Save this configuration?", "backup › configuration"))
	b.WriteString("  " + m.cfgInput.View() + "\n\n")
	if list := m.cfgInput.ViewList(m.width, m.pathListRows()); list != "" {
		b.WriteString(list + "\n")
	}

	path, err := cli.ResolveConfigPath(strings.TrimSpace(m.cfgInput.Value()))
	if err == nil {
		if _, statErr := os.Stat(path); statErr == nil {
			if m.confirmSave == path {
				b.WriteString(stWarn.Render("  Press enter again to replace "+shorten(path, m.width-30)) + "\n")
			} else {
				b.WriteString(stWarn.Render("  A configuration is already there; saving replaces it.") + "\n")
			}
			// The shipped arca.toml is mostly comments, and they are the one
			// thing a round trip through this interface cannot carry.
			b.WriteString(stMuted.Render("  Comments in the existing file are not carried over.") + "\n")
		}
	}

	b.WriteString("\n" + stTitle.Render("What would be written") + "\n")
	b.WriteString(m.previewTOML())
	return b.String() + m.help("tab complete", "↑↓ pick", "enter save",
		"ctrl+d continue without saving", "esc back")
}

// previewTOML shows as much of the rendered file as the terminal has room for.
// The point is to make "replaces it" concrete before the second enter, so what
// matters is the top of the file, not all of it.
func (m *model) previewTOML() string {
	if m.cfg == nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(m.renderTOML(m.cfg), "\n"), "\n")
	rows := maxInt(3, m.height-20)

	var b strings.Builder
	for i, line := range lines {
		if i == rows {
			b.WriteString("    " + stCrumb.Render(fmt.Sprintf("+%d more", len(lines)-rows)) + "\n")
			break
		}
		b.WriteString("    " + stMuted.Render(shorten(line, m.width-8)) + "\n")
	}
	return b.String()
}

func (m *model) viewRunning() string {
	var b strings.Builder
	b.WriteString(header("Writing the archive", "backup › running"))

	var ratio float64
	if m.plan != nil && m.plan.Stats.Bytes > 0 {
		ratio = float64(m.lastProg.Bytes) / float64(m.plan.Stats.Bytes)
	}
	b.WriteString("  " + m.bar.ViewAs(minFloat(ratio, 1)) + "\n\n")
	b.WriteString(fmt.Sprintf("  %s of %s · %s\n",
		humanBytes(m.lastProg.Bytes),
		humanBytes(planBytes(m)),
		cli.Count(m.lastProg.Files, "file", "files")))
	b.WriteString("  " + stMuted.Render(shorten(m.lastProg.Member, m.width-6)) + "\n")
	b.WriteString("\n  " + stMuted.Render("Encrypted on the way out; nothing is written in the clear.") + "\n")
	return b.String() + m.help("ctrl+c abort")
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

func (m *model) viewDone() string {
	var b strings.Builder
	b.WriteString(header("Backup complete", "backup › done"))
	if m.result == nil {
		return b.String() + m.help("enter menu")
	}

	b.WriteString("  " + stOK.Render(shorten(m.result.Path, m.width-6)) + "\n\n")
	b.WriteString(fmt.Sprintf("  %s · %s from %s of sources\n\n",
		cli.Count(m.result.Stats.Files, "file", "files"),
		humanBytes(m.result.ArchiveBytes), humanBytes(m.result.Stats.Bytes)))

	if len(m.result.Warnings) > 0 {
		b.WriteString(stWarn.Render(cli.Count(len(m.result.Warnings), "warning", "warnings")+
			" recorded in the archive summary") + "\n\n")
	}

	b.WriteString(stTitle.Render("Restore without arca, on any Unix machine") + "\n")
	b.WriteString("  " + stKey.Render(cli.ManualRestoreCommand(m.result.Path, m.cfg)) + "\n\n")

	notice := cli.SecretNotice
	if m.genPass != "" {
		notice = "Your passphrase — save it now, it is shown once:\n\n    " + m.genPass + "\n\n" + cli.SecretNotice
	}
	b.WriteString(stNotice.Width(minInt(m.width-8, 84)).Render(notice) + "\n")

	if m.notice != "" {
		b.WriteString(stOK.Render("✓ "+m.notice) + "\n")
	}
	if !m.fromDisk {
		return b.String() + m.help("s save this selection to ~/.arca/arca.toml", "enter menu", "q quit")
	}
	return b.String() + m.help("enter menu", "q quit")
}

// ---------- reading an archive ----------

func (m *model) viewPickArchive() string {
	title := "Choose an archive to inspect"
	if m.intent == intentRestore {
		title = "Choose an archive to restore"
	}
	var b strings.Builder
	b.WriteString(header(title, "archive › open"))
	b.WriteString(m.browser.View(m.width-6, nil))
	return b.String() + m.help("↑↓ move", "→ open", "← up", "enter choose", ". hidden",
		"h home", "esc back")
}

func (m *model) viewArchiveKey() string {
	var b strings.Builder
	b.WriteString(header("Unlock the archive", "archive › key"))
	b.WriteString("  " + stMuted.Render(shorten(m.archivePath, m.width-6)) + "\n\n")
	b.WriteString("  " + m.passInput.View() + "\n\n")
	b.WriteString(stMuted.Render("  A passphrase, or the path to an age identity file.") + "\n")
	return b.String() + m.help("enter unlock", "esc back")
}

func (m *model) viewArchiveInfo() string {
	var b strings.Builder
	b.WriteString(header("Archive contents", "archive › details"))
	if m.info == nil {
		return b.String() + m.help("esc back")
	}
	mf := m.info.Manifest

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
		b.WriteString(fmt.Sprintf("  %s %s\n", stMuted.Render(padRight(r[0]+":", 12)), r[1]))
	}

	b.WriteString("\n" + stTitle.Render("Mapping") + "\n")
	for _, g := range mf.Groups {
		b.WriteString("  " + stKey.Render(g.Name) + "\n")
		for _, s := range g.Sources {
			b.WriteString(fmt.Sprintf("      %s %s %s\n",
				shorten(s.Path, m.width-24), stMuted.Render("→"), s.Member))
		}
	}

	if m.intent == intentRestore {
		return b.String() + m.help("enter choose a target and restore", "esc back", "q quit")
	}
	return b.String() + m.help("esc back", "q quit")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func (m *model) viewRestoreTarget() string {
	var b strings.Builder
	b.WriteString(header("Where should it be restored?", "restore › target"))
	b.WriteString(stMuted.Render("The archive already holds the mapped layout, so it is recreated under this directory.") + "\n")
	b.WriteString(stMuted.Render("Nothing outside it is ever written, and existing files are never replaced.") + "\n\n")
	b.WriteString("  " + m.targetInput.View() + "\n\n")
	b.WriteString(m.targetInput.ViewList(m.width, m.pathListRows()))
	return b.String() + m.help("tab complete", "↑↓ pick", "enter restore", "esc back")
}

func (m *model) viewRestoring() string {
	var b strings.Builder
	b.WriteString(header("Restoring", "restore › running"))
	b.WriteString(fmt.Sprintf("  %s · %s\n",
		cli.Count(m.lastProg.Files, "file", "files"), humanBytes(m.lastProg.Bytes)))
	b.WriteString("  " + stMuted.Render(shorten(m.lastProg.Member, m.width-6)) + "\n")
	return b.String() + m.help("ctrl+c abort")
}

func (m *model) viewRestoreDone() string {
	var b strings.Builder
	b.WriteString(header("Restore complete", "restore › done"))
	if m.restored == nil {
		return b.String() + m.help("enter menu")
	}
	b.WriteString("  " + stOK.Render(m.restored.Target) + "\n\n")
	b.WriteString(fmt.Sprintf("  %s · %s · %s\n",
		cli.Count(m.restored.Files, "file", "files"),
		cli.Count(m.restored.Dirs, "directory", "directories"),
		humanBytes(m.restored.Bytes)))
	if len(m.restored.Warnings) > 0 {
		b.WriteString("\n" + stWarn.Render(cli.Count(len(m.restored.Warnings), "warning", "warnings")+":") + "\n")
		for _, w := range m.restored.Warnings {
			b.WriteString("  " + stWarn.Render("!") + " " + shorten(w, m.width-6) + "\n")
		}
	}
	return b.String() + m.help("enter menu", "q quit")
}

func (m *model) viewGenerated() string {
	var b strings.Builder
	b.WriteString(header("Generated", "keys"))
	b.WriteString("\n    " + stKey.Render(m.generated) + "\n\n")
	if m.notice != "" {
		b.WriteString("  " + stMuted.Render(m.notice) + "\n\n")
	}
	b.WriteString(stNotice.Width(minInt(m.width-8, 84)).Render(cli.SecretNotice) + "\n")
	return b.String() + m.help("enter menu", "q quit")
}
