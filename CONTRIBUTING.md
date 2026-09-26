# Contributing to arca

Thanks for looking. Issues, questions and pull requests are all welcome.

Security problems are the exception: do not open an issue for one, follow
[SECURITY.md](SECURITY.md) instead.

## What arca is, and is not

It helps to know where the line is before writing code:

- arca writes **one self-contained file** per backup. No sidecar files, no
  index, no repository. Anything that needs state beside the archive is out.
- The archive stays **plain `tar` + `zstd` + `age`**, restorable with stock
  tools. A change that makes `age -d … | zstd -d | tar -x` stop working will
  not be merged, whatever it buys.
- **Full backups only.** Incrementals and deduplication are what restic, borg
  and kopia are for.
- The three front-ends (TUI, `arca.toml`, flags) converge on one
  `config.Config` and one execution path. New behaviour goes in the shared
  path, not into a front-end.

If you are unsure whether an idea fits, open an issue before writing it. That
is cheaper for you than a rejected pull request.

## Getting set up

Go 1.26.2 or newer, plus `zstd` and `tar` on your `PATH` for the full suite.

```bash
git clone https://github.com/henriquemenezes/arca
cd arca
make help      # every target
make build
make check     # lint + govulncheck + the whole test suite
```

`make check` is exactly what CI runs. If it passes locally, CI should pass too.
It fetches a stock `age` binary into `.tools/` on first run.

## Pull requests

- **`make check` passes.** Formatting is `golangci-lint fmt` (gofmt +
  goimports); `make fmt` applies it.
- **New behaviour comes with a test.** The safety-critical paths — restore
  containment, exclude matching, key handling, the stock-tool restore — are
  tested, and they stay that way.
- **One concern per pull request.** A refactor and a feature in the same diff
  are hard to review and harder to revert.
- **Commit messages follow [Conventional Commits](https://www.conventionalcommits.org):**
  `feat(tui): …`, `fix: …`, `docs: …`, `refactor: …`, `test: …`, `ci: …`,
  `build: …`, `chore: …`. The subject line says what changes, in the
  imperative, lowercase, without a trailing period.
- **Say why, not just what.** The codebase explains its decisions in comments;
  a pull request that changes a decision should say what the old one missed.
- **Update the docs in the same change.** The README is expected to match the
  binary exactly — a flag that exists and is undocumented is a bug.

## Tests

```bash
make test        # unit tests, race detector
make test-stock  # the above plus the restore path using stock age/zstd/tar
```

Tests must never touch a real home directory: `$ARCA_HOME` and `t.TempDir()`
are what keep them fenced in. Tests that need a stock tool skip when it is
missing locally, but CI fails if they skip there.

## Reporting bugs

Open an issue with the version (`arca --version`), the OS and architecture, the
exact command, and what happened against what you expected.

**Never paste a passphrase, an identity file, or a real archive into an issue.**
Redact paths that you would rather not publish — a reproduction with
`/tmp/example` is usually just as useful.

## Licensing

By contributing, you agree that your contribution is licensed under the
[MIT License](LICENSE), the same terms that cover the rest of the project.
There is no CLA.

## Code of conduct

Participation is covered by the [Code of Conduct](CODE_OF_CONDUCT.md).
