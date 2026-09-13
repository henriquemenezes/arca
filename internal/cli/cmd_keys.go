package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hamsa/arca/internal/secret"
)

//nolint:lll // the sample is meant to be read as a file, not as source
const sampleConfig = `# arca.toml — what to back up, and where it lands inside the archive.
#
# "dest" is the destination directory inside the archive; each source
# contributes its basename under it. So the two sources below are stored as
# dotfiles/.ssh and dotfiles/.aws, and that is exactly how they come back.

[settings]
compression     = "best"     # fastest | default | better | best
threads         = 0          # 0 = one per CPU
follow_symlinks = false      # store links as links, not as copies
one_filesystem  = true       # do not cross mount points

[encryption]
# Empty means passphrase mode.
# Listing recipients switches to key mode, where several keys can open the
# archive. Use that: one everyday key, plus a recovery key kept offline.
recipients = []
# recipients = ["age1...", "age1recovery..."]

[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = [
  "~/.ssh",
  "~/.aws",
  "~/.gnupg",
  # Two paths whose basename is the same need an explicit alias:
  # { path = "~/.config/nvim",      as = "nvim-config" },
  # { path = "~/.local/share/nvim", as = "nvim-data"   },
]
exclude = ["**/known_hosts.old"]

[[group]]
name    = "projects"
dest    = "Work"
sources = ["~/Work"]
exclude = ["**/node_modules", "**/target", "**/.venv", "**/__pycache__"]

[[group]]
name    = "downloads"
dest    = "Downloads"
sources = ["~/Downloads"]
exclude = ["*.iso", "*.dmg"]
`

func newInitCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a commented arca.toml to start from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if _, err := os.Stat(ConfigFileName); err == nil && !force {
				return fmt.Errorf("%s already exists (use --force to replace it)", ConfigFileName)
			}
			if err := os.WriteFile(ConfigFileName, []byte(sampleConfig), 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", ConfigFileName, err)
			}

			fmt.Fprintf(out, "%s %s\n\n", StyleOK.Render("Created"), ConfigFileName)
			fmt.Fprintln(out, "Next:")
			fmt.Fprintf(out, "  1. Edit %s so it lists what you actually want.\n", ConfigFileName)
			fmt.Fprintf(out, "  2. %s   %s\n", StyleKey.Render("arca plan"), StyleMuted.Render("— check the mapping and sizes; writes nothing"))
			fmt.Fprintf(out, "  3. %s %s\n\n", StyleKey.Render("arca backup"), StyleMuted.Render("— write the archive"))
			fmt.Fprintln(out, StyleMuted.Render(
				"For unattended backups, run `arca gen-key` and list the recipient in [encryption]."))
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing arca.toml")
	return cmd
}

func newGenKeyCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "gen-key",
		Short: "Generate an age identity for key-based backups",
		Long: "gen-key creates an X25519 identity.\n\n" +
			"Key mode lets several recipients open the same archive, which is how you avoid " +
			"locking yourself out: keep one key for everyday use and one recovery key offline. " +
			"It also makes unattended backups possible, since writing needs only the public part.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			path := output
			if path == "" {
				path = DefaultIdentityPath()
				if path == "" {
					return fmt.Errorf("cannot determine a config directory; pass --output")
				}
			}
			recipient, err := WriteIdentity(path)
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "%s %s %s\n\n", StyleOK.Render("Identity written:"), path, StyleMuted.Render("(mode 0600)"))
			fmt.Fprintf(out, "%s\n\n    %s\n\n", StyleTitle.Render("Public recipient — safe to share, put it in arca.toml"), StyleKey.Render(recipient))
			fmt.Fprintf(out, "  %s\n", StyleMuted.Render("[encryption]"))
			fmt.Fprintf(out, "  %s\n\n", StyleMuted.Render(fmt.Sprintf(`recipients = ["%s"]`, recipient)))
			PrintSecretNotice(out)
			fmt.Fprintf(out, "\n%s\n", StyleDanger.Render(
				"Back this identity file up somewhere other than the archives it protects."))
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "where to write the identity (default: the user config dir)")
	return cmd
}

func newGenPassphraseCommand() *cobra.Command {
	var words int
	cmd := &cobra.Command{
		Use:   "gen-passphrase",
		Short: "Generate a strong diceware passphrase",
		Long: "gen-passphrase draws words from the EFF large wordlist using the system CSPRNG.\n\n" +
			"A passphrase you invent is the weakest part of an otherwise strong design. " +
			"Generating one removes that problem.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			p, err := secret.Generate(words)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "\n    %s\n\n", StyleKey.Render(p))
			fmt.Fprintf(out, "  %s\n\n", StyleMuted.Render(fmt.Sprintf(
				"%d words from a list of %d, about %.0f bits of entropy.",
				words, len(secret.Words()), secret.EntropyBits(words))))
			PrintSecretNotice(out)
			return nil
		},
	}
	cmd.Flags().IntVarP(&words, "words", "w", secret.DefaultWords,
		fmt.Sprintf("how many words (minimum %d)", secret.MinWords))
	return cmd
}
