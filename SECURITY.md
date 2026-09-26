# Security policy

arca encrypts backups. A bug in the wrong place can expose data that was meant
to stay private, or destroy data that was meant to come back. Reports are
welcome and taken seriously.

## Supported versions

arca is pre-1.0. Only the latest release is supported; fixes land there and in
`main`, and are not backported.

| Version | Supported |
|---|---|
| latest release | yes |
| anything older | no |

## Reporting a vulnerability

**Please do not open a public issue.**

Use GitHub's private reporting instead:
[**Report a vulnerability**](https://github.com/henriquemenezes/arca/security/advisories/new)
(Security → Advisories → Report a vulnerability). That channel is private
between you and the maintainers until a fix is published.

Useful things to include, as far as you have them:

- what an attacker gets, and what they need to start with;
- the affected version (`arca --version`) and platform;
- minimal steps to reproduce — a script is ideal;
- whether it needs a crafted archive, a crafted config, or neither.

**Never attach a real archive, identity file, passphrase or passphrase file to
a report.** If a proof of concept needs an archive, generate a throwaway one
with a throwaway key.

### What to expect

This is a small project maintained in spare time. An acknowledgement should
take a few days. There is no bounty programme. Credit in the advisory and the
release notes is offered unless you would rather stay anonymous.

Please give a reasonable window for a fix before disclosing publicly —
90 days is the usual expectation, and less if the issue is already being
exploited.

## In scope

- Anything that lets a crafted archive write outside `--target`, overwrite
  files without `--overwrite`, or execute code during a restore.
- Anything that leaks plaintext, file names, the manifest, keys or passphrases
  into the archive, into logs, into process arguments, or onto disk.
- Weakening of the encryption as written: wrong recipients, reused nonces,
  passphrase mode silently combined with key mode, a missing authenticity
  check.
- An archive that `arca verify` accepts but that does not restore correctly.

## Out of scope

- Vulnerabilities in `age`, `zstd`, `tar` or any dependency — report those
  upstream. If arca *uses* one of them unsafely, that is in scope here.
- Losing data because the passphrase or identity was lost. That is the design:
  there is no recovery path, by construction.
- The fact that the archive's approximate size, its encryption mode and its
  number of recipients are observable. This is documented in the README.
- Anything that requires an attacker who already has root, or who already has
  your identity file or passphrase.
- Missing hardening that has no exploit attached to it.
