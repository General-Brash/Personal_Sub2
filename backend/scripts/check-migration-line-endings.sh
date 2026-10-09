#!/bin/sh
# Migration checksums include internal line endings. Never normalize at runtime
# or silently rewrite applied SQL: reject a noncanonical build input instead.
set -eu
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
MIGRATIONS_DIR="${1:-$SCRIPT_DIR/../migrations}"
found=0
failed=0
for file in "$MIGRATIONS_DIR"/*.sql; do
    [ -f "$file" ] || continue
    found=1
    # Inspect raw bytes: Git for Windows grep can silently strip CR in text mode.
    bytes="$(LC_ALL=C od -An -v -t x1 "$file")"
    case " $bytes " in
        *" 0d"*)
            printf 'ERROR: migration contains CR/CRLF bytes: %s\n' "$file" >&2
            failed=1
            ;;
    esac
done
if [ "$found" -eq 0 ]; then
    printf 'ERROR: no SQL migrations found in %s\n' "$MIGRATIONS_DIR" >&2
    exit 1
fi
if [ "$failed" -ne 0 ]; then
    printf 'Build refused. Use the LF bytes from Git (for example git archive); do not change database checksums.\n' >&2
    exit 1
fi
printf 'Migration line endings verified: LF only.\n'
