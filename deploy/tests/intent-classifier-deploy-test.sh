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
    assert_contains "${block}" 'ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.6-P1.2'
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
grep -q '^INTENT_CLASSIFIER_IMAGE=ghcr.io/general-brash/personal_sub2-intent-classifier:v0.1.6-P1.2$' \
    "${DEPLOY_DIR}/.env.example" \
    || fail '.env.example is missing the versioned GHCR classifier image'
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
    "- 'v*'" \
    'workflow_dispatch:' \
    'contents: read' \
    'packages: write' \
    'ghcr.io/general-brash/personal_sub2-intent-classifier' \
    'context: ./services/intent-classifier' \
    'platforms: linux/amd64,linux/arm64' \
    'type=raw,value=${{ env.VERSION }}' \
    'type=raw,value=latest' \
    'type=raw,value=sha-${{ steps.source.outputs.sha }}' \
    'provenance: false' \
    'sbom: false'; do
    grep -Fq -- "${expected}" "${workflow}" \
        || fail "classifier publish workflow is missing ${expected}"
done

if grep -Eq 'model\.onnx|intent-models|/models' "${workflow}"; then
    fail 'classifier publish workflow must not package model data'
fi

bash -n "${DEPLOY_DIR}/docker-deploy.sh"

printf 'Intent classifier deployment contract tests passed.\n'
