---
description: Draft CHANGELOG.md [Unreleased] entries from the commits since the last release
allowed-tools: Bash(git log:*), Bash(git tag:*), Bash(git describe:*), Bash(git diff:*), Read, Edit
---

Draft the `## [Unreleased]` entries in `CHANGELOG.md` for the work that has
landed since the last released section, then stop for review.

Do not commit. Do not tag. The release notes are extracted from this file by
`scripts/release-notes.sh`, so what you write is published verbatim — it is
reviewed by a person first, and that review is the point.

## Gather

Read the commits with their **bodies**, not just their subjects:

```
git log $(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD)..HEAD --format='%h %s%n%b%n---'
```

The subject of a commit in this repo says what changed; the body says why, and
why is what the changelog is for. A changelog written from subjects alone throws
away the more useful half.

Then read the released sections already in `CHANGELOG.md`. They are the only
record of the project's voice — imitate them rather than writing generic release
notes.

## Write

- Group under `### Added`, `### Changed`, `### Fixed`, appending to whatever is
  already in `[Unreleased]`. Keep a Changelog order, and no other headings.
- Write what changes **for someone using arca**, and why. Not "refactored the
  walker" but what they will now see, and what it means.
- One entry per user-visible change, not one per commit. Several commits that
  add one capability are one entry.
- Leave out `chore(deps)` bumps and `docs:` commits unless a user-visible claim
  changed with them.
- Wrap at 80 columns, in the prose style of the existing entries.

## Do not invent

If a commit's user-visible effect is not clear from its message and diff, do not
guess at one. Collect those commits in a short list at the end of your reply,
outside the file, and say what you could not determine. A changelog entry that
is confidently wrong is worse than a question.
