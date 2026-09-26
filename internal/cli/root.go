// Package cli wires the command-line front-end.
//
// Flags, the config file and the TUI all converge on the same config.Config, so
// there is one execution path and one thing to test. Precedence is
// defaults < config file < flags.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/henriquemenezes/arca/internal/config"
)

// Version is set at build time via -ldflags.
var Version = "dev"

type globalFlags struct {
	configPath     string
	sources        []string
	level          string
	threads        int
	compressor     string
	cipher         string
	recipients     []string
	followSymlinks bool
	oneFilesystem  bool
}

// bind registers the configuration flags shared by plan and backup.
func (g *globalFlags) bind(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&g.configPath, "config", "c", "", "path to arca.toml (default: ./arca.toml, then ~/.arca/arca.toml)")
	f.StringArrayVar(&g.sources, "source", nil, "ad-hoc source as PATH[:DEST], repeatable; works with no config file")
	f.StringVar(&g.level, "level", "", "compression level: fastest, default, better, best")
	f.IntVar(&g.threads, "threads", 0, "compression threads (0 = one per CPU)")
	f.StringVar(&g.compressor, "compressor", "", "compression format")
	f.StringVar(&g.cipher, "cipher", "", "encryption format")
	f.StringArrayVarP(&g.recipients, "recipient", "r", nil, "age recipient, repeatable; selects key mode instead of a passphrase")
	f.BoolVar(&g.followSymlinks, "follow-symlinks", false, "store what symlinks point at instead of the links themselves")
	f.BoolVar(&g.oneFilesystem, "one-filesystem", true, "do not cross mount points")
}

// load resolves the configuration for this run and returns it alongside the
// verbatim file text, which is stored inside the archive manifest.
func (g *globalFlags) load(cmd *cobra.Command) (*config.Config, string, error) {
	path, err := FindConfig(g.configPath)
	if err != nil {
		return nil, "", err
	}

	cfg := config.Default()
	tomlText := ""
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("reading config: %w", err)
		}
		cfg, err = config.Parse(data)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", path, err)
		}
		tomlText = string(data)
	}

	// Only flags the user actually typed override the file.
	o := config.Overrides{Sources: g.sources}
	changed := cmd.Flags().Changed
	if changed("level") {
		o.Compression = &g.level
	}
	if changed("threads") {
		o.Threads = &g.threads
	}
	if changed("compressor") {
		o.Compressor = &g.compressor
	}
	if changed("cipher") {
		o.Cipher = &g.cipher
	}
	if changed("recipient") {
		o.Recipients = g.recipients
	}
	if changed("follow-symlinks") {
		o.FollowSymlinks = &g.followSymlinks
	}
	if changed("one-filesystem") {
		o.OneFilesystem = &g.oneFilesystem
	}
	if err := cfg.Apply(o); err != nil {
		return nil, "", err
	}

	if len(cfg.Groups) == 0 {
		return nil, "", fmt.Errorf(
			"nothing to back up: no config file found and no --source given.\n" +
				"Run `arca init` to create one, or `arca backup --source ~/.ssh:dotfiles`")
	}
	return cfg, tomlText, nil
}

// bindKeys registers the key material flags.
func bindKeys(cmd *cobra.Command, k *KeyFlags, forWriting bool) {
	f := cmd.Flags()
	f.StringVar(&k.PassphraseFile, "passphrase-file", "",
		"read the passphrase from this file (must be mode 0600); for unattended runs")
	if forWriting {
		f.BoolVar(&k.GeneratePass, "generate-passphrase", false, "generate a strong passphrase and print it once")
		f.BoolVar(&k.AllowWeak, "allow-weak-passphrase", false, "proceed with a passphrase that fails the strength check")
	} else {
		f.StringArrayVarP(&k.Identities, "identity", "i", nil, "age identity file, repeatable")
	}
}

// NewRootCommand builds the command tree. With no arguments on a terminal it
// opens the interactive interface, which is the friendliest default for a tool
// people reach for a few times a year.
func NewRootCommand(runTUI func() error) *cobra.Command {
	fancy := fancyOutput()
	long := "arca packs the paths you choose into one compressed, encrypted file.\n\n" +
		"The archive is plain tar + zstd + age, so it can always be restored without arca:\n" +
		"  age -d ARCHIVE | zstd -d | tar -x"
	if fancy {
		long = CompactBanner(Version) + "\n\n" + long
	}

	root := &cobra.Command{
		Use:           "arca",
		Short:         "Single-file encrypted backups for Unix",
		Long:          long,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if term.IsTerminal(int(os.Stdout.Fd())) && runTUI != nil {
				return runTUI()
			}
			return cmd.Help()
		},
	}
	root.SetVersionTemplate(VersionText(Version, fancy))
	root.AddCommand(
		newInitCommand(),
		newGenKeyCommand(),
		newGenPassphraseCommand(),
		newPlanCommand(),
		newBackupCommand(),
		newListCommand(),
		newVerifyCommand(),
		newRestoreCommand(),
		newTUICommand(runTUI),
	)
	return root
}

// Execute runs the CLI and returns a process exit code.
func Execute(runTUI func() error) int {
	if err := NewRootCommand(runTUI).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, StyleDanger.Render("error: ")+err.Error())
		return 1
	}
	return 0
}

func newTUICommand(runTUI func() error) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive interface",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runTUI == nil {
				return fmt.Errorf("this build has no interactive interface")
			}
			return runTUI()
		},
	}
}
