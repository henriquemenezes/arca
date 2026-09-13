package codec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
)

func init() { RegisterCipher(ageCipher{}) }

// ageCipher wraps filippo.io/age, the reference implementation. Primitives are
// age's own choices, deliberately: X25519 + HKDF-SHA-256 for key agreement,
// ChaCha20-Poly1305 in the STREAM construction for the payload, scrypt for
// passphrases. Nothing here invents cryptography.
type ageCipher struct{}

func (ageCipher) Name() string  { return "age" }
func (ageCipher) Ext() string   { return "age" }
func (ageCipher) Magic() []byte { return []byte("age-encryption.org/v1\n") }

// ErrMixedKeyModes reports the age spec rule that a passphrase recipient must
// be the only recipient: mixing it with public keys would silently reduce the
// whole file to the strength of the passphrase. This is why the tool picks an
// encryption mode per run instead of combining them.
var ErrMixedKeyModes = errors.New(
	"age does not allow a passphrase to be combined with public-key recipients: " +
		"a passphrase recipient must be the only one, otherwise the file would be " +
		"reduced to the strength of the passphrase. Choose one mode per backup")

// Validate checks the keyring cheaply: no scrypt derivation, no output file.
func (c ageCipher) Validate(k *Keyring) error {
	if k.Empty() {
		return errors.New("no key material: provide a passphrase or at least one recipient")
	}
	if len(k.Passphrase) > 0 && len(k.Recipients) > 0 {
		return ErrMixedKeyModes
	}
	if len(k.Passphrase) == 0 && len(k.Recipients) == 0 {
		return errors.New("no recipients and no passphrase: nothing could decrypt this archive")
	}
	for _, s := range k.Recipients {
		if _, err := parseRecipient(s); err != nil {
			return err
		}
	}
	return nil
}

func (c ageCipher) Seal(w io.Writer, k *Keyring) (io.WriteCloser, error) {
	if err := c.Validate(k); err != nil {
		return nil, err
	}
	var recipients []age.Recipient
	if len(k.Passphrase) > 0 {
		r, err := age.NewScryptRecipient(string(k.Passphrase))
		if err != nil {
			return nil, fmt.Errorf("passphrase recipient: %w", err)
		}
		// The work factor stays at age's default on purpose. Raising it risks
		// tripping the reference implementation's maximum-work-factor guard,
		// which would break emergency restore with the stock `age` binary.
		recipients = append(recipients, r)
	}
	for _, s := range k.Recipients {
		r, err := parseRecipient(s)
		if err != nil {
			return nil, err
		}
		recipients = append(recipients, r)
	}
	if len(recipients) == 0 {
		return nil, errors.New("no recipients resolved from the keyring")
	}
	return age.Encrypt(w, recipients...)
}

func (c ageCipher) Open(r io.Reader, k *Keyring) (io.ReadCloser, error) {
	if k.Empty() {
		return nil, errors.New("no key material: provide a passphrase or an identity")
	}

	var identities []age.Identity
	if len(k.Passphrase) > 0 {
		id, err := age.NewScryptIdentity(string(k.Passphrase))
		if err != nil {
			return nil, fmt.Errorf("passphrase identity: %w", err)
		}
		identities = append(identities, id)
	}
	for _, s := range k.Identities {
		ids, err := age.ParseIdentities(strings.NewReader(s))
		if err != nil {
			return nil, fmt.Errorf("inline identity: %w", err)
		}
		identities = append(identities, ids...)
	}
	for _, path := range k.IdentityFiles {
		ids, err := identitiesFromFile(path)
		if err != nil {
			return nil, err
		}
		identities = append(identities, ids...)
	}
	if len(identities) == 0 {
		return nil, errors.New("no identities resolved from the keyring")
	}

	dec, err := age.Decrypt(r, identities...)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(dec), nil
}

func parseRecipient(s string) (age.Recipient, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "age1") {
		r, err := age.ParseX25519Recipient(s)
		if err != nil {
			return nil, fmt.Errorf("invalid age recipient %q: %w", s, err)
		}
		return r, nil
	}
	if strings.HasPrefix(s, "ssh-") || strings.HasPrefix(s, "sk-ssh-") || strings.HasPrefix(s, "ecdsa-") {
		r, err := agessh.ParseRecipient(s)
		if err != nil {
			return nil, fmt.Errorf("invalid ssh recipient: %w", err)
		}
		return r, nil
	}
	return nil, fmt.Errorf("unrecognised recipient %q: expected an age1... key or an ssh public key", s)
}

// identitiesFromFile loads a private key file, refusing one that other users on
// the machine can read. A private key readable by anyone is a finding, not a
// warning, so this fails the operation rather than printing a notice.
func identitiesFromFile(path string) ([]age.Identity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("identity file: %w", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf(
			"identity file %s has mode %#o: it is readable by other users on this machine. "+
				"Fix it with: chmod 600 %s", path, mode, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("identity file: %w", err)
	}
	if ids, err := age.ParseIdentities(strings.NewReader(string(data))); err == nil {
		return ids, nil
	}
	// Fall back to an OpenSSH private key, so an existing ~/.ssh key works.
	id, sshErr := agessh.ParseIdentity(data)
	if sshErr != nil {
		return nil, fmt.Errorf("identity file %s: not a valid age or ssh private key: %w", path, sshErr)
	}
	return []age.Identity{id}, nil
}
