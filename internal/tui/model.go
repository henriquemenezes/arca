package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/hamsa/arca/internal/archive"
	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/config"
)

type state int

const (
	stateMenu state = iota
	stateSources
	stateMapping
	stateCrypto
	statePassphrase
	stateRecipients
	stateOutput
	stateReview
	stateSaveConfig
	stateRunning
	stateDone
	statePickArchive
	stateArchiveKey
	stateArchiveInfo
	stateRestoreTarget
	stateRestoring
	stateRestoreDone
	stateGenerated
)

// cfgIntent says why the source and mapping screens are open, which is what
// decides both what a finished mapping is built on top of and where it goes
// next.
type cfgIntent int

const (
	cfgFresh cfgIntent = iota // no configuration at all; the original flow
	cfgEdit                   // reshaping the one the review screen showed
	cfgNew                    // replacing it with one built from scratch
)

// mapEntry is one chosen source and the directory it lands in inside the
// archive. Member previews the final name, which is what makes the mapping
// concrete while the user is still deciding.
//
// as carries an alias read from a configuration file. The mapping screen does
// not offer a way to set one — resolving a basename collision is rare enough to
// belong in the file — but it must survive being edited here, or opening the
// interface would break the archive layout the alias exists to fix.
type mapEntry struct {
	path string
	dest string
	as   string
}

func (m mapEntry) member() string {
	base := m.as
	if base == "" {
		base = filepath.Base(m.path)
	}
	if m.dest == "" {
		return base
	}
	return filepath.ToSlash(filepath.Join(m.dest, base))
}

type model struct {
	state state
	back  []state

	width, height int

	// backup flow
	cfg      *config.Config
	cfgPath  string
	cfgTOML  string
	fromDisk bool

	// intentCfg is why the source and mapping screens are open; base is what a
	// configuration built there inherits its settings from. Together they are
	// what keeps editing from quietly resetting everything the mapping screen
	// never asks about.
	intentCfg cfgIntent
	base      *config.Config

	// destName and destExclude remember, per destination, what a configuration
	// file said about the group that owned it. The mapping screen works in
	// destinations, so this is how a group keeps its name and its exclude
	// patterns across an edit.
	destName    map[string]string
	destExclude map[string][]string

	browser  browser
	mapping  []mapEntry
	mapIndex int
	editing  bool

	menu        list.Model
	cryptoIndex int

	destInput   textinput.Model
	passInput   textinput.Model
	confirmPass textinput.Model
	rcptInput   textinput.Model
	outInput    pathInput
	targetInput pathInput
	cfgInput    pathInput
	passStage   int

	// confirmSave is the path the first enter asked about, when that path
	// already holds a configuration. Only an enter on the same path again
	// replaces it, so editing the field after being warned starts over.
	confirmSave string

	plan *archive.PlanResult

	// backupKey encrypts an archive being written; archiveKey opens one being
	// read. They are separate fields because they are separate secrets, chosen
	// on different screens and meaning different things. One field for both let
	// the passphrase typed to inspect somebody's archive become the passphrase
	// a later backup was encrypted with, silently and without a screen ever
	// asking.
	backupKey  *codec.Keyring
	archiveKey *codec.Keyring

	genPass  string
	outPath  string
	bar      progress.Model
	lastProg archive.Progress
	result   *archive.BackupResult

	// read flow
	intent      readIntent
	archivePath string
	info        *archive.Info
	restored    *archive.RestoreResult

	generated string
	notice    string
	err       error
	msgs      chan tea.Msg
}

type readIntent int

const (
	intentInspect readIntent = iota
	intentRestore
)

// menuItem is one entry on the entry screen. It satisfies list.DefaultItem so
// the list's delegate can draw the blurb under the title.
type menuItem struct{ title, blurb string }

func (i menuItem) Title() string       { return i.title }
func (i menuItem) Description() string { return i.blurb }
func (i menuItem) FilterValue() string { return i.title }

