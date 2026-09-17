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
// directory first and then in arca's own directory.
const ConfigFileName = "arca.toml"

// UserDirName is the directory arca owns inside the user's home, and HomeEnv
// is the only way to move it.
const (
	UserDirName = ".arca"
	HomeEnv     = "ARCA_HOME"
)

// UserDir is where arca writes the config and the key: ~/.arca.
//
// Deliberately not the XDG location. This is the same path on every Unix, next
// to the ~/.ssh and ~/.gnupg it exists to protect, and a restore onto a freshly
// installed machine is one directory to put back.
//
// $ARCA_HOME overrides it, which is also what lets the test suite run without
// ever going near a real home directory.
func UserDir() string {
	if dir := os.Getenv(HomeEnv); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, UserDirName)
}

// legacyUserDir is where arca used to keep both files: <os.UserConfigDir>/arca.
//
// It is still read so that an existing install keeps working. An identity.age
// stranded there is not an inconvenience — it is a backup nobody can open
// again. Nothing is ever written to it, and $ARCA_HOME switches it off so that
// an override is a complete one.
func legacyUserDir() string {
	if os.Getenv(HomeEnv) != "" {
		return ""
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "arca")
}

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

// configSearchPath lists the candidates in precedence order: the working
// directory beats the user's own, which beats the directory arca used before.
func configSearchPath() []string {
	paths := []string{ConfigFileName}
	for _, dir := range []string{UserDir(), legacyUserDir()} {
		if dir != "" {
			paths = append(paths, filepath.Join(dir, ConfigFileName))
		}
	}
	return paths
}

// ResolveConfigPath turns an optional user-given path into the file to write.
//
// Empty means the default, <UserDir>/arca.toml. A directory — existing, or
// written with a trailing separator — gets the conventional name inside it, and
// anything else is taken literally. That is the same rule ResolveOutputPath
// applies to the archive, so learning one teaches the other.
func ResolveConfigPath(given string) (string, error) {
	if given == "" {
		dir := UserDir()
		if dir == "" {
			return "", fmt.Errorf("cannot locate your home directory; give an explicit path")
		}
		return filepath.Join(dir, ConfigFileName), nil
	}
	if strings.HasSuffix(given, string(filepath.Separator)) {
		return filepath.Join(given, ConfigFileName), nil
	}
	if info, err := os.Stat(given); err == nil && info.IsDir() {
		return filepath.Join(given, ConfigFileName), nil
	}
	return given, nil
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
