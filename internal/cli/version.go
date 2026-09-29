package cli

import "runtime/debug"

// devVersion is what a build carries when nothing named it.
const devVersion = "dev"

// ResolveVersion decides what `arca --version` reports, and therefore what
// every archive records as its ToolVersion.
//
// The release path sets main.version with -ldflags "-X" and wins.
// `go install ...@v0.1.0` has no way to pass -X, so there the tag survives only
// in the module metadata Go stamps into the binary — reporting that beats
// writing "dev" into an archive nobody can trace back to a release.
func ResolveVersion(ldflags string) string {
	return pickVersion(ldflags, moduleVersion())
}

// pickVersion holds the precedence alone, so the decision is testable without
// compiling a binary per case.
func pickVersion(ldflags, module string) string {
	if ldflags != "" && ldflags != devVersion {
		return ldflags
	}
	if module != "" && module != "(devel)" {
		return module
	}
	return devVersion
}

// moduleVersion is the version Go records for the main module: the tag for
// `go install ...@tag`, a pseudo-version for a plain build since Go 1.24, and
// "(devel)" or nothing before that.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return info.Main.Version
}
