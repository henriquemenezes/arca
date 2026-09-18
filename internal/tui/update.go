package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hamsa/arca/internal/archive"
	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/secret"
)

func (m *model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While something is running, the only useful key is quit.
	if m.state == stateRunning || m.state == stateRestoring {
		return m, nil
	}

	switch m.state {
	case stateMenu:
		return m.keyMenu(msg)
	case stateSources:
		return m.keySources(msg)
	case stateMapping:
		return m.keyMapping(msg)
	case stateCrypto:
		return m.keyCrypto(msg)
	case statePassphrase:
		return m.keyPassphrase(msg)
	case stateRecipients:
		return m.keyText(msg, &m.rcptInput, m.acceptRecipients)
	case stateOutput:
		return m.keyPath(msg, &m.outInput, m.acceptOutput)
	case stateReview:
		return m.keyReview(msg)
	case stateSaveConfig:
		return m.keySaveConfig(msg)
	case stateDone, stateRestoreDone, stateGenerated:
		return m.keyFinished(msg)
	case statePickArchive:
		return m.keyPickArchive(msg)
	case stateArchiveKey:
		return m.keyText(msg, &m.passInput, m.acceptArchiveKey)
	case stateArchiveInfo:
		return m.keyArchiveInfo(msg)
	case stateRestoreTarget:
		return m.keyPath(msg, &m.targetInput, m.acceptRestoreTarget)
	}
	return m, nil
}

// ---------- menu ----------

func (m *model) keyMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		return m.chooseMenu()
	}
	var cmd tea.Cmd
	m.menu, cmd = m.menu.Update(msg)
	return m, cmd
}

func (m *model) chooseMenu() (tea.Model, tea.Cmd) {
	switch m.menu.Index() {
	case 0: // back up
		if m.hasConfig() {
			// Something already says what to capture — a file, or a session
			// spent editing one — so go straight to its review.
			m.push(stateReview)
			return m, m.refreshPlan()
		}
		m.intentCfg = cfgFresh
		m.push(stateSources)
	case 1: // restore
		m.intent = intentRestore
		m.browser = newBrowser(defaultOutputDir())
		m.push(statePickArchive)
	case 2: // inspect
		m.intent = intentInspect
		m.browser = newBrowser(defaultOutputDir())
		m.push(statePickArchive)
	case 3: // identity
		return m, m.generateIdentity()
	case 4: // passphrase
		return m, m.generatePassphrase()
	case 5:
		return m, tea.Quit
	}
	return m, nil
}

// ---------- backup: source selection ----------

func (m *model) keySources(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pop()
		return m, nil
	case "q":
		return m, tea.Quit
	case "tab", "ctrl+d":
		sel := m.browser.Selected()
		if len(sel) == 0 {
			m.fail(errors.New("nothing selected yet: highlight a file or directory and press space"))
			return m, nil
		}
		m.mapping = rebuildMapping(m.mapping, sel)
		m.mapIndex = 0
		m.push(stateMapping)
		return m, nil
	}
	m.browser.Update(msg)
	return m, nil
}

// rebuildMapping reconciles the browser's selection with the mapping already in
// hand: a path that was there keeps the destination it was given, and only a
// genuinely new one is guessed at.
//
// Keeping them matters twice over. It is what lets an edit start from what the
// configuration file says, and it is what stops a trip back to the browser from
// resetting every destination the user typed on the way forward.
func rebuildMapping(prev []mapEntry, selected []string) []mapEntry {
	known := make(map[string]mapEntry, len(prev))
	for _, e := range prev {
		known[e.path] = e
	}
	// The browser hands back absolute paths; a configuration file may spell the
	// same place with a "~/". Both have to find each other, and the spelling
	// already in hand is the one worth keeping.
	byAbs := make(map[string]mapEntry, len(prev))
	for _, e := range prev {
		if abs, err := config.ExpandPath(e.path); err == nil {
			byAbs[abs] = e
		}
	}

	out := make([]mapEntry, 0, len(selected))
	for _, p := range selected {
		if e, ok := known[p]; ok {
			out = append(out, e)
			continue
		}
		if e, ok := byAbs[p]; ok {
			out = append(out, e)
			continue
		}
		out = append(out, mapEntry{path: p, dest: suggestDest(p)})
	}
	return out
}

