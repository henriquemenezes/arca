# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
While the major version is 0, the command-line interface and the interactive
screens may change between minor versions; the **archive format will not break
without a major version**, and any archive arca has written stays restorable
with `age`, `zstd` and `tar` regardless.

## [Unreleased]

### Added

- Interactive interface: `/` opens the finder on the screen that chooses an
  archive to restore or inspect, as it already did on the source screen. A path
  is completed and a bare name is searched for below the directory on screen,
  so an archive several directories down is named rather than walked to.
- Packages on every release: `.deb` for Debian and Ubuntu, `.rpm` for Fedora,
  `.apk` for Alpine. The binary is static and shells out to nothing, so the
  packages declare no dependencies at all and one of them serves every version
  of every derivative.
- Homebrew on macOS: `brew install --cask henriquemenezes/tap/arca`.
- Builds for 32-bit arm (`armv7`), which is what an older Raspberry Pi or a NAS
  runs — machines a backup is quite likely to be written from.
- Every release is signed and attested. `SHA256SUMS` carries a keyless Sigstore
  signature, each artefact carries a build provenance attestation, and an SBOM
  is published alongside. A checksum answers whether the file is the one the
  release page lists; only the attestation answers whether this project's CI
  built it, which for a tool that handles your keys is the question that
  matters. The README spells out both commands.
- Release notes are this file's section for the tag. What appears on the release
  page is what was written and reviewed here, not a list of commit subjects.

### Changed

- The description says **Unix-like** rather than Unix, and no longer calls
  `age` and `zstd` stock tools: neither ships in a base install, and the
  restore one-liner needs GNU tar or bsdtar for its `-C`. The guarantee is
  unchanged — only the claims about it are now accurate.
- `make test-stock` is now `make test-upstream`.
- Releases are built by GoReleaser instead of a loop in the Makefile. `make
  build` and `make cross` are unchanged; `make dist` is gone, and `make
  snapshot` takes its place by building every release artefact locally without
  needing a tag.
- Builds are reproducible: the timestamps in the output come from the commit
  rather than from the clock, so the same source rebuilds to the same bytes and
  a third party can check that the published binary matches this tree.

### Fixed

- The emergency-restore tests run on macOS in CI as well, so the `tar -xp -C`
  in the README is exercised against bsdtar and not only against GNU tar.
- A binary installed with `go install` reported its version as `dev`, and wrote
  that into the `tool_version` of the manifest of every archive it packed — a
  backup that could not be traced back to a release. `go install` has no way to
  pass linker flags, so the version now falls back to the module version that Go
  stamps into the binary, which for `go install ...@v0.1.0` is the tag.

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
