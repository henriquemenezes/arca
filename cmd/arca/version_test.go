package main_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `go install github.com/henriquemenezes/arca/cmd/arca@v0.1.0` has no way to
// pass -ldflags, and the version it therefore leaves unset is copied into every
// archive's manifest as ToolVersion. A binary built without -X has to name
// itself from the module metadata Go stamps in, not report "dev".
//
// This compiles the command, so it is one of the slower tests in the suite. It
// is also the only one that covers the install path users actually take.
func TestVersionOfABuildWithoutLdflags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "arca")

	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building without -ldflags failed: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("arca --version failed: %v\n%s", err, out)
	}

	got := strings.TrimSpace(string(out))
	if got == "arca version dev" {
		t.Fatalf("a build without -ldflags reports no version: %q\n"+
			"the module version Go stamps in should have been used instead", got)
	}
	if !strings.HasPrefix(got, "arca version v") {
		t.Fatalf("arca --version = %q, want a version starting with \"v\"", got)
	}
}
