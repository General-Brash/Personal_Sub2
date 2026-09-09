#!/bin/bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
REPO_DIR="$(cd "${DEPLOY_DIR}/.." && pwd)"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    local text="$1"
    local expected="$2"
    [[ "${text}" == *"${expected}"* ]] || fail "Missing '${expected}'"
}

for compose in \
    docker-compose.yml \
    docker-compose.local.yml \
    docker-compose.standalone.yml; do
    path="${DEPLOY_DIR}/${compose}"
    block=$(awk '
        /^  sub2api:$/ { capture = 1 }
        capture && /^  [A-Za-z0-9_-]+:$/ && $0 != "  sub2api:" { exit }
        capture { print }
    ' "${path}")

    [[ -n "${block}" ]] || fail "${compose} has no sub2api service"
    assert_contains "${block}" 'image: ghcr.io/general-brash/personal_sub2:${SUB2API_IMAGE_TAG:-latest}'
    if [[ "${block}" == *'weishaw/sub2api'* ]]; then
        fail "${compose} uses the official sub2api image"
    fi
done

for compose in \
    docker-compose.yml \
    docker-compose.dev.yml \
    docker-compose.local.yml \
    docker-compose.standalone.yml; do
    path="${DEPLOY_DIR}/${compose}"
    block=$(awk '
        /^  intent-classifier:$/ { capture = 1 }
        capture && /^  [A-Za-z0-9_-]+:$/ && $0 != "  intent-classifier:" { exit }
        capture { print }
    ' "${path}")

    [[ -n "${block}" ]] || fail "${compose} has no intent-classifier service"
    assert_contains "${block}" 'ghcr.io/general-brash/personal_sub2-intent-classifier:${SUB2API_IMAGE_TAG:-latest}'
    assert_contains "${block}" 'target: /models'
    assert_contains "${block}" 'read_only: true'
    assert_contains "${block}" 'intent_classifier_state:/state'
    assert_contains "${block}" 'INTENT_CLASSIFIER_STATE_DIR=/state'
    assert_contains "${block}" 'INTENT_CLASSIFIER_ADMIN_TOKEN'
    assert_contains "${block}" 'INTENT_CLASSIFIER_INFERENCE_TIMEOUT_MS'
    assert_contains "${block}" '/health/live'
    assert_contains "${block}" 'expose:'
    if grep -q '^    ports:' <<<"${block}"; then
        fail "${compose} publishes an intent-classifier host port"
    fi
done

grep -q '^INTENT_CLASSIFIER_MODEL_DIR=./intent-models$' "${DEPLOY_DIR}/.env.example" \
    || fail '.env.example is missing the model directory'
grep -q '^INTENT_CLASSIFIER_IMAGE=$' \
    "${DEPLOY_DIR}/.env.example" \
    || fail '.env.example is missing the classifier image override slot'
grep -q '^INTENT_CLASSIFIER_ADMIN_TOKEN=' "${DEPLOY_DIR}/.env.example" \
    || fail '.env.example is missing the admin token'
grep -q '^INTENT_CLASSIFIER_INFERENCE_TIMEOUT_MS=250$' "${DEPLOY_DIR}/.env.example" \
    || fail '.env.example is missing the bounded inference timeout'
grep -q '^INTENT_CLASSIFIER_STATE_DIR=/var/lib/sub2api/intent-classifier-state$' \
    "${DEPLOY_DIR}/intent-classifier.env.example" \
    || fail 'systemd environment is missing the state directory'
grep -q '^ReadOnlyPaths=/var/lib/sub2api/intent-models$' "${DEPLOY_DIR}/intent-classifier.service" \
    || fail 'systemd unit does not protect the model directory'
grep -q '^ReadWritePaths=/var/lib/sub2api/intent-classifier-state$' "${DEPLOY_DIR}/intent-classifier.service" \
    || fail 'systemd unit does not allow persistent state writes'

grep -q 'install /import/package --models-dir /models' "${DEPLOY_DIR}/INTENT_CLASSIFIER.md" \
    || fail 'runbook is missing the one-off install command'

for command in validate preload activate list rollback; do
    grep -q "intent-classifier ${command}" "${DEPLOY_DIR}/INTENT_CLASSIFIER.md" \
        || fail "runbook is missing the ${command} command"
done

workflow="${REPO_DIR}/.github/workflows/publish-intent-classifier-ghcr.yml"
[[ -f "${workflow}" ]] || fail 'classifier publish workflow is missing'
for expected in \
    'workflow_call:' \
    'workflow_dispatch:' \
    'contents: read' \
    'packages: write' \
    'ghcr.io/general-brash/personal_sub2-intent-classifier' \
    'context: ./services/intent-classifier' \
    "platforms: \${{ inputs.simple_release && 'linux/amd64' || 'linux/amd64,linux/arm64' }}" \
    'type=raw,value=${{ steps.source.outputs.version }}' \
    'type=raw,value=sha-${{ steps.source.outputs.sha }}' \
    'provenance: false' \
    'sbom: false'; do
    grep -Fq -- "${expected}" "${workflow}" \
        || fail "classifier publish workflow is missing ${expected}"
done

# The release workflow owns tag pushes so the classifier is published once,
# only after the same source has passed CI and security gates.
if grep -Eq '^  push:' "${workflow}"; then
    fail 'classifier must not publish independently on a tag push'
fi
release_workflow="${REPO_DIR}/.github/workflows/release.yml"
for expected in \
    "- 'v*'" \
    'needs: [resolve-target, backend-ci, security-scan]' \
    'uses: ./.github/workflows/publish-intent-classifier-ghcr.yml'; do
    grep -Fq -- "${expected}" "${release_workflow}" \
        || fail "release workflow is missing classifier gate ${expected}"
done

# A classifier rerun must not silently roll a previous release back to latest.
grep -Fq 'latest=false' "${workflow}" \
    || fail 'classifier publish workflow must disable automatic latest tags'
grep -Fq 'PROMOTE_LATEST: ${{ inputs.promote_latest }}' "${workflow}" \
    || fail 'classifier publish workflow must use the explicit latest promotion input'
grep -Fq 'if [[ "${PROMOTE_LATEST}" == "true" ]]; then' "${workflow}" \
    || fail 'classifier publish workflow must guard the latest tag with explicit promotion'
grep -Fq '${IMAGE_NAME}:latest' "${workflow}" \
    || fail 'classifier publish workflow is missing the explicit latest promotion tag'

if grep -Eq 'model\.onnx|intent-models|/models' "${workflow}"; then
    fail 'classifier publish workflow must not package model data'
fi

bash -n "${DEPLOY_DIR}/docker-deploy.sh"

printf 'Intent classifier deployment contract tests passed.\n'