// suggestDest proposes a destination that matches how people usually group
// things: dotfiles together, everything else under its own name.
func suggestDest(path string) string {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") {
		return "dotfiles"
	}
	return base
}

// ---------- backup: mapping ----------

func (m *model) keyMapping(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editing {
		switch msg.String() {
		case "enter":
			m.mapping[m.mapIndex].dest = strings.TrimSpace(m.destInput.Value())
			m.editing = false
			m.destInput.Blur()
			return m, nil
		case "esc":
			m.editing = false
			m.destInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.destInput, cmd = m.destInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		m.pop()
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.mapIndex > 0 {
			m.mapIndex--
		}
	case "down", "j":
		if m.mapIndex < len(m.mapping)-1 {
			m.mapIndex++
		}
	case "d":
		if len(m.mapping) > 1 {
			m.mapping = append(m.mapping[:m.mapIndex], m.mapping[m.mapIndex+1:]...)
			if m.mapIndex >= len(m.mapping) {
				m.mapIndex = len(m.mapping) - 1
			}
		}
	case "e", "enter":
		m.editing = true
		m.destInput.SetValue(m.mapping[m.mapIndex].dest)
		m.destInput.CursorEnd()
		return m, m.destInput.Focus()
	case "tab", "ctrl+d":
		cfg, err := m.buildConfig()
		if err != nil {
			m.fail(err)
			return m, nil
		}
		// The rendered text is what the archive manifest records, so it has to
		// describe the configuration that actually produced the archive.
		// Whatever file this started from, it no longer says this. cfgPath is
		// left alone: it is where the configuration came from, and so the
		// obvious place to offer to write it back to.
		m.cfg, m.cfgTOML, m.fromDisk = cfg, m.renderTOML(cfg), false
		if m.intentCfg == cfgFresh {
			m.push(stateCrypto)
			return m, nil
		}
		return m, m.toSaveConfig()
	}
	return m, nil
}

// ---------- backup: encryption ----------

func (m *model) keyCrypto(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pop()
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.cryptoIndex = (m.cryptoIndex - 1 + len(cryptoItems)) % len(cryptoItems)
	case "down", "j":
		m.cryptoIndex = (m.cryptoIndex + 1) % len(cryptoItems)
	case "enter":
		switch m.cryptoIndex {
		case 0:
			p, err := secret.Generate(secret.DefaultWords)
			if err != nil {
				m.fail(err)
				return m, nil
			}
			m.genPass = p
			m.backupKey = &codec.Keyring{Passphrase: []byte(p)}
			m.cfg.Encryption.Recipients = nil
			return m, m.toOutput()
		case 1:
			m.passStage = 0
			m.passInput.SetValue("")
			m.confirmPass.SetValue("")
			m.push(statePassphrase)
			return m, m.passInput.Focus()
		case 2:
			m.rcptInput.SetValue(strings.Join(m.cfg.Encryption.Recipients, " "))
			m.push(stateRecipients)
			return m, m.rcptInput.Focus()
		}
	}
	return m, nil
}

func (m *model) keyPassphrase(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pop()
		return m, nil
	case "enter":
		if m.passStage == 0 {
			if err := secret.CheckStrength(m.passInput.Value()); err != nil {
				m.fail(err)
				return m, nil
			}
			m.err = nil
			m.passStage = 1
			m.passInput.Blur()
			return m, m.confirmPass.Focus()
		}
		if m.passInput.Value() != m.confirmPass.Value() {
			m.fail(errors.New("the two entries do not match"))
			m.confirmPass.SetValue("")
			return m, nil
		}
		m.genPass = ""
		m.backupKey = &codec.Keyring{Passphrase: []byte(m.passInput.Value())}
		m.cfg.Encryption.Recipients = nil
		m.confirmPass.Blur()
		return m, m.toOutput()
	}

	var cmd tea.Cmd
	if m.passStage == 0 {
		m.passInput, cmd = m.passInput.Update(msg)
	} else {
		m.confirmPass, cmd = m.confirmPass.Update(msg)
	}
	return m, cmd
}

