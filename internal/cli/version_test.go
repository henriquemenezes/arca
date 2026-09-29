package cli

import "testing"

// The version reported is not cosmetic: it is copied into every archive's
// manifest as ToolVersion. A build that cannot name itself writes an archive
// nobody can trace back to a release.
func TestPickVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ldflags string
		module  string
		want    string
	}{
		{
			// The release path: `make dist` and GoReleaser both pass -X.
			name:    "ldflags wins over the module version",
			ldflags: "v0.1.0",
			module:  "v0.0.0-20260929003357-1b057d4807fa",
			want:    "v0.1.0",
		},
		{
			// `go install ...@v0.1.0` cannot pass -X, so the tag survives
			// only in the module metadata Go stamps into the binary.
			name:    "the module version is used when ldflags is absent",
			ldflags: "dev",
			module:  "v0.1.0",
			want:    "v0.1.0",
		},
		{
			// -X main.version= (empty) is the same as not passing it.
			name:    "an empty ldflags value is not a version",
			ldflags: "",
			module:  "v0.1.0",
			want:    "v0.1.0",
		},
		{
			// Go before 1.24, and any build with -buildvcs=false.
			name:    "(devel) is not a version",
			ldflags: "dev",
			module:  "(devel)",
			want:    "dev",
		},
		{
			name:    "dev survives when nothing is known",
			ldflags: "dev",
			module:  "",
			want:    "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickVersion(tc.ldflags, tc.module); got != tc.want {
				t.Fatalf("pickVersion(%q, %q) = %q, want %q",
					tc.ldflags, tc.module, got, tc.want)
			}
		})
	}
}
