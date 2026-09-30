# arca

Single-file encrypted backups: `tar` + `zstd` + `age`, one opaque artefact,
restorable on a machine that has nothing installed.

## The invariant

An archive must stay restorable with real `age`, `zstd` and `tar`:

```
age -d ARCHIVE | zstd -d | tar -xp -C DESTINATION
```

This is the project's reason to exist, not a nice property. `internal/archive`
covers it with tests that run the actual binaries against a real archive, and CI
fails if those tests are *skipped* rather than run. Nothing may change the
format so that this one-liner stops working, and nothing may make arca itself
necessary for a restore. A feature that requires arca to read arca's output is
the wrong feature.

The format is versioned (`format_version` in the manifest); it does not break
without a major version.

## Working here

- `make help` lists every target. `make check` is what CI runs.
- `make test` is the unit tests; `make test-upstream` also runs the
  emergency-restore path against a real `age` binary, which `make tools`
  fetches. Prefer it before claiming a change is safe.
- Tests use real binaries and real archives. Mocks are a last resort, and a
  mocked restore proves nothing.
- Comments explain **why**, not what. Match the density of the file you are in;
  most non-obvious decisions here carry a sentence saying what would go wrong
  otherwise.
- Linux and macOS, amd64/arm64/armv7. No Windows build: the archive carries
  uid/gid and permission bits, and restoring them is most of the point.

## Commits

Conventional commits: `type(scope): imperative subject`, lowercase, under 72
characters. The body is prose paragraphs wrapped at about 76 columns explaining
why the change is right and what it would have cost to do otherwise — read
`git log` before writing one, the bar is set there.

## Changelog and releases

`CHANGELOG.md` is hand-written, Keep a Changelog format. Every user-visible
change gets an entry under `[Unreleased]`; `/changelog` drafts them from the
commits for review.

Releasing is two: `make release-prep VERSION=vX.Y.Z` moves `[Unreleased]` into a
dated section and fixes the link references — mechanical, and it refuses to move
an empty section — then you commit that and push an annotated tag `vX.Y.Z`. `.github/workflows/release.yml`
then builds every platform with GoReleaser, signs `SHA256SUMS` with a keyless
Sigstore identity, attests provenance, publishes packages and the Homebrew cask,
and takes the release notes from this version's `CHANGELOG.md` section — so a tag
whose version has no section there fails the release on purpose.

`make snapshot` builds every release artefact locally without a tag. It skips
signing always: keyless cosign attests that the *workflow* built the artefact,
so signing from a laptop would record the wrong identity.

Platform lists live in `.goreleaser.yaml`. Tool versions are pinned in the
Makefile and the workflows reference the same numbers; keep them in sync.
