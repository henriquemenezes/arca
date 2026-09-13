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
		})},
		{"mapping", mk(stateMapping, func(m *model) {
			m.mapping = []mapEntry{
				{path: filepath.Join(home, ".ssh"), dest: "dotfiles"},
				{path: filepath.Join(home, "Downloads"), dest: "Downloads"},
			}
		})},
		{"encryption", mk(stateCrypto, nil)},
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
			m.outPath = "/home/u/backups/arca-omarchy-2026-09-13T14-30-05Z.tar.zst.age"
			m.refreshPlan()
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
