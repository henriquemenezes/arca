package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/hamsa/arca/internal/archive"
	"github.com/hamsa/arca/internal/codec"
	"github.com/hamsa/arca/internal/config"
	"github.com/hamsa/arca/internal/secret"
)

// DefaultIdentityName is where `arca gen-key` puts a generated identity, and
// the first place a restore looks when no --identity is given.
const DefaultIdentityName = "identity.age"

// KeyFlags collects the key material options shared by several commands.
//
// There is deliberately no environment-variable option: a passphrase in the
// environment is visible in /proc/<pid>/environ and to anyone who can list
// processes.
type KeyFlags struct {
	PassphraseFile string
	Identities     []string
	Recipients     []string
	AllowWeak      bool
	GeneratePass   bool
}

// DefaultIdentityPath is ~/.config/arca/identity.age.
func DefaultIdentityPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "arca", DefaultIdentityName)
}

// EncryptKeyring assembles the key material for writing an archive.
//
// Recipient mode and passphrase mode are mutually exclusive by the age spec, so
// the choice is made here, once, and the user is told which one is in effect.
func (k *KeyFlags) EncryptKeyring(cfg *config.Config, out io.Writer) (*codec.Keyring, error) {
	if len(cfg.Encryption.Recipients) > 0 {
		fmt.Fprintf(out, "%s %s\n",
			StyleKey.Render("Encrypting to:"), Count(len(cfg.Encryption.Recipients), "recipient", "recipients"))
		return &codec.Keyring{Recipients: cfg.Encryption.Recipients}, nil
	}

	pass, err := k.passphraseForWriting(out)
	if err != nil {
		return nil, err
	}
	if !k.AllowWeak {
		if err := secret.CheckStrength(string(pass)); err != nil {
			secret.Zero(pass)
			return nil, err
		}
	} else if err := secret.CheckStrength(string(pass)); err != nil {
		fmt.Fprintln(out, StyleDanger.Render("! Using a weak passphrase because --allow-weak-passphrase was given."))
		fmt.Fprintln(out, StyleMuted.Render("  "+err.Error()))
	}
	return &codec.Keyring{Passphrase: pass}, nil
}

func (k *KeyFlags) passphraseForWriting(out io.Writer) ([]byte, error) {
	switch {
	case k.PassphraseFile != "":
		return secret.ReadFile(k.PassphraseFile)

	case k.GeneratePass:
		p, err := secret.Generate(secret.DefaultWords)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(out, "\n%s\n\n    %s\n\n",
			StyleTitle.Render("Generated passphrase"), StyleKey.Render(p))
		fmt.Fprintf(out, "%s\n\n", StyleMuted.Render(fmt.Sprintf(
			"  %d words from the EFF list, about %.0f bits of entropy. Save it now — it is shown once.",
			secret.DefaultWords, secret.EntropyBits(secret.DefaultWords))))
		// The notice is printed once, at the end of the run.
		return []byte(p), nil

	default:
		return promptPassphrase(out, true)
	}
}

// DecryptKeyring assembles key material for reading an archive.
//
// When nothing is specified it prefers a default identity file if one exists,
// and otherwise asks for a passphrase. OpenWithFallback covers the case where
// that guess is wrong.
func (k *KeyFlags) DecryptKeyring(out io.Writer) (*codec.Keyring, bool, error) {
	if len(k.Identities) > 0 {
		return &codec.Keyring{IdentityFiles: k.Identities}, false, nil
	}
	if k.PassphraseFile != "" {
		pass, err := secret.ReadFile(k.PassphraseFile)
		if err != nil {
			return nil, false, err
		}
		return &codec.Keyring{Passphrase: pass}, false, nil
	}

	if p := DefaultIdentityPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			fmt.Fprintf(out, "%s %s\n", StyleMuted.Render("Trying identity:"), p)
			// A guess, so the caller may fall back to a passphrase.
			return &codec.Keyring{IdentityFiles: []string{p}}, true, nil
		}
	}
	pass, err := promptPassphrase(out, false)
	if err != nil {
		return nil, false, err
	}
	return &codec.Keyring{Passphrase: pass}, false, nil
}

// OpenArchiveKeyring resolves key material for reading, retrying with a
// passphrase prompt when the default identity turns out to be the wrong guess.
func (k *KeyFlags) OpenArchiveKeyring(path string, out io.Writer) (*codec.Keyring, error) {
	ring, guessed, err := k.DecryptKeyring(out)
	if err != nil {
		return nil, err
	}
	if _, err := archive.Inspect(path, ring); err == nil {
		return ring, nil
	} else if !guessed {
		return nil, err
	}

	fmt.Fprintln(out, StyleMuted.Render("That identity does not open this archive; it may use a passphrase."))
	pass, err := promptPassphrase(out, false)
	if err != nil {
		return nil, err
	}
	return &codec.Keyring{Passphrase: pass}, nil
}

func promptPassphrase(out io.Writer, confirm bool) ([]byte, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New(
			"a passphrase is required but there is no terminal to ask on: " +
				"use --passphrase-file, or --identity for a key-encrypted archive")
	}
	p := &secret.Prompter{
		Out: out,
		ReadSecret: func(prompt string) ([]byte, error) {
			fmt.Fprint(out, prompt)
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(out)
			if err != nil {
				return nil, fmt.Errorf("reading passphrase: %w", err)
			}
			return b, nil
		},
	}
	return p.Passphrase(confirm)
}

// WarnCircularKeyDependency catches the trap of encrypting a backup with an ssh
// key that lives inside that same backup: if the machine dies, both are lost
// together.
func WarnCircularKeyDependency(out io.Writer, cfg *config.Config, recipients []string) {
	var ssh []string
	for _, r := range recipients {
		if strings.HasPrefix(strings.TrimSpace(r), "ssh-") {
			ssh = append(ssh, r)
		}
	}
	if len(ssh) == 0 {
		return
	}
	sources, err := cfg.Resolve()
	if err != nil {
		return
	}
	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")
	for _, s := range sources {
		if s.Path == sshDir || strings.HasPrefix(sshDir, s.Path+string(filepath.Separator)) {
			fmt.Fprintln(out, StyleDanger.Render(
				"! This backup is encrypted to an ssh key, and it also contains "+s.Path+"."))
			fmt.Fprintln(out, StyleMuted.Render(
				"  If this machine is lost you lose the key and the backup together. "+
					"Add a recovery recipient that is stored elsewhere."))
			return
		}
	}
}
