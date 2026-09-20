package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hamsa/arca/internal/archive"
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/manifest"
)

// TestRenderPreview is a development aid: `go test -run RenderPreview -v`
// prints every screen so layout regressions are visible.
func TestRenderPreview(t *testing.T) {
	if os.Getenv("ARCA_PREVIEW") == "" {
		t.Skip("set ARCA_PREVIEW=1 to print the screens")
	}
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o755)
	os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("KEY"), 0o600)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host x"), 0o644)
	os.MkdirAll(filepath.Join(home, "Downloads"), 0o755)
	os.WriteFile(filepath.Join(home, "Downloads", "big.bin"), make([]byte, 900000), 0o644)
	os.MkdirAll(filepath.Join(home, "Work", "site", "node_modules", "left-pad"), 0o755)
	os.WriteFile(filepath.Join(home, "Work", "site", "index.js"), []byte("main"), 0o644)
	os.WriteFile(filepath.Join(home, "Work", "site", "node_modules", "left-pad", "i.js"),
		make([]byte, 40000), 0o644)

	mk := func(s state, tweak func(*model)) *model {
		m := newModel()
		m.Update(tea.WindowSizeMsg{Width: 92, Height: 34})
		m.cfg = config.Default()
		m.state = s
		if tweak != nil {
			tweak(m)
		}
		return m
	}

	frames := []struct {
		name string
		m    *model
	}{
		{"menu", mk(stateMenu, nil)},
		{"sources", mk(stateSources, func(m *model) {
			m.browser = newBrowser(home)
			m.browser.selected[filepath.Join(home, ".ssh")] = true
			m.browser.selected[filepath.Join(home, "Downloads")] = true
			run(m, m.measureSelection())
		})},
		{"sources (nothing chosen)", mk(stateSources, func(m *model) {
			m.browser = newBrowser(home)
		})},
		{"sources (too narrow for the preview)", mk(stateSources, func(m *model) {
			m.Update(tea.WindowSizeMsg{Width: 64, Height: 24})
			m.browser = newBrowser(home)
			m.browser.selected[filepath.Join(home, ".ssh")] = true
			run(m, m.measureSelection())
		})},
		{"sources (excludes)", mk(stateSources, func(m *model) {
			m.browser = newBrowser(home)
			site := filepath.Join(home, "Work", "site")
			m.browser.selected[site] = true
			m.browser.selected[filepath.Join(home, ".ssh")] = true
			m.addPattern(site, "node_modules")
			m.addPattern(site, "**/*.log")
			m.mode = srcExcludes
			m.excIndex = 1
			run(m, m.measureSelection())
		})},
		{"sources (excludes, too narrow for two columns)", mk(stateSources, func(m *model) {
			m.Update(tea.WindowSizeMsg{Width: 64, Height: 24})
			m.browser = newBrowser(home)
			site := filepath.Join(home, "Work", "site")
			m.browser.selected[site] = true
			m.addPattern(site, "node_modules")
			m.mode = srcExcludes
			run(m, m.measureSelection())
		})},
		{"mapping", mk(stateMapping, func(m *model) {
			m.mapping = []mapEntry{
				{path: filepath.Join(home, ".ssh"), dest: "dotfiles"},
				{path: filepath.Join(home, "Downloads"), dest: "Downloads"},
			}
		})},
		{"encryption", mk(stateCrypto, nil)},
		{"destination (typed prefix)", mk(stateOutput, func(m *model) {
			m.outInput.SetValue(filepath.Join(home, "D"))
		})},
		{"destination (field cleared)", mk(stateOutput, func(m *model) {
			m.outInput.SetValue("")
		})},
		{"restore target", mk(stateRestoreTarget, func(m *model) {
			m.targetInput.SetValue(home + string(filepath.Separator))
		})},
		{"passphrase", mk(statePassphrase, func(m *model) { m.passInput.SetValue("senha123") })},
		{"review", mk(stateReview, func(m *model) {
			m.mapping = []mapEntry{
				{path: filepath.Join(home, ".ssh"), dest: "dotfiles"},
				{path: filepath.Join(home, "Downloads"), dest: "Downloads"},
			}
			cfg, err := m.buildConfig()
			if err != nil {
				t.Fatal(err)
			}
			m.cfg = cfg
			m.cfgPath, m.fromDisk = "/home/u/.arca/arca.toml", true
			m.cfg.Groups[0].Exclude = []string{"**/known_hosts.old"}
			m.outPath = "/home/u/backups/arca-omarchy-2026-09-13T14-30-05Z.tar.zst.age"
			m.refreshPlan()
		})},
		{"save configuration", mk(stateSaveConfig, func(m *model) {
			m.mapping = []mapEntry{
				{path: filepath.Join(home, ".ssh"), dest: "dotfiles"},
				{path: filepath.Join(home, "Downloads"), dest: "Downloads"},
			}
			cfg, err := m.buildConfig()
			if err != nil {
				t.Fatal(err)
			}
			cfg.Groups[0].Exclude = []string{"**/known_hosts.old"}
			m.cfg = cfg
			existing := filepath.Join(home, "arca.toml")
			os.WriteFile(existing, []byte("# hand written\n"), 0o644)
			m.cfgInput.SetValue(existing)
		})},
		{"running", mk(stateRunning, func(m *model) {
			m.plan = &archive.PlanResult{Stats: manifest.Stats{Files: 412, Bytes: 900000}}
			m.lastProg = archive.Progress{Files: 180, Bytes: 380000, Member: "Downloads/Downloads/big.bin"}
		})},
		{"done", mk(stateDone, func(m *model) {
			m.genPass = "populate-strobe-froth-shadow-decoy-tacking"
			m.result = &archive.BackupResult{
				Path:         "/home/u/backups/arca-omarchy-2026-09-13T14-30-05Z.tar.zst.age",
				ArchiveBytes: 812345,
				Stats:        manifest.Stats{Files: 412, Dirs: 33, Bytes: 900000},
			}
		})},
	}

	for _, f := range frames {
		t.Logf("\n\n╔══ %s ══════════════════════════════════════════════════════════\n%s", f.name, f.m.View())
	}
}
