#!/usr/bin/env bash
# Extract one version's section from CHANGELOG.md, to be published as the
# release notes of that tag.
#
# The notes are written by hand and only extracted here. Notes generated from
# commit subjects answer "what was changed"; someone deciding whether to upgrade
# a tool that holds their encryption keys is asking "what changes for me, and
# why", which is what the changelog already answers. Extracting rather than
# regenerating also means the published notes cannot drift from the committed
# changelog.
#
# A tag whose version has no section is an error. An empty release note is worse
# than a failed release, because only one of the two gets noticed.
set -euo pipefail

version="${1:?usage: release-notes.sh VERSION [OUT]}"
version="${version#v}"
out="${2:-RELEASE_NOTES.md}"

# The heading is matched literally, not as a regexp: the dots in a version are
# not wildcards, and "## [Unreleased]" must never satisfy a version match.
awk -v want="## [$version]" '
	substr($0, 1, length(want)) == want { found = 1; next }
	found && /^## \[/                   { exit }
	# The link-reference block at the foot of the file belongs to the document,
	# not to any one version. Markdown renders it as nothing, so leaving it in
	# would silently pad every release body with a list that only grows.
	found && /^\[[^]]+\]: /             { exit }
	found                               { print }
' CHANGELOG.md | sed -e '/./,$!d' >"$out"

if [ ! -s "$out" ]; then
	echo "error: CHANGELOG.md has no section for $version" >&2
	exit 1
fi

echo "wrote $out"
