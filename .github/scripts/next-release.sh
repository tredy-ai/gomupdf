#!/usr/bin/env bash
# Prints the version tag HEAD should be released as, and writes its release
# notes. Prints nothing when HEAD already carries a version tag.
#
#   .github/scripts/next-release.sh <notes-file>
#
# The version comes from the commits since the latest version tag reachable
# from HEAD (Conventional Commits):
#
#   a breaking change (`type!:` or a `BREAKING CHANGE:` footer), or `feat`  -> minor
#   anything else                                                           -> patch
#
# The MAJOR version never comes from commits. A Go module's major version is
# part of its import path (v2 and later need a /vN suffix in go.mod), so a
# breaking change cannot simply become v2.0.0: nothing could import it. The
# major is the one go.mod declares; to start vN, change the module path to
# .../vN in a commit, and the next release is vN.0.0.
#
# With no version tag yet, the release is $INITIAL_VERSION.
#
# Environment: INITIAL_VERSION (default v1.2.0), MUPDF_VERSION (optional, for
# the notes), GITHUB_REPOSITORY (optional, for the compare link).
set -euo pipefail

notes=${1:?usage: next-release.sh <notes-file>}
initial=${INITIAL_VERSION:-v1.2.0}
version='^v[0-9]+\.[0-9]+\.[0-9]+$'

if git tag --points-at HEAD | grep -qE "$version"; then
	exit 0
fi

prev=$(git tag --merged HEAD --sort=-v:refname | grep -E "$version" | head -n 1 || true)
mod_major=$(sed -nE 's|^module .*/v([0-9]+)$|\1|p' go.mod)

breaking=() feats=() fixes=() others=()
if [ -n "$prev" ]; then
	while read -r sha; do
		subject=$(git show -s --format=%s "$sha")
		line="- $subject (${sha:0:7})"
		if [[ $subject =~ ^[a-z]+(\([^\)]*\))?!: ]] ||
			git show -s --format=%b "$sha" | grep -qE '^BREAKING[ -]CHANGE:'; then
			breaking+=("$line")
		elif [[ $subject =~ ^feat(\([^\)]*\))?: ]]; then
			feats+=("$line")
		elif [[ $subject =~ ^(fix|perf|revert)(\([^\)]*\))?: ]]; then
			fixes+=("$line")
		else
			others+=("$line")
		fi
	done < <(git rev-list --reverse "$prev..HEAD")
fi

if [ -z "$prev" ]; then
	next=$initial
else
	IFS=. read -r major minor patch <<<"${prev#v}"
	if [ -n "$mod_major" ] && ((mod_major > major)); then
		next="v$mod_major.0.0"
	elif ((${#breaking[@]} + ${#feats[@]} > 0)); then
		next="v$major.$((minor + 1)).0"
	else
		next="v$major.$minor.$((patch + 1))"
	fi
fi

section() { # section <title> <lines...>
	local title=$1
	shift
	(($# == 0)) && return 0
	printf '\n### %s\n\n' "$title"
	printf '%s\n' "$@"
}

{
	if [ -n "${MUPDF_VERSION:-}" ]; then
		printf 'Built and tested with MuPDF %s.\n' "$MUPDF_VERSION"
	fi
	if [ -z "$prev" ]; then
		printf '\nFirst release of the fork. [FORK.md](https://github.com/%s/blob/%s/FORK.md) lists every change from upstream, the behaviour differences and the migration notes.\n' "${GITHUB_REPOSITORY:-tredy-ai/gomupdf}" "$next"
	else
		section 'Breaking changes' "${breaking[@]}"
		section 'Features' "${feats[@]}"
		section 'Fixes' "${fixes[@]}"
		section 'Other changes' "${others[@]}"
		if [ -n "${GITHUB_REPOSITORY:-}" ]; then
			printf '\n**Full changes:** https://github.com/%s/compare/%s...%s\n' "$GITHUB_REPOSITORY" "$prev" "$next"
		fi
	fi
} >"$notes"

echo "$next"
