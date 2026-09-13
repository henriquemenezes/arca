package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"filippo.io/age"
)

// WriteIdentity generates an age identity and stores it, returning the public
// recipient. Shared by the CLI and the interactive interface so key handling
// exists in exactly one place.
//
// The file is created with 0600 from the start: a private key must never exist
// on disk with looser permissions, not even for an instant.
func WriteIdentity(path string) (recipient string, err error) {
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf(
			"%s already exists: refusing to replace an identity, which would make every archive "+
				"encrypted to it unreadable", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("creating key directory: %w", err)
	}

	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", fmt.Errorf("generating identity: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("creating identity file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "# created by arca\n# public key: %s\n%s\n", id.Recipient(), id); err != nil {
		f.Close()
		return "", fmt.Errorf("writing identity file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("writing identity file: %w", err)
	}
	return id.Recipient().String(), nil
}
