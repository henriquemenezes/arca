package cli

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/henriquemenezes/arca/internal/archive"
	"github.com/henriquemenezes/arca/internal/manifest"
	"github.com/henriquemenezes/arca/internal/secret"
)

func newListCommand() *cobra.Command {
	var (
		keys  KeyFlags
		short bool
	)
	cmd := &cobra.Command{
		Use:   "list ARCHIVE",
		Short: "Show what an archive contains",
		Long: "list reads the manifest stored inside the archive and, unless --short is given, " +
			"enumerates every member.\n\n" +
			"The manifest is encrypted along with the content, so this needs the key.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ring, err := keys.OpenArchiveKeyring(args[0], out)
			if err != nil {
				return err
			}
			defer secret.Zero(ring.Passphrase)

			if short {
				info, err := archive.Inspect(args[0], ring)
				if err != nil {
					return err
				}
				printManifest(out, info)
				return nil
			}

			res, err := archive.List(args[0], ring)
			if err != nil {
				return err
			}
			printManifest(out, res.Info)
			fmt.Fprintln(out)
			printStats(out, "Contents", res.Stats)
			if res.Summary != nil && len(res.Summary.Warnings) > 0 {
				fmt.Fprintln(out)
				fmt.Fprintln(out, StyleTitle.Render("Recorded at backup time"))
				PrintWarnings(out, res.Summary.Warnings)
			}

			fmt.Fprintln(out)
			fmt.Fprintln(out, StyleTitle.Render("Members"))
			for _, e := range res.Entries {
				suffix := ""
				if e.LinkTarget != "" {
					suffix = StyleMuted.Render(" → " + e.LinkTarget)
				}
				fmt.Fprintf(out, "  %s %9s  %s%s\n",
					e.Mode.String(), HumanBytes(e.Size), e.Name, suffix)
			}
			return nil
		},
	}
	bindKeys(cmd, &keys, false)
	cmd.Flags().BoolVar(&short, "short", false, "manifest only; does not read the whole archive")
	return cmd
}

func newVerifyCommand() *cobra.Command {
	var keys KeyFlags
	cmd := &cobra.Command{
		Use:   "verify ARCHIVE",
		Short: "Check that an archive is intact and restorable",
		Long: "verify decrypts and decompresses the entire archive, discarding the content.\n\n" +
			"This is stronger than a checksum stored next to the file: the cipher authenticates " +
			"every chunk and marks the last one, so tampering anywhere and truncation at the end " +
			"both fail here. The completion record written at the end of a successful backup is " +
			"checked too.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ring, err := keys.OpenArchiveKeyring(args[0], out)
			if err != nil {
				return err
			}
			defer secret.Zero(ring.Passphrase)

			res, err := archive.Verify(args[0], ring)
			if err != nil {
				return fmt.Errorf("archive did NOT verify: %w", err)
			}
			printManifest(out, res.Info)
			fmt.Fprintln(out)
			printStats(out, "Contents", res.Stats)
			PrintWarnings(out, res.Warnings)
			fmt.Fprintf(out, "\n%s this archive decrypts, authenticates and is complete.\n",
				StyleOK.Render("Verified:"))
			return nil
		},
	}
	bindKeys(cmd, &keys, false)
	return cmd
}

func newRestoreCommand() *cobra.Command {
	var (
		keys          KeyFlags
		target        string
		groups        []string
		dryRun        bool
		overwrite     bool
		preserveOwner bool
	)
	cmd := &cobra.Command{
		Use:   "restore ARCHIVE",
		Short: "Extract an archive, reproducing the mapped layout",
		Long: "restore extracts into --target.\n\n" +
			"The mapping was applied when the archive was written, so extraction reproduces it " +
			"directly: .ssh and .aws come back under dotfiles/, and so on. Nothing outside the " +
			"target is ever written.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ring, err := keys.OpenArchiveKeyring(args[0], out)
			if err != nil {
				return err
			}
			defer secret.Zero(ring.Passphrase)

			info, err := archive.Inspect(args[0], ring)
			if err != nil {
				return err
			}
			printManifest(out, info)
			fmt.Fprintln(out)

			bar := newProgressPrinter(os.Stderr, 0)
			if info.Manifest.Planned != nil {
				bar = newProgressPrinter(os.Stderr, info.Manifest.Planned.Bytes)
			}
			res, err := archive.Restore(archive.RestoreOptions{
				Path:          args[0],
				Keyring:       ring,
				Target:        target,
				Groups:        groups,
				DryRun:        dryRun,
				Overwrite:     overwrite,
				PreserveOwner: preserveOwner,
				Progress:      bar.update,
			})
			bar.done()
			if err != nil {
				return err
			}

			verb := "Restored to"
			if dryRun {
				verb = "Would restore to"
			}
			fmt.Fprintf(out, "%s %s\n", StyleOK.Render(verb), res.Target)
			printStats(out, "Restored", manifest.Stats{
				Files: res.Files, Dirs: res.Dirs, Symlinks: res.Symlinks,
				Hardlinks: res.Hardlinks, Bytes: res.Bytes,
			})
			PrintWarnings(out, res.Warnings)
			if dryRun {
				// The newline stays outside Render: lipgloss pads every line of
				// a multi-line block to the same width, which shows up as a
				// trail of spaces.
				fmt.Fprintln(out)
				fmt.Fprintln(out, StyleMuted.Render("  Nothing was written. Drop --dry-run to do it for real."))
			}
			return nil
		},
	}
	bindKeys(cmd, &keys, false)
	f := cmd.Flags()
	f.StringVar(&target, "target", "", "directory to extract into (required)")
	f.StringArrayVar(&groups, "group", nil, "restore only these groups, repeatable")
	f.BoolVar(&dryRun, "dry-run", false, "report what would happen without writing anything")
	f.BoolVar(&overwrite, "overwrite", false, "replace files that already exist in the target")
	f.BoolVar(&preserveOwner, "preserve-owner", false, "restore uid/gid (only has an effect as root)")
	// Cannot fail: "target" is declared above.
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

// printManifest renders what the archive says about itself.
func printManifest(w io.Writer, info *archive.Info) {
	m := info.Manifest
	fmt.Fprintln(w, StyleTitle.Render("Archive"))
	Field(w, "created", m.CreatedAt.Local().Format("2006-01-02 15:04:05 MST"))
	Field(w, "host", fmt.Sprintf("%s (%s/%s, user %s)", m.Host.Hostname, m.Host.OS, m.Host.Arch, m.Host.User))
	Field(w, "written by", fmt.Sprintf("%s %s", m.Tool, m.ToolVersion))
	Field(w, "pipeline", fmt.Sprintf("tar → %s (%s) → %s", info.Compressor, m.Pipeline.Compression, info.Cipher))
	mode := m.Pipeline.EncryptionMode
	if m.Pipeline.Recipients > 0 {
		mode = fmt.Sprintf("%s (%d)", mode, m.Pipeline.Recipients)
	}
	Field(w, "encryption", mode)
	if m.Planned != nil {
		Field(w, "planned", fmt.Sprintf("%s, %s", Count(m.Planned.Files, "file", "files"), HumanBytes(m.Planned.Bytes)))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, StyleTitle.Render("Mapping"))
	groups := make([]manifest.Group, len(m.Groups))
	copy(groups, m.Groups)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	for _, g := range groups {
		fmt.Fprintf(w, "  %s\n", StyleKey.Render(g.Name))
		for _, s := range g.Sources {
			fmt.Fprintf(w, "      %s %s %s\n", s.Path, StyleMuted.Render("→"), s.Member)
		}
	}
}
