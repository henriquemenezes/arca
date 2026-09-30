#!/usr/bin/env bash
# Move [Unreleased] into a numbered section, ready for a tag.
#
# Mechanical only: the heading, its date, and the link references at the foot of
# the file. The prose is never touched — it is the part a person reviewed, and
# scripts/release-notes.sh publishes it verbatim as the release notes.
#
# It does not commit and it does not tag. Review the diff, commit it, tag it.
set -euo pipefail

version="${1:?usage: release-prep.sh VERSION [DATE]}"
version="${version#v}"
date="${2:-$(date +%F)}"

case "$version" in
[0-9]*.[0-9]*.[0-9]*) ;;
*)
	echo "error: '$version' is not a version like 0.2.0" >&2
	exit 1
	;;
esac

if ! grep -q '^## \[Unreleased\]' CHANGELOG.md; then
	echo "error: CHANGELOG.md has no [Unreleased] section to move" >&2
	exit 1
fi

if grep -q "^## \[$version\]" CHANGELOG.md; then
	echo "error: CHANGELOG.md already has a section for $version" >&2
	exit 1
fi

# The base URL is read back out of the file rather than written down twice.
repo=$(sed -nE 's#^\[Unreleased\]: (https://[^/]+/[^/]+/[^/]+)/.*#\1#p' CHANGELOG.md | head -1)
if [ -z "$repo" ]; then
	echo "error: CHANGELOG.md has no [Unreleased] link reference to learn the URL from" >&2
	exit 1
fi

tmp=$(mktemp)
out=$(mktemp)
trap 'rm -f "$tmp" "$out"' EXIT

# Releasing an empty section would publish an empty release note, which is the
# one failure this whole path exists to prevent. Asking release-notes.sh keeps
# the two scripts agreeing on what counts as a section.
if ! ./scripts/release-notes.sh Unreleased "$tmp" >/dev/null 2>&1; then
	echo "error: [Unreleased] is empty; there is nothing to release" >&2
	exit 1
fi

# The newest numbered heading is the previous release, and the first release has
# none - its link points at the tag rather than at a comparison.
previous=$(sed -nE 's/^## \[([0-9]+\.[0-9]+\.[0-9]+)\].*/\1/p' CHANGELOG.md | head -1)

if [ -n "$previous" ] &&
	[ "$(printf '%s\n%s\n' "$previous" "$version" | sort -V | tail -1)" != "$version" ]; then
	echo "error: $version does not come after $previous" >&2
	exit 1
fi

awk -v v="$version" -v d="$date" -v prev="$previous" -v repo="$repo" '
	$0 == "## [Unreleased]" {
		print
		print ""
		print "## [" v "] - " d
		next
	}
	/^\[Unreleased\]: / {
		print "[Unreleased]: " repo "/compare/v" v "...HEAD"
		if (prev == "")
			print "[" v "]: " repo "/releases/tag/v" v
		else
			print "[" v "]: " repo "/compare/v" prev "...v" v
		next
	}
	{ print }
' CHANGELOG.md >"$out"

cp "$out" CHANGELOG.md
echo "CHANGELOG.md: [Unreleased] is now [$version] - $date"
