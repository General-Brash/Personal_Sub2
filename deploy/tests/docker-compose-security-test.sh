#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_root"

check_application_security_opt() {
  file=$1
  count=$(
    awk '
      $0 == "  sub2api:" {
        in_application = 1
        next
      }
      in_application && $0 ~ /^  [A-Za-z0-9_-]+:$/ {
        in_application = 0
      }
      in_application && $0 == "    security_opt:" {
        in_security_opt = 1
        next
      }
      in_application && in_security_opt && $0 == "      - no-new-privileges:true" {
        count++
      }
      END { print count + 0 }
    ' "$file"
  )

  if [ "$count" -ne 1 ]; then
    printf '%s must enable no-new-privileges exactly once for the sub2api service\n' "$file" >&2
    exit 1
  fi
}

check_proxy_stream_circuit_disabled() {
  file=$1
  count=$(
    awk '
      $0 == "  sub2api:" {
        in_application = 1
        next
      }
      in_application && $0 ~ /^  [A-Za-z0-9_-]+:$/ {
        in_application = 0
      }
      in_application && $0 == "      - GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED=${GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED:-false}" {
        count++
      }
      END { print count + 0 }
    ' "$file"
  )

  if [ "$count" -ne 1 ]; then
    printf '%s must pass GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED with default false exactly once for the sub2api service\n' "$file" >&2
    exit 1
  fi
}

for compose_file in \
  deploy/docker-compose.yml \
  deploy/docker-compose.local.yml \
  deploy/docker-compose.standalone.yml \
  deploy/docker-compose.dev.yml
do
  check_application_security_opt "$compose_file"
  check_proxy_stream_circuit_disabled "$compose_file"
done

env_example_count=$(
  awk '
    { sub(/\r$/, "") }
    $0 == "GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED=false" { count++ }
    END { print count + 0 }
  ' deploy/.env.example
)
if [ "$env_example_count" -ne 1 ]; then
  printf 'deploy/.env.example must define GATEWAY_OPENAI_PROXY_STREAM_CIRCUIT_DISABLED=false exactly once\n' >&2
  exit 1
fi

printf 'docker compose security test passed\n'
