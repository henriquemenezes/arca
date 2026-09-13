// Command arca writes and restores single-file encrypted backups.
//
// The archive is plain tar + zstd + age, so it can always be restored with
// stock Unix tools and never depends on this program:
//
//	age -d ARCHIVE | zstd -d | tar -xp -C DESTINATION
package main

import (
	"os"

	"github.com/hamsa/arca/internal/cli"
	"github.com/hamsa/arca/internal/tui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Execute(tui.Run))
}
