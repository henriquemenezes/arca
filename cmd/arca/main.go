// Command arca writes and restores single-file encrypted backups.
//
// The archive is plain tar + zstd + age, so it can always be restored with age,
// zstd and tar, and never depends on this program:
//
//	age -d ARCHIVE | zstd -d | tar -xp -C DESTINATION
package main

import (
	"os"

	"github.com/henriquemenezes/arca/internal/cli"
	"github.com/henriquemenezes/arca/internal/tui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
// A build that cannot be given flags - `go install ...@v0.1.0` - leaves it
// alone, and cli.ResolveVersion recovers the tag from the module metadata.
var version = "dev"

func main() {
	cli.Version = cli.ResolveVersion(version)
	os.Exit(cli.Execute(tui.Run))
}
