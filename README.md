# arca

Single-file encrypted backups for Unix. One binary, no runtime dependencies.

```
arca backup --source ~/.ssh:dotfiles --source ~/.aws:dotfiles -o ~/backups/
```

produces one file — `arca-<host>-<timestamp>.tar.zst.age` — and nothing else.
No sidecar, no index, no repository.

## Why not restic, borg or kopia

Those are repository engines built for frequent incremental backups of data that
barely changes. They deduplicate, they store absolute paths, and they restore to
the origin. If that is your situation, use one of them — they are excellent.

arca is for the other case:

- **Occasional full backups.** Before reinstalling the OS, when migrating
  machines, once a month. Deduplication never gets exercised, so you would pay
  its complexity — repository, pruning, retention policy, `check` — for nothing.
- **Any destination.** A restic repository inside a Dropbox or OneDrive folder is
  thousands of small blobs, and reconciling those is one of the most common ways
  to corrupt one. A single file is one upload.
- **Path remapping.** `~/.ssh` and `~/.aws` should come back under `dotfiles/`,
  not at their original absolute paths. No repository engine does this.
- **Metadata secrecy.** File names, directory structure and the mapping itself
  are inside the encryption. The artifact is one opaque file.
- **Restoring on a machine with nothing installed.** This is the important one.

## The guarantee

The archive is plain `tar` + `zstd` + `age`. If arca has vanished, your backup
has not:

```
age -d archive.tar.zst.age | zstd -d | tar -xp -C /destination
```

Three tools in every distribution's repositories. This is covered by a test that
runs the real `age`, `zstd` and `tar` binaries against a real archive — if it
ever stops passing, the project has lost its reason to exist.

## The mapping

`dest` is a directory inside the archive; each source contributes its basename
under it.

```toml
[[group]]
name    = "dotfiles"
dest    = "dotfiles"
sources = ["~/.ssh", "~/.aws"]
```

gives you `dotfiles/.ssh` and `dotfiles/.aws`. The mapping is applied while
writing, so restoring is a plain extraction — even with stock `tar`.

Two sources whose basename collides (`~/.config/nvim` and `~/.local/share/nvim`)
are a hard error at plan time, never a silent overwrite. Resolve it with an alias:

```toml
sources = [
  { path = "~/.config/nvim",      as = "nvim-config" },
  { path = "~/.local/share/nvim", as = "nvim-data"   },
]
```

## Three ways to drive it

All three build the same configuration and take the same code path.
Precedence: **defaults < `arca.toml` < flags**.

```bash
arca                    # interactive, when run on a terminal with no arguments
arca init               # write a commented arca.toml
arca plan               # what would be captured; writes nothing
arca backup -o ~/backups/
arca verify  ARCHIVE
arca list    ARCHIVE
arca restore ARCHIVE --target ~/restored
```

Ad-hoc, with no config file at all:

```bash
arca backup --source ~/.ssh:dotfiles --source ~/Downloads -o /media/usb/
```

## Encryption

Primitives are age's, not invented here: X25519 + HKDF-SHA-256 for key
agreement, ChaCha20-Poly1305 in the STREAM construction for the payload, scrypt
for passphrases.

Two modes, and **age forbids combining them** — a passphrase recipient must be
the only recipient, or the archive would fall back to the strength of that
passphrase:

```bash
arca gen-passphrase                       # diceware, ~77 bits, from the EFF list
arca backup --generate-passphrase         # generate and use one, shown once

arca gen-key                              # an age identity
arca backup -r age1daily... -r age1recovery...
```

Use several recipients. One everyday key, one recovery key kept offline, is the
protection against locking yourself out.

A passphrase you invent is refused if it fails a strength check
(`--allow-weak-passphrase` overrides, loudly). Passphrases are never read from
the environment: that would expose them in `/proc/<pid>/environ` and to anyone
who can list processes. Use a terminal or `--passphrase-file` (mode 0600).

## What an attacker holding the file learns

| Observable | Leaks? |
|---|---|
| File and directory names | **No** |
| Directory structure | **No** |
| File contents | **No** |
| The manifest and the mapping | **No** |
| uid/gid, mtimes, permissions | **No** |
| Approximate total size | Yes — inherent without padding |
| Whether it uses a passphrase or keys | Yes — the age header says so |
| Number of recipients | Yes; the recipients themselves do not leak |
| Date and hostname | Only through the file name; `--output` overrides it |

There is no checksum beside the file, and none is needed: the STREAM
construction authenticates every 64 KiB chunk and marks the final one, so
tampering anywhere and truncation at the end both fail. `arca verify` decrypts
the whole archive, which is a stronger check than any hash stored next to it.

Compressing before encrypting leaks plaintext information through size only when
an attacker has an adaptive oracle — injecting chosen plaintext and watching the
result, as in CRIME/BREACH. A one-shot backup gives no such oracle, which is why
restic, borg and kopia all do the same.

## Restoring safely

`arca restore` writes nothing outside `--target`. It refuses member paths that
are absolute or climb out, refuses to write through a symlink (the classic tar
attack: plant `link -> /etc`, then write `link/passwd`), refuses hardlinks
pointing outside, and skips device nodes. Existing files are never replaced
without `--overwrite`.

Permissions are restored with an explicit `chmod`, immune to your umask — an ssh
key restored as 0644 is silently ignored by ssh, and that is the difference
between a restore that works and one that appears to.

## Extending it

Compression and encryption are registry entries identified by magic bytes, not
by file extension, which is why a renamed archive still restores and why you
only need the key, not the algorithm:

```
peek → "age-encryption.org/v1\n" → age → decrypt
     → peek → 28 B5 2F FD        → zstd → decompress → tar
```

Adding gpg, openssl, xz or lz4 means implementing `codec.Compressor` or
`codec.Cipher` and registering it. The pipeline does not change.

## Known limitations

- **Full backup every time.** By design; see the top of this file.
- **macOS xattrs and resource forks are not preserved.** Keeping them would
  require a non-standard format, which would break restoring with stock `tar` —
  a deliberate trade.
- **Maximum compression is roughly `zstd -11`, not `-19`.** The pure-Go zstd
  implementation is what keeps the binary cgo-free and cross-compilable to
  macOS.
- **Zeroing passphrase memory is best effort.** Go's garbage collector may have
  copied the bytes already, and nothing here keeps them out of swap.

## Development

```
make build     # static binary
make test      # unit tests
make check     # everything, including restoring with the real age/zstd/tar
make cross     # prove it cross-compiles to linux and darwin, amd64 and arm64
```
