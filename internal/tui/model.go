package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// mapEntry is one chosen source and the directory it lands in inside the
// archive. Member previews the final name, which is what makes the mapping
// concrete while the user is still deciding.
type mapEntry struct {
	path string
	dest string
}

func (m mapEntry) member() string {
	base := filepath.Base(m.path)
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

	browser  browser
	mapping  []mapEntry
	mapIndex int
	editing  bool

	menuIndex   int
	cryptoIndex int

	destInput   textinput.Model
	passInput   textinput.Model
	confirmPass textinput.Model
	rcptInput   textinput.Model
	outInput    textinput.Model
	targetInput textinput.Model
	passStage   int

	plan     *archive.PlanResult
	keyring  *codec.Keyring
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

var menuItems = []struct{ title, blurb string }{
	{"Back up", "pack the paths you choose into one encrypted file"},
	{"Restore", "extract an archive, reproducing its mapped layout"},
	{"Inspect", "read what an archive holds, without extracting"},
	{"Generate an identity", "an age key pair, for unattended backups and recovery keys"},
	{"Generate a passphrase", "a strong diceware passphrase from the EFF wordlist"},
	{"Quit", ""},
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
		bar:         progress.New(progress.WithDefaultGradient()),
		msgs:        make(chan tea.Msg, 64),
		destInput:   mk("dotfiles  (empty = archive root)", 40),
		rcptInput:   mk("age1…  (comma or space separated)", 60),
		outInput:    mk(".", 60),
		targetInput: mk("./restored", 60),
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
func (m *model) buildConfig() (*config.Config, error) {
	cfg := config.Default()
	byDest := map[string][]config.Source{}
	var order []string
	for _, e := range m.mapping {
		if _, seen := byDest[e.dest]; !seen {
			order = append(order, e.dest)
		}
		byDest[e.dest] = append(byDest[e.dest], config.Source{Path: e.path})
	}
	for i, dest := range order {
		name := dest
		if name == "" {
			name = fmt.Sprintf("group-%d", i+1)
		}
		cfg.Groups = append(cfg.Groups, config.Group{Name: name, Dest: dest, Sources: byDest[dest]})
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// renderTOML writes the selection back out as a config file, so a session spent
// in the interface is not lost.
func (m *model) renderTOML(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("# written by arca's interactive interface\n\n[settings]\n")
	fmt.Fprintf(&b, "compression     = %q\n", cfg.Settings.Compression)
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
			fmt.Fprintf(&b, "  %q,\n", s.Path)
		}
		b.WriteString("]\n")
	}
	return b.String()
}

func (m *model) archiveName() string {
	comp, _ := codec.GetCompressor(m.cfg.Settings.Compressor)
	ciph, _ := codec.GetCipher(m.cfg.Settings.Cipher)
	host, _ := os.Hostname()
	return cli.DefaultArchiveName(host, time.Now(), comp, ciph)
}
