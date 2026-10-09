#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
checker="$repo_root/backend/scripts/check-migration-line-endings.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir "$tmp/lf" "$tmp/crlf" "$tmp/mixed" "$tmp/empty"
printf 'SELECT 1;\nSELECT 2;\n' > "$tmp/lf/001_test.sql"
printf 'SELECT 1;\r\nSELECT 2;\r\n' > "$tmp/crlf/001_test.sql"
printf 'SELECT 1;\nSELECT 2;\r\n' > "$tmp/mixed/001_test.sql"
sh "$checker" "$tmp/lf"
for kind in crlf mixed; do
    cp "$tmp/$kind/001_test.sql" "$tmp/$kind.before"
    if sh "$checker" "$tmp/$kind" > "$tmp/output" 2>&1; then
        printf 'FAIL: accepted %s migration\n' "$kind" >&2
        exit 1
    fi
    grep -F '001_test.sql' "$tmp/output" >/dev/null
    cmp "$tmp/$kind/001_test.sql" "$tmp/$kind.before"
done
for kind in empty missing; do
    if sh "$checker" "$tmp/$kind" > "$tmp/output" 2>&1; then
        printf 'FAIL: accepted %s migration directory\n' "$kind" >&2
        exit 1
    fi
done
# All supported source build paths must check before compiling/embedding SQL.
for file in Dockerfile deploy/Dockerfile backend/Dockerfile; do
    grep -F 'RUN sh ./scripts/check-migration-line-endings.sh' "$repo_root/$file" >/dev/null
done
for file in .goreleaser.yaml .goreleaser.simple.yaml; do
    grep -F '    - sh backend/scripts/check-migration-line-endings.sh' "$repo_root/$file" >/dev/null
done
grep -F 'sh ./scripts/check-migration-line-endings.sh' "$repo_root/backend/Makefile" >/dev/null
[ "$(grep -Fc 'sh ./scripts/check-migration-line-endings.sh' "$repo_root/deploy/Makefile")" -eq 2 ]
printf 'Migration build gate regression tests passed.\n'
