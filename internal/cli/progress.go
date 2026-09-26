package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/henriquemenezes/arca/internal/archive"
)

// progressPrinter renders a single updating line on a terminal, and stays quiet
// when output is redirected so logs do not fill with carriage returns.
type progressPrinter struct {
	out      io.Writer
	tty      bool
	total    int64
	started  time.Time
	lastDraw time.Time
	width    int
}

func newProgressPrinter(out io.Writer, total int64) *progressPrinter {
	p := &progressPrinter{out: out, total: total, started: time.Now()}
	if f, ok := out.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		p.tty = true
		if w, _, err := term.GetSize(int(f.Fd())); err == nil {
			p.width = w
		}
	}
	return p
}

func (p *progressPrinter) update(pr archive.Progress) {
	if !p.tty {
		return
	}
	// Roughly ten frames a second: enough to look live, cheap enough that the
	// display never becomes the bottleneck on a tree with a million files.
	if time.Since(p.lastDraw) < 100*time.Millisecond {
		return
	}
	p.lastDraw = time.Now()

	elapsed := time.Since(p.started).Seconds()
	rate := ""
	if elapsed > 0.5 {
		rate = fmt.Sprintf(" · %s/s", HumanBytes(int64(float64(pr.Bytes)/elapsed)))
	}
	head := fmt.Sprintf("  %d files · %s", pr.Files, HumanBytes(pr.Bytes))
	if p.total > 0 {
		pct := float64(pr.Bytes) / float64(p.total) * 100
		if pct > 100 {
			pct = 100
		}
		head = fmt.Sprintf("  %5.1f%% · %d files · %s of %s", pct, pr.Files, HumanBytes(pr.Bytes), HumanBytes(p.total))
	}
	line := head + rate + "  " + StyleMuted.Render(truncateLeft(pr.Member, p.remaining(head+rate)))
	fmt.Fprintf(p.out, "\r\033[2K%s", line)
}

func (p *progressPrinter) remaining(used string) int {
	if p.width <= 0 {
		return 40
	}
	n := p.width - len(used) - 4
	if n < 10 {
		return 10
	}
	return n
}

func (p *progressPrinter) done() {
	if p.tty {
		fmt.Fprint(p.out, "\r\033[2K")
	}
}

// truncateLeft keeps the tail of a path, which is the informative end.
func truncateLeft(s string, max int) string {
	if max <= 1 || len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max+1:]
}