func (m *model) acceptRecipients() (tea.Model, tea.Cmd) {
	rcpts := splitRecipients(m.rcptInput.Value())
	if len(rcpts) == 0 {
		m.fail(errors.New("no recipients given; press esc to choose a passphrase instead"))
		return m, nil
	}
	cipher, err := codec.GetCipher(m.cfg.Settings.Cipher)
	if err != nil {
		m.fail(err)
		return m, nil
	}
	ring := &codec.Keyring{Recipients: rcpts}
	if err := cipher.Validate(ring); err != nil {
		m.fail(err)
		return m, nil
	}
	if len(rcpts) == 1 {
		m.notice = "Only one recipient. Add a recovery key kept offline, or losing this key loses the archive."
	}
	m.cfg.Encryption.Recipients = rcpts
	m.backupKey = ring
	m.genPass = ""
	m.rcptInput.Blur()
	return m, m.toOutput()
}

// ---------- backup: output and review ----------

func (m *model) toOutput() tea.Cmd {
	if m.outInput.Value() == "" {
		m.outInput.SetValue(defaultOutputDir())
	}
	m.outInput.CursorEnd()
	m.push(stateOutput)
	return m.outInput.Focus()
}

func (m *model) acceptOutput() (tea.Model, tea.Cmd) {
	dir := expandHome(strings.TrimSpace(m.outInput.Value()))
	if dir == "" {
		dir = defaultOutputDir()
	}
	comp, err := codec.GetCompressor(m.cfg.Settings.Compressor)
	if err != nil {
		m.fail(err)
		return m, nil
	}
	ciph, err := codec.GetCipher(m.cfg.Settings.Cipher)
	if err != nil {
		m.fail(err)
		return m, nil
	}
	path, err := cli.ResolveOutputPath(ensureDirSuffix(dir), timeNow(), comp, ciph)
	if err != nil {
		m.fail(err)
		return m, nil
	}
	m.outPath = path
	m.outInput.Blur()
	m.push(stateReview)
	return m, m.refreshPlan()
}

// ensureDirSuffix keeps a path that names a directory from being taken as the
// archive's own file name.
func ensureDirSuffix(p string) string {
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return p + string(filepath.Separator)
	}
	if strings.HasSuffix(p, string(filepath.Separator)) {
		return p
	}
	if filepath.Ext(p) == "" {
		return p + string(filepath.Separator)
	}
	return p
}

// refreshPlan runs the counting pass that feeds both the review screen and the
// progress percentage.
func (m *model) refreshPlan() tea.Cmd {
	skip := map[string]bool{}
	if m.outPath != "" {
		skip[m.outPath] = true
		skip[m.outPath+archive.PartialSuffix] = true
	}
	plan, err := archive.Plan(m.cfg, skip)
	if err != nil {
		m.fail(err)
		return nil
	}
	m.plan = plan
	return nil
}

func (m *model) keyReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pop()
	case "q":
		return m, tea.Quit
	case "e":
		return m, m.editConfig()
	case "n":
		return m, m.newConfig()
	case "enter":
		if m.plan == nil {
			m.fail(errors.New("nothing to back up"))
			return m, nil
		}
		if m.backupKey == nil {
			// Reached from a config on disk, so encryption has not been chosen.
			if len(m.cfg.Encryption.Recipients) > 0 {
				m.backupKey = &codec.Keyring{Recipients: m.cfg.Encryption.Recipients}
			} else {
				m.push(stateCrypto)
				return m, nil
			}
		}
		if m.outPath == "" {
			return m, m.toOutput()
		}
		m.state = stateRunning
		m.lastProg = archive.Progress{}
		return m, tea.Batch(
			startBackup(archive.BackupOptions{
				Config:      m.cfg,
				ConfigTOML:  m.cfgTOML,
				Keyring:     m.backupKey,
				OutputPath:  m.outPath,
				ToolVersion: cli.Version,
				Planned:     &m.plan.Stats,
			}, m.msgs),
			waitFor(m.msgs),
		)
	}
	return m, nil
}

