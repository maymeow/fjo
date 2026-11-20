#!/usr/bin/env bash
# Prints the CHANGELOG.md section of one version without its heading, for
# the GitHub release notes (.github/workflows/release.yaml).
# Usage: scripts/release-notes.sh v0.2.0 [CHANGELOG.md]
# Fails when the section is missing or empty.
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: $0 <version> [changelog]" >&2
	exit 2
fi
version="${1#v}"
changelog="${2:-CHANGELOG.md}"

notes="$(awk -v heading="## [${version}]" '
	/^## \[/ {
		if (found) exit
		if (index($0, heading) == 1) { found = 1; next }
	}
	/^\[[^]]+\]: / { if (found) exit }
	found { print }
' "$changelog" | sed -e '/./,$!d')"

if [ -z "$notes" ]; then
	echo "no release notes for ${version} in ${changelog}" >&2
	exit 1
fi
printf '%s\n' "$notes"
