package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/hamsa/arca/internal/archive"
)

type progressMsg archive.Progress

type backupDoneMsg struct {
	res *archive.BackupResult
	err error
}

type restoreDoneMsg struct {
	res *archive.RestoreResult
	err error
}

// waitFor turns the progress channel into a stream of Bubble Tea messages.
func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// startBackup runs the backup off the UI goroutine.
//
// Progress updates are dropped when the channel is full, deliberately: the
// archive must never be slowed down by how fast the terminal can redraw.
func startBackup(opts archive.BackupOptions, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		opts.Progress = func(p archive.Progress) {
			select {
			case ch <- progressMsg(p):
			default:
			}
		}
		res, err := archive.Backup(opts)
		return backupDoneMsg{res: res, err: err}
	}
}

func startRestore(opts archive.RestoreOptions, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		opts.Progress = func(p archive.Progress) {
			select {
			case ch <- progressMsg(p):
			default:
			}
		}
		res, err := archive.Restore(opts)
		return restoreDoneMsg{res: res, err: err}
	}
}