// editConfig reopens the guided flow on the configuration under review, with
// the browser already marking what it captures and the mapping screen already
// holding each destination. Everything the flow never asks about is carried on
// m.base, so what comes back out is the same configuration minus the changes.
func (m *model) editConfig() tea.Cmd {
	if m.cfg == nil {
		m.fail(errors.New("there is no configuration to edit"))
		return nil
	}
	m.intentCfg = cfgEdit
	m.adoptConfig(m.cfg)
	m.seedMapping(m.cfg)

	home, _ := os.UserHomeDir()
	m.browser = newBrowser(home)
	for _, e := range m.mapping {
		abs, err := config.ExpandPath(e.path)
		if err != nil {
			continue
		}
		// A source that has since been deleted still belongs to the
		// configuration; it is the plan's job to report it missing, not this
		// screen's job to drop it.
		m.browser.selected[abs] = true
	}
	m.push(stateSources)
	return nil
}

// newConfig starts over: nothing selected, nothing inherited, and the built-in
// defaults underneath.
func (m *model) newConfig() tea.Cmd {
	m.intentCfg = cfgNew
	m.adoptConfig(config.Default())
	m.mapping = m.mapping[:0]
	m.mapIndex = 0

	home, _ := os.UserHomeDir()
	m.browser = newBrowser(home)
	m.push(stateSources)
	return nil
}

// ---------- backup: saving the configuration ----------

// saveTarget is where a configuration would be written if it were saved now:
// back over the file it came from, or arca's own directory when it has none.
func (m *model) saveTarget() string {
	if m.cfgPath != "" {
		return m.cfgPath
	}
	path, err := cli.ResolveConfigPath("")
	if err != nil {
		return cli.ConfigFileName
	}
	return path
}

func (m *model) toSaveConfig() tea.Cmd {
	m.cfgInput.SetValue(m.saveTarget())
	m.cfgInput.CursorEnd()
	m.confirmSave = ""
	m.push(stateSaveConfig)
	return m.cfgInput.Focus()
}

// keySaveConfig is keyPath's shape with one key more. Writing over a file the
// user wrote by hand is worth two presses of enter, and skipping the write
// entirely has to be reachable, since a configuration is worth trying before it
// is worth keeping.
func (m *model) keySaveConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cfgInput.Blur()
		m.pop()
		return m, nil

	case "ctrl+d":
		// Used for this run only. Nothing on disk is touched, and fromDisk is
		// already false, so nothing claims a file backs what is about to run.
		m.cfgInput.Blur()
		m.notice = "Not saved — this configuration is used for this run only."
		return m, m.returnToReview()

	case "enter":
		path, err := cli.ResolveConfigPath(strings.TrimSpace(m.cfgInput.Value()))
		if err != nil {
			m.fail(err)
			return m, nil
		}
		if _, statErr := os.Stat(path); statErr == nil && m.confirmSave != path {
			m.confirmSave = path
			m.err = nil
			return m, nil
		}
		if err := m.writeConfig(path, true); err != nil {
			m.fail(err)
			return m, nil
		}
		m.cfgInput.Blur()
		return m, m.returnToReview()
	}

	return m, m.cfgInput.Update(msg)
}

// ---------- finished screens ----------

func (m *model) keyFinished(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "enter":
		m.state = stateMenu
		m.back = nil
		m.err = nil
		m.notice = ""
		m.endRun()
		return m, nil
	case "s":
		if m.state == stateDone && !m.fromDisk {
			path, err := cli.ResolveConfigPath("")
			if err != nil {
				m.fail(err)
				return m, nil
			}
			// This screen has nowhere to ask, so it never replaces anything.
			if err := m.writeConfig(path, false); err != nil {
				m.fail(err)
			}
		}
	}
	return m, nil
}

// writeConfig renders the configuration in hand to path. replace is the
// caller's answer to a file already being there: the save screen asks the user
// and says yes, the done screen has no room to ask and says no.
func (m *model) writeConfig(path string, replace bool) error {
	if !replace {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; not replacing it", path)
		}
	}
	// 0700 on the directory: it is shared with identity.age.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := m.renderTOML(m.cfg)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	m.cfgTOML, m.cfgPath, m.fromDisk = body, path, true
	m.confirmSave = ""
	m.notice = "Saved " + path + " — next time `arca backup` reuses it."
	return nil
}