var menuItems = []list.Item{
	menuItem{"Back up", "pack the paths you choose into one encrypted file"},
	menuItem{"Restore", "extract an archive, reproducing its mapped layout"},
	menuItem{"Inspect", "read what an archive holds, without extracting"},
	menuItem{"Generate an identity", "an age key pair, for unattended backups and recovery keys"},
	menuItem{"Generate a passphrase", "a strong diceware passphrase from the EFF wordlist"},
	menuItem{"Quit", "leave arca; nothing is written"},
}

var cryptoItems = []struct{ title, blurb string }{
	{"Generate a passphrase", "recommended — a passphrase you invent is the weakest link"},
	{"Type a passphrase", "checked for strength before anything is written"},
	{"Encrypt to age recipients", "several keys can open the archive; keep one offline for recovery"},
}

// Run starts the interactive interface.
func Run() error {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func newModel() *model {
	mk := func(placeholder string, width int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = 4096
		ti.Width = width
		return ti
	}

	m := &model{
		state:       stateMenu,
		menu:        newMenuList(),
		bar:         progress.New(progress.WithDefaultGradient()),
		msgs:        make(chan tea.Msg, 64),
		destInput:   mk("dotfiles  (empty = archive root)", 40),
		rcptInput:   mk("age1…  (comma or space separated)", 60),
		outInput:    newPathInput(".", 60),
		targetInput: newPathInput("./restored", 60),
		cfgInput:    newPathInput(cli.ConfigFileName, 60),
	}
	m.passInput = mk("passphrase", 48)
	m.passInput.EchoMode = textinput.EchoPassword
	m.confirmPass = mk("confirm", 48)
	m.confirmPass.EchoMode = textinput.EchoPassword

	// A config on disk means the user already described what they want; the
	// flow then starts at the review rather than asking again.
	if path, err := cli.FindConfig(""); err == nil && path != "" {
		if data, readErr := os.ReadFile(path); readErr == nil {
			if cfg, parseErr := config.Parse(data); parseErr == nil {
				m.cfg, m.cfgPath, m.cfgTOML, m.fromDisk = cfg, path, string(data), true
				m.adoptConfig(cfg)
			}
		}
	}
	home, _ := os.UserHomeDir()
	m.browser = newBrowser(home)
	return m
}

func (m *model) Init() tea.Cmd { return textinput.Blink }

func (m *model) push(s state) {
	m.back = append(m.back, m.state)
	m.state = s
	m.err = nil
}

func (m *model) pop() {
	if len(m.back) == 0 {
		m.state = stateMenu
		return
	}
	m.state = m.back[len(m.back)-1]
	m.back = m.back[:len(m.back)-1]
	m.err = nil
}

func (m *model) fail(err error) { m.err = err }

// endRun forgets everything that belonged to the run just finished, so the next
// one starts from the menu rather than from the leftovers of the last.
//
// The keys are the reason this exists. A key that outlives its run is a key no
// screen asked about: the next backup would be encrypted with whatever was last
// typed, and the user could not tell from any screen which secret opens which
// archive. The output path goes with them — reusing it would name an archive
// that already exists, which the writer refuses — and so does the generated
// passphrase, which has already been shown its one time.
//
// The configuration is deliberately kept: it is what the user described, not
// what one run did with it.
func (m *model) endRun() {
	m.backupKey, m.archiveKey = nil, nil
	m.genPass = ""
	m.outPath = ""
	m.plan, m.result, m.restored, m.info = nil, nil, nil, nil
	m.lastProg = archive.Progress{}
}

// returnToReview lands on the review with a back stack that leads straight to
// the menu. Popping instead would walk back through the screens that built the
// configuration, and re-entering them from behind is not what esc means here.
func (m *model) returnToReview() tea.Cmd {
	m.state, m.back, m.err = stateReview, []state{stateMenu}, nil
	return m.refreshPlan()
}

// hasConfig reports whether there is something to review: a configuration read
// from disk, or one built earlier in this session and not yet saved.
func (m *model) hasConfig() bool { return m.cfg != nil && len(m.cfg.Groups) > 0 }

// adoptConfig takes a configuration as the one being worked on: what the
// mapping screen will inherit, and what it must hand back untouched.
//
// Two groups may share a destination — nothing forbids it, since only the names
// have to be unique — and the mapping screen cannot tell them apart, so they
// merge: the first name wins and the exclude patterns are unioned.
func (m *model) adoptConfig(cfg *config.Config) {
	m.base = cfg
	m.destName = map[string]string{}
	m.destExclude = map[string][]string{}
	for _, g := range cfg.Groups {
		if _, seen := m.destName[g.Dest]; !seen {
			m.destName[g.Dest] = g.Name
		}
		m.destExclude[g.Dest] = appendUnique(m.destExclude[g.Dest], g.Exclude)
	}
}

// seedMapping fills the mapping screen from a configuration, keeping each path
// spelled the way the file spells it so that a "~/" survives a round trip.
func (m *model) seedMapping(cfg *config.Config) {
	m.mapping = m.mapping[:0]
	for _, g := range cfg.Groups {
		for _, s := range g.Sources {
			m.mapping = append(m.mapping, mapEntry{path: s.Path, dest: g.Dest, as: s.As})
		}
	}
	m.mapIndex = 0
}

func appendUnique(dst, add []string) []string {
	seen := make(map[string]bool, len(dst))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range add {
		if !seen[v] {
			seen[v] = true
			dst = append(dst, v)
		}
	}
	return dst
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.browser.height = maxInt(6, msg.Height-14)
		m.bar.Width = minInt(60, maxInt(20, msg.Width-20))
		return m, nil

	case tea.KeyMsg:
		// Ctrl+C always works, even mid-run.
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m.key(msg)

	case progressMsg:
		m.lastProg = archive.Progress(msg)
		return m, waitFor(m.msgs)

	case backupDoneMsg:
		if msg.err != nil {
			m.state, m.err = stateReview, msg.err
			return m, nil
		}
		m.result = msg.res
		m.state = stateDone
		return m, nil

	case restoreDoneMsg:
		if msg.err != nil {
			m.state, m.err = stateRestoreTarget, msg.err
			return m, nil
		}
		m.restored = msg.res
		m.state = stateRestoreDone
		return m, nil
	}
	return m, nil
}

