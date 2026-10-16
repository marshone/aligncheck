#!/usr/bin/env bash
# next-version.sh staging <major|minor|patch>   next release candidate, vX.Y.Z-rc.N
# next-version.sh main                          stable version of the latest RC, vX.Y.Z
#
# Used by promote-branch.yml (preview) and release-management.yml (tagging),
# so the version announced in a promotion PR is the one that gets tagged.
set -euo pipefail

target="${1:?usage: next-version.sh staging <major|minor|patch> | main}"

stable_re='^v[0-9]+\.[0-9]+\.[0-9]+$'
rc_re='^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$'

case "$target" in
staging)
	bump="${2:-patch}"
	latest_stable="$(git tag -l 'v*' | grep -E "$stable_re" | sort -V | tail -n1 || true)"
	IFS=. read -r major minor patch <<<"${latest_stable:-v0.0.0}"
	major="${major#v}"
	case "$bump" in
	major) major=$((major + 1)); minor=0; patch=0 ;;
	minor) minor=$((minor + 1)); patch=0 ;;
	patch) patch=$((patch + 1)) ;;
	*) echo "unknown bump: $bump" >&2; exit 2 ;;
	esac
	base="v$major.$minor.$patch"

	# Within a cycle, never go backwards: if an unreleased RC already targets
	# a higher version (e.g. a minor bump), keep building toward that version.
	latest_rc="$(git tag -l 'v*-rc.*' | grep -E "$rc_re" | sort -V | tail -n1 || true)"
	rc_base="${latest_rc%-rc.*}"
	if [ -n "$latest_rc" ] && ! git rev-parse -q --verify "refs/tags/$rc_base" >/dev/null &&
		[ "$(printf '%s\n%s\n' "$base" "$rc_base" | sort -V | tail -n1)" = "$rc_base" ]; then
		base="$rc_base"
	fi

	n="$(git tag -l "$base-rc.*" | grep -cE "$rc_re" || true)"
	echo "$base-rc.$((n + 1))"
	;;
main)
	latest_rc="$(git tag -l 'v*-rc.*' | grep -E "$rc_re" | sort -V | tail -n1 || true)"
	if [ -z "$latest_rc" ]; then
		echo "no release candidate found: promote develop to staging first" >&2
		exit 1
	fi
	echo "${latest_rc%-rc.*}"
	;;
*)
	echo "unknown target: $target" >&2
	exit 2
	;;
esac
