# IP Node Traffic Usage

## Delivered

- Added a Traffic Usage column next to the node name in IP Management.
- Today uses MB below 1024 MB and G at or above that boundary; the last 30 days always uses G. Both show two decimal places.
- Counts uploaded and downloaded socket bytes, including proxy/TLS/HTTP/WebSocket framing, compressed traffic, failed requests and retries. No balance deductions or pricing changes.
- Covers shared HTTP transports, SOCKS, TLS fingerprint proxy dialers, and OpenAI/Grok/live WebSocket paths. Direct traffic is not assigned to a managed proxy.
- Day boundaries follow the configured application timezone. The last 30 days includes today and the previous 29 calendar days.
- Independent migration `252_proxy_traffic_daily.sql` preserves node totals when request logs are cleaned or compacted. No existing migration was edited.
- Per-process absolute counters are flushed every five seconds and on list reads. Idempotent snapshots prevent double counting after an uncertain commit; separate process IDs support multiple instances and restarts.
- Shutdown closes observed connections, waits for in-flight byte observations, then flushes before database shutdown.
- Duplicate route identities use account/node attribution and separate connection pools. Route refresh does not block socket I/O; unresolved routes retain pending bytes until background refresh.
- Stats failures are displayed as unavailable, not misleading zero usage.

## Verification

- New network, service, SQLmock and handler tests passed, including partial writes, midnight boundaries, concurrency, uncertain commits, duplicate nodes, compressed responses, SSE, SOCKS, TLS/HTTP2, rejected TLS fingerprint handshakes and compressed WebSockets.
- Existing focused proxy, HTTP transport, HTTP2 keepalive and application cleanup tests passed.
- Backend application compiled; frontend typecheck, focused ESLint, 36 tests across five files and a production Vite build passed.
- Network utility lint passed. Broader backend lint remains blocked by existing data-cleanup test compilation errors; the production-only lint run reported existing data-cleanup/import and unused-code issues outside this feature.
- Service/handler/server tests used a Go overlay outside the repository to exclude the pre-existing `usage_compaction_test.go` compile error (`AuditLogService.runRetentionOnce` is missing). The source file was not modified.
- Browser screenshots verified 320, 390, 768, 1024 and 1440 px widths, including sticky-column visibility; no document horizontal overflow or page errors.
- `git diff --check` passed. Dependencies and lockfiles were not changed.
- No real database migration or integration test was executed: Docker is unavailable and no disposable PostgreSQL instance was started. No production server, account or node was accessed.

## Activation And Limits

- Deploy the updated backend and frontend; the existing startup migration runner applies migration 252. Traffic begins accumulating after activation; historical unrecorded bytes cannot be reconstructed.
- An abrupt process kill can lose bytes not yet flushed (normally up to five seconds; longer during database outages). This is a traffic meter, not a lossless financial billing ledger.
- Socket-byte totals do not include operating-system TCP/IP packet headers or retransmissions and may differ from an ISP invoice. Transfers that do not use a managed proxy are outside node totals.
- Identical proxy routes without account/node identity (for example anonymous probes) cannot be assigned uniquely and are not arbitrarily attributed to one of the duplicate nodes.
- Existing unrelated workspace changes were preserved. No commit, push or deployment was performed.

## Local Preview

- Read-only browser fixtures: `http://127.0.0.1:3091/admin/proxies`.
- Preview script, test overlay, build output and screenshots: `D:/Codex_Program/Personal_Sub2/traffic-validation-20261113`.
- The preview API is bound to loopback and rejects all non-GET requests. Fixture authentication is injected only by the standalone preview script, never by production application code.