func (m *model) View() string {
	if m.width == 0 {
		return "starting…"
	}
	var body string
	switch m.state {
	case stateMenu:
		body = m.viewMenu()
	case stateSources:
		body = m.viewSources()
	case stateMapping:
		body = m.viewMapping()
	case stateCrypto:
		body = m.viewCrypto()
	case statePassphrase:
		body = m.viewPassphrase()
	case stateRecipients:
		body = m.viewRecipients()
	case stateOutput:
		body = m.viewOutput()
	case stateReview:
		body = m.viewReview()
	case stateSaveConfig:
		body = m.viewSaveConfig()
	case stateRunning:
		body = m.viewRunning()
	case stateDone:
		body = m.viewDone()
	case statePickArchive:
		body = m.viewPickArchive()
	case stateArchiveKey:
		body = m.viewArchiveKey()
	case stateArchiveInfo:
		body = m.viewArchiveInfo()
	case stateRestoreTarget:
		body = m.viewRestoreTarget()
	case stateRestoring:
		body = m.viewRestoring()
	case stateRestoreDone:
		body = m.viewRestoreDone()
	case stateGenerated:
		body = m.viewGenerated()
	}
	if m.err != nil {
		body += "\n" + stErr.Render("error: ") + wrap(m.err.Error(), m.width-8) + "\n"
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(body)
}

// ---------- small helpers ----------

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// wrap breaks text on spaces so long errors stay readable in a narrow terminal.
func wrap(s string, width int) string {
	if width < 20 {
		width = 20
	}
	var out, line strings.Builder
	for _, word := range strings.Fields(s) {
		if line.Len() > 0 && line.Len()+1+len(word) > width {
			out.WriteString(line.String() + "\n")
			line.Reset()
		}
		if line.Len() > 0 {
			line.WriteString(" ")
		}
		line.WriteString(word)
	}
	out.WriteString(line.String())
	return out.String()
}

func humanBytes(n int64) string { return cli.HumanBytes(n) }

func defaultOutputDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func splitRecipients(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// buildConfig turns the browser selection into the same Config the CLI builds
// from a file, so both front-ends converge before anything runs.
//
// It inherits rather than defaults. The mapping screen only ever asks about
// sources and destinations, so everything else — the settings, the recipients,
// the group names, the exclude patterns — has to come back out of whatever the
// selection is being built on top of. Starting from Default() instead would
// make editing a configuration a silent way to lose most of it.
func (m *model) buildConfig() (*config.Config, error) {
	base := m.base
	if base == nil {
		base = config.Default()
	}
	cfg := &config.Config{
		Settings:   base.Settings,
		Encryption: config.Encryption{Recipients: append([]string(nil), base.Encryption.Recipients...)},
	}

	byDest := map[string][]config.Source{}
	var order []string
	for _, e := range m.mapping {
		if _, seen := byDest[e.dest]; !seen {
			order = append(order, e.dest)
		}
		byDest[e.dest] = append(byDest[e.dest], config.Source{Path: e.path, As: e.as})
	}
	for i, dest := range order {
		name := m.destName[dest]
		if name == "" {
			name = dest
		}
		if name == "" {
			name = fmt.Sprintf("group-%d", i+1)
		}
		cfg.Groups = append(cfg.Groups, config.Group{
			Name:    name,
			Dest:    dest,
			Sources: byDest[dest],
			Exclude: append([]string(nil), m.destExclude[dest]...),
		})
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// renderTOML writes the configuration back out as a config file, so a session
// spent in the interface is not lost.
//
// Every field Config holds is written, including the ones no screen asks about.
// This text can replace a file the user wrote by hand, and a rendering that
// dropped a key would turn saving into a way to lose an exclude pattern or a
// thread count. Comments are the one thing that cannot survive, which is why
// the screen that writes this says so.
func (m *model) renderTOML(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("# written by arca's interactive interface\n\n[settings]\n")
	fmt.Fprintf(&b, "compression     = %q\n", cfg.Settings.Compression)
	fmt.Fprintf(&b, "compressor      = %q\n", cfg.Settings.Compressor)
	fmt.Fprintf(&b, "cipher          = %q\n", cfg.Settings.Cipher)
	fmt.Fprintf(&b, "threads         = %d\n", cfg.Settings.Threads)
	fmt.Fprintf(&b, "follow_symlinks = %v\n", cfg.Settings.FollowSymlinks)
	fmt.Fprintf(&b, "one_filesystem  = %v\n\n[encryption]\n", cfg.Settings.OneFilesystem)
	if len(cfg.Encryption.Recipients) == 0 {
		b.WriteString("recipients = []\n")
	} else {
		b.WriteString("recipients = [\n")
		for _, r := range cfg.Encryption.Recipients {
			fmt.Fprintf(&b, "  %q,\n", r)
		}
		b.WriteString("]\n")
	}
	for _, g := range cfg.Groups {
		fmt.Fprintf(&b, "\n[[group]]\nname    = %q\ndest    = %q\nsources = [\n", g.Name, g.Dest)
		for _, s := range g.Sources {
			path := contractHome(s.Path)
			if s.As == "" {
				fmt.Fprintf(&b, "  %q,\n", path)
				continue
			}
			fmt.Fprintf(&b, "  { path = %q, as = %q },\n", path, s.As)
		}
		b.WriteString("]\n")
		if len(g.Exclude) > 0 {
			b.WriteString("exclude = [\n")
			for _, e := range g.Exclude {
				fmt.Fprintf(&b, "  %q,\n", e)
			}
			b.WriteString("]\n")
		}
	}
	return b.String()
}

func (m *model) archiveName() string {
	comp, _ := codec.GetCompressor(m.cfg.Settings.Compressor)
	ciph, _ := codec.GetCipher(m.cfg.Settings.Cipher)
	host, _ := os.Hostname()
	return cli.DefaultArchiveName(host, time.Now(), comp, ciph)
}
