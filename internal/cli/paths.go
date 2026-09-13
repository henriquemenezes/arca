package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hamsa/arca/internal/codec"
)

// ConfigFileName is the conventional config name, looked up in the working
// directory first and then in the user's config directory.
const ConfigFileName = "arca.toml"

// FindConfig locates the configuration to use. An explicit path must exist; the
// implicit search returns "" when there is none, which is not an error because
// a backup can run entirely from --source flags.
func FindConfig(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("config file: %w", err)
		}
		return explicit, nil
	}
	for _, candidate := range configSearchPath() {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

func configSearchPath() []string {
	paths := []string{ConfigFileName}
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "arca", ConfigFileName))
	}
	return paths
}

// DefaultArchiveName builds the conventional file name. Host and timestamp are
// conveniences only: both are also recorded inside the encrypted manifest, and
// --output can override the name entirely.
func DefaultArchiveName(host string, at time.Time, comp codec.Compressor, ciph codec.Cipher) string {
	if host == "" {
		host = "backup"
	}
	stamp := at.UTC().Format("2006-01-02T15-04-05Z")
	return fmt.Sprintf("arca-%s-%s.tar.%s.%s", sanitiseHost(host), stamp, comp.Ext(), ciph.Ext())
}

// sanitiseHost keeps the file name portable across filesystems.
func sanitiseHost(h string) string {
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "backup"
	}
	return out
}

// ResolveOutputPath turns --output into a concrete archive path.
//
// A directory (existing, or written with a trailing separator) gets a generated
// name inside it; anything else is taken literally, so a user who names the
// file gets exactly that name.
func ResolveOutputPath(out string, at time.Time, comp codec.Compressor, ciph codec.Cipher) (string, error) {
	host, _ := os.Hostname()
	generated := DefaultArchiveName(host, at, comp, ciph)

	if out == "" {
		return filepath.Abs(generated)
	}
	if strings.HasSuffix(out, string(filepath.Separator)) {
		return filepath.Abs(filepath.Join(out, generated))
	}
	if info, err := os.Stat(out); err == nil && info.IsDir() {
		return filepath.Abs(filepath.Join(out, generated))
	}
	return filepath.Abs(out)
}
