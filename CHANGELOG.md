# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is 0, the command-line interface and the interactive
screens may change between minor versions; the **archive format will not break
without a major version**, and any archive arca has written stays restorable
with stock `age`, `zstd` and `tar` regardless.

## [Unreleased]

## [0.1.0] - 2026-09-26

First public release.

### Added

- `arca backup`: pack any number of sources into a single
  `tar` + `zstd` + `age` file, with the group mapping applied while writing so
  that restoring is a plain extraction.
- `arca plan`: resolve the configuration and walk the sources reading only
  metadata, reporting the mapping, the sizes and everything that would be
  skipped. Writes nothing.
- `arca restore`: extraction confined to `--target`, refusing absolute and
  escaping member paths, writes through symlinks, and hardlinks pointing
  outside; device nodes are skipped and permissions are restored with an
  explicit `chmod`. `--dry-run`, `--overwrite`, `--group` and
  `--preserve-owner`.
- `arca verify` and `arca list`: authenticate the whole archive, or read the
  manifest and the members out of it.
- `arca init`: write a commented `arca.toml`, in `~/.arca` or in a directory of
  your choosing.
- `arca gen-key` and `arca gen-passphrase`: an age X25519 identity, or a
  diceware passphrase from the EFF long wordlist (~77 bits by default).
- Interactive interface, opened by running `arca` with no arguments on a
  terminal: pick sources, map them, write exclude patterns or apply ready-made
  preset sets, review and run.
- Encryption with age in either passphrase mode or multi-recipient key mode;
  a strength check on passphrases the user invents, overridable only with
  `--allow-weak-passphrase`.
- Exclude patterns per group, following gitignore's convention: a pattern
  without a separator matches a basename at any depth, one with a separator is
  matched against the path.
- Configuration from `./arca.toml`, then `~/.arca/arca.toml`, with
  `$ARCA_HOME` overriding the directory and flags overriding the file.
- Compression and encryption dispatched by magic bytes rather than file
  extension, so a renamed archive still restores.

[Unreleased]: https://github.com/henriquemenezes/arca/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/henriquemenezes/arca/releases/tag/v0.1.0
