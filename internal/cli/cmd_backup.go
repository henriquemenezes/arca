package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/codec"
	"github.com/henriquemenezes/arca/internal/manifest"
	"github.com/henriquemenezes/arca/internal/secret"
)

func newPlanCommand() *cobra.Command {
	var g globalFlags
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what a backup would capture, without writing anything",
		Long: "plan resolves the configuration, walks the sources reading only metadata, " +
			"and reports the mapping, the sizes and anything it would skip.\n\n" +
			"Nothing is read, written or encrypted. Run it before a real backup.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _, err := g.load(cmd)
			if err != nil {
				return err
			}
			res, err := archive.Plan(cfg, nil)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, StyleTitle.Render("Plan"))
			Field(out, "compression", fmt.Sprintf("%s (%s)", cfg.Settings.Compressor, cfg.Settings.Compression))
			Field(out, "encryption", encryptionDescription(cfg))
			fmt.Fprintln(out)

			fmt.Fprintln(out, StyleTitle.Render("Mapping"))
			for _, s := range res.Sources {
				fmt.Fprintf(out, "  %s\n      %s %s   %s\n",
					s.Path,
					StyleMuted.Render("→"),
					StyleKey.Render(s.Member),
					StyleMuted.Render(fmt.Sprintf("%s, %s", Count(s.Stats.Files, "file", "files"), HumanBytes(s.Stats.Bytes))))
			}
			fmt.Fprintln(out)

			printStats(out, "Totals", res.Stats)
			PrintWarnings(out, res.Warnings)
			fmt.Fprintf(out, "\n%s\n", StyleMuted.Render(
				"This is an estimate: compression and encryption decide the final size."))
			return nil
		},
	}
	g.bind(cmd)
	return cmd
}

func newBackupCommand() *cobra.Command {
	var (
		g      globalFlags
		keys   KeyFlags
		output string
		noPlan bool
	)
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write a single compressed, encrypted archive",
		Long: "backup packs every configured source into one file.\n\n" +
			"The result is plain tar + zstd + age and can always be restored without arca:\n" +
			"  age -d ARCHIVE | zstd -d | tar -x",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, tomlText, err := g.load(cmd)
			if err != nil {
				return err
			}
			compressor, err := codec.GetCompressor(cfg.Settings.Compressor)
			if err != nil {
				return err
			}
			cipher, err := codec.GetCipher(cfg.Settings.Cipher)
			if err != nil {
				return err
			}
			outPath, err := ResolveOutputPath(output, time.Now(), compressor, cipher)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			var planned *manifest.Stats
			if !noPlan {
				// A counting pass costs only metadata reads, and buys both a
				// real percentage below and the planned totals in the manifest.
				p, err := archive.Plan(cfg, map[string]bool{outPath: true, outPath + archive.PartialSuffix: true})
				if err != nil {
					return err
				}
				planned = &p.Stats
				fmt.Fprintf(out, "%s %s, %s\n",
					StyleMuted.Render("To archive:"), Count(p.Stats.Files, "file", "files"), HumanBytes(p.Stats.Bytes))
				PrintWarnings(out, p.Warnings)
			}

			WarnCircularKeyDependency(out, cfg, cfg.Encryption.Recipients)

			ring, err := keys.EncryptKeyring(cfg, out)
			if err != nil {
				return err
			}
			defer secret.Zero(ring.Passphrase)

			var total int64
			if planned != nil {
				total = planned.Bytes
			}
			bar := newProgressPrinter(os.Stderr, total)

			res, err := archive.Backup(archive.BackupOptions{
				Config:      cfg,
				ConfigTOML:  tomlText,
				Keyring:     ring,
				OutputPath:  outPath,
				ToolVersion: Version,
				Planned:     planned,
				Progress:    bar.update,
			})
			bar.done()
			if err != nil {
				return err
			}

			fmt.Fprintf(out, "\n%s %s\n", StyleOK.Render("Archive written:"), res.Path)
			printStats(out, "Contents", res.Stats)
			Field(out, "archive size", HumanBytes(res.ArchiveBytes))
			if res.Stats.Bytes > 0 {
				Field(out, "ratio", fmt.Sprintf("%.1f%% of the source bytes",
					float64(res.ArchiveBytes)/float64(res.Stats.Bytes)*100))
			}
			PrintWarnings(out, res.Warnings)

			fmt.Fprintf(out, "\n%s\n  %s\n",
				StyleTitle.Render("Restore without arca, with age, zstd and tar:"),
				StyleKey.Render(ManualRestoreCommand(res.Path, cfg)))
			fmt.Fprintln(out)
			PrintSecretNotice(out)
			fmt.Fprintln(out, StyleMuted.Render(
				"  One copy is not a backup: keep 3 copies, on 2 kinds of media, 1 off-site."))
			return nil
		},
	}
	g.bind(cmd)
	bindKeys(cmd, &keys, true)
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"archive path, or a directory to place a generated name in (default: working directory)")
	cmd.Flags().BoolVar(&noPlan, "no-plan", false, "skip the counting pass (no percentage in the progress display)")
	return cmd
}
