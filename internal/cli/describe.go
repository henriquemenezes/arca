package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/henriquemenezes/arca/internal/codec"
	"github.com/henriquemenezes/arca/internal/config"
	"github.com/henriquemenezes/arca/internal/manifest"
)

func printStats(w io.Writer, title string, s manifest.Stats) {
	fmt.Fprintln(w, StyleTitle.Render(title))
	Field(w, "files", fmt.Sprintf("%d", s.Files))
	Field(w, "directories", fmt.Sprintf("%d", s.Dirs))
	if s.Symlinks > 0 {
		Field(w, "symlinks", fmt.Sprintf("%d", s.Symlinks))
	}
	if s.Hardlinks > 0 {
		Field(w, "hardlinks", fmt.Sprintf("%d", s.Hardlinks))
	}
	Field(w, "size", HumanBytes(s.Bytes))
}

func encryptionDescription(cfg *config.Config) string {
	if n := len(cfg.Encryption.Recipients); n > 0 {
		return fmt.Sprintf("%s, %s", cfg.Settings.Cipher, Count(n, "recipient", "recipients"))
	}
	return cfg.Settings.Cipher + ", passphrase"
}

// ManualRestoreCommand renders the command that restores an archive with age,
// zstd and tar. It is printed after every backup, because the guarantee is only
// useful if the user knows it exists.
func ManualRestoreCommand(path string, cfg *config.Config) string {
	comp, err := codec.GetCompressor(cfg.Settings.Compressor)
	if err != nil {
		return ""
	}
	decrypt := "age -d"
	if len(cfg.Encryption.Recipients) > 0 {
		decrypt = "age -d -i IDENTITY"
	}
	return fmt.Sprintf("%s %s | %s | tar -xp -C DESTINATION", decrypt, shellQuote(path), decompressCommand(comp))
}

func decompressCommand(c codec.Compressor) string {
	switch c.Name() {
	case "zstd":
		return "zstd -d"
	default:
		return c.Name() + " -d"
	}
}

// shellQuote keeps a printed command copy-pasteable when a path has spaces.
func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t'\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
