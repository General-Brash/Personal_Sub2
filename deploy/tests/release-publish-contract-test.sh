#!/bin/bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${TEST_DIR}/../.." && pwd)"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    local file="$1"
    local expected="$2"
    grep -Fq -- "$expected" "$file" || fail "${file} is missing '${expected}'"
}

assert_not_contains() {
    local file="$1"
    local unexpected="$2"
    if grep -Fq -- "$unexpected" "$file"; then
        fail "${file} still contains '${unexpected}'"
    fi
}

release="${REPO_DIR}/.github/workflows/release.yml"
personal="${REPO_DIR}/.github/workflows/publish-personal-ghcr.yml"
classifier="${REPO_DIR}/.github/workflows/publish-intent-classifier-ghcr.yml"
full="${REPO_DIR}/.goreleaser.yaml"
simple="${REPO_DIR}/.goreleaser.simple.yaml"

for file in "$release" "$personal" "$classifier" "$full" "$simple"; do
    [[ -f "$file" ]] || fail "missing release config ${file}"
done

assert_contains "$release" 'concurrency:'
assert_contains "$release" 'cancel-in-progress: false'
assert_contains "$release" 'EVENT_SHA: ${{ github.sha }}'
assert_contains "$release" 'if [[ "$target_commit" != "$EVENT_SHA" ]]'
assert_contains "$release" 'publish_moving_tags'
assert_contains "$release" 'PUBLISH_MOVING_TAGS: ${{ needs.resolve_release.outputs.publish_moving_tags }}'

assert_contains "$personal" 'type=raw,value=latest'
assert_contains "$personal" 'type=sha,prefix=sha-,format=long'
assert_not_contains "$personal" 'type=raw,value=${{ steps.version.outputs.value }}'

assert_contains "$classifier" 'concurrency:'
assert_contains "$classifier" 'cancel-in-progress: false'
assert_contains "$classifier" 'EVENT_SHA: ${{ github.sha }}'
assert_contains "$classifier" 'if [[ "$target_commit" != "$EVENT_SHA" ]]'
assert_contains "$classifier" "type=raw,value=latest,enable=\${{ needs.resolve_release.outputs.publish_moving_tags == 'true' }}"

ghcr_moving_skip="    skip_push: '{{ if ne .Env.PUBLISH_MOVING_TAGS \"true\" }}true{{ else }}false{{ end }}'"
dockerhub_moving_skip="    skip_push: '{{ if eq .Env.DOCKERHUB_USERNAME \"skip\" }}true{{ else if ne .Env.PUBLISH_MOVING_TAGS \"true\" }}true{{ else }}false{{ end }}'"
[[ "$(grep -Fc -- "$ghcr_moving_skip" "$full")" -eq 3 ]] || fail 'full GoReleaser config does not gate the three GHCR moving manifests'
[[ "$(grep -Fc -- "$dockerhub_moving_skip" "$full")" -eq 3 ]] || fail 'full GoReleaser config does not gate the three DockerHub moving manifests'
assert_contains "$full" 'sha-{{ .Commit }}'
assert_not_contains "$simple" 'ghcr.io/general-brash/personal_sub2:latest'
assert_contains "$simple" 'sha-{{ .Commit }}'

printf 'Release publication contract tests passed.\n'
