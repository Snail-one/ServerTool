#!/usr/bin/env bash

set -euo pipefail

RELEASE_TAG="${RELEASE_TAG:?RELEASE_TAG is required}"
TARGET_SHA="${TARGET_SHA:-HEAD}"
REPOSITORY="${GITHUB_REPOSITORY:-Snail-one/ServerTool}"
OUTPUT_PATH="${1:-release-notes.md}"

if git rev-parse --verify --quiet "refs/tags/${RELEASE_TAG}" >/dev/null; then
	TARGET="refs/tags/${RELEASE_TAG}"
else
	TARGET="$TARGET_SHA"
fi

PREVIOUS_TAG="$(
	git tag --merged "$TARGET" --list 'v*' --sort=-version:refname |
		awk -v current="$RELEASE_TAG" '$0 != current && !found { print; found = 1 }'
)"

if [ -n "$PREVIOUS_TAG" ]; then
	RANGE="${PREVIOUS_TAG}..${TARGET}"
else
	RANGE="$TARGET"
fi

{
	printf '## 本次更新\n\n'
	FIRST_COMMIT="$(git rev-list --max-count=1 --no-merges "$RANGE")"
	if [ -n "$FIRST_COMMIT" ]; then
		git log "$RANGE" --no-merges --format='%H%x09%s' --no-decorate |
			while IFS=$'\t' read -r commit subject; do
				short_commit="${commit:0:7}"
				subject="$(printf '%s' "$subject" | sed -e 's/^[[:space:]]*//' -e 's/^[•*-][[:space:]]*//')"
				if [ -z "$subject" ]; then
					subject="无标题提交"
				fi
				printf -- '- %s ([`%s`](https://github.com/%s/commit/%s))\n' \
					"$subject" "$short_commit" "$REPOSITORY" "$commit"
			done
	else
		printf '本版本未检测到新的提交。\n'
	fi
} >"$OUTPUT_PATH"

printf 'Release notes generated: %s\n' "$OUTPUT_PATH"