// ---------- reading an archive ----------

func (m *model) keyPickArchive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pop()
		return m, nil
	case "q":
		return m, tea.Quit
	case "enter":
		if e, ok := m.browser.current(); ok && !e.isDir {
			m.archivePath = e.path
			m.passInput.SetValue("")
			m.push(stateArchiveKey)
			return m, m.passInput.Focus()
		}
	}
	m.browser.Update(msg)
	return m, nil
}

// acceptArchiveKey opens the archive, trying an identity file when the input
// looks like a path rather than a passphrase.
func (m *model) acceptArchiveKey() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.passInput.Value())
	if value == "" {
		m.fail(errors.New("a passphrase or an identity file path is required"))
		return m, nil
	}

	var ring *codec.Keyring
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		ring = &codec.Keyring{IdentityFiles: []string{value}}
	} else {
		ring = &codec.Keyring{Passphrase: []byte(value)}
	}

	info, err := archive.Inspect(m.archivePath, ring)
	if err != nil {
		m.fail(err)
		return m, nil
	}
	m.archiveKey, m.info = ring, info
	m.passInput.Blur()
	m.push(stateArchiveInfo)
	return m, nil
}

func (m *model) keyArchiveInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pop()
	case "q":
		return m, tea.Quit
	case "enter":
		if m.intent != intentRestore {
			return m, nil
		}
		if m.targetInput.Value() == "" {
			m.targetInput.SetValue(filepath.Join(defaultOutputDir(), "restored"))
		}
		m.targetInput.CursorEnd()
		m.push(stateRestoreTarget)
		return m, m.targetInput.Focus()
	}
	return m, nil
}

func (m *model) acceptRestoreTarget() (tea.Model, tea.Cmd) {
	target := expandHome(strings.TrimSpace(m.targetInput.Value()))
	if target == "" {
		m.fail(errors.New("a target directory is required"))
		return m, nil
	}
	m.targetInput.Blur()
	m.state = stateRestoring
	m.lastProg = archive.Progress{}

	return m, tea.Batch(
		startRestore(archive.RestoreOptions{
			Path:    m.archivePath,
			Keyring: m.archiveKey,
			Target:  target,
		}, m.msgs),
		waitFor(m.msgs),
	)
}

// ---------- generators ----------

func (m *model) generateIdentity() tea.Cmd {
	path := cli.DefaultIdentityPath()
	if path == "" {
		m.fail(errors.New("cannot locate your home directory for the identity"))
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		m.fail(fmt.Errorf("%s already exists: replacing an identity would make every archive "+
			"encrypted to it unreadable", path))
		return nil
	}
	recipient, err := writeIdentity(path)
	if err != nil {
		m.fail(err)
		return nil
	}
	m.generated = recipient
	m.notice = "Identity written to " + path + " (mode 0600). The line below is the public recipient."
	m.push(stateGenerated)
	return nil
}

func (m *model) generatePassphrase() tea.Cmd {
	p, err := secret.Generate(secret.DefaultWords)
	if err != nil {
		m.fail(err)
		return nil
	}
	m.generated = p
	m.notice = fmt.Sprintf("%d words from a list of %d — about %.0f bits of entropy.",
		secret.DefaultWords, len(secret.Words()), secret.EntropyBits(secret.DefaultWords))
	m.push(stateGenerated)
	return nil
}

// ---------- generic text input screens ----------

func (m *model) keyText(msg tea.KeyMsg, ti *textinput.Model, accept func() (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		ti.Blur()
		m.pop()
		return m, nil
	case "enter":
		return accept()
	}
	var cmd tea.Cmd
	*ti, cmd = ti.Update(msg)
	return m, cmd
}

// keyPath is keyText's sibling for the two screens that take a filesystem
// path. Accepting and going back work the same; what differs is everything in
// between, because a pathInput claims tab and the arrows for completion before
// the text field sees them.
func (m *model) keyPath(msg tea.KeyMsg, pi *pathInput, accept func() (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		pi.Blur()
		m.pop()
		return m, nil
	case "enter":
		return accept()
	}
	return m, pi.Update(msg)
}
