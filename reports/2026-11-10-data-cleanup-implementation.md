# Unified Data Cleanup And Durable Usage History

## Scope

- Implemented in `D:/Codex_Program/Personal_Sub2/main`; no commit, push or server deployment.
- Production SSH/database were not used during implementation. All database writes targeted the disposable local PostgreSQL container `sub2-cleanup-validation-20261110`, loopback port 55483.
- The concurrent retry/disconnect task owns `server_auto_retry.go`, gateway response/transport handling, `ops_error_logger.go`, `ops_repo.go`, `ops_service.go`, `ops_upstream_context.go`, the error table and its existing translations. Those edits were preserved and are not claimed as this task's work.

## Delivered

- Dedicated `/admin/data-cleanup` page with storage overview, cleanup/compaction, retention policies and task history.
- Removed cleanup controls from usage, runtime logs, operation audit, ops settings and prompt audit. Prompt audit filter cleanup is hosted by the center using its existing specialized API.
- Old raw usage task creation, runtime cleanup and audit-clear routes return 410 and direct callers to the unified preview flow. Existing task history/cancellation remains readable.
- Preview is signed and bound to normalized dates, maximum ID and ordered row-ID digests. Sensitive/all/usage operations require TOTP and reject admin API keys.
- Large deletion jobs and all usage compaction jobs use durable audit-backed batches. Progress and changes commit together; owner fencing protects multi-instance recovery; cancellation stops future batches.
- New migration `250_usage_compaction_history.sql` creates structures only. It does not compact or delete history at startup.
- Compaction preserves daily numeric dimensions, original decimal costs, request count, token categories and account-cost precision. Derived user-month, site-month, site-year and lifetime views do not maintain independent counters.
- Request identity tombstones survive raw deletion. Legacy, batch and modern billing paths consult archive protection; financial references retain their original usage rows.
- `usage_statistics` contains either unprocessed raw contributions or permanent numeric history, never both. User/API-key/global/group/model/endpoint/financial statistics use that source.
- Current natural month stays raw. Historical hour trends fall back to daily numeric history. Per-request details and exact historical percentiles cannot be recreated from numeric summaries.
- New automatic policies are opt-in; existing persisted policies are preserved. Usage retention compacts completed months rather than deleting values. General settings/ops saves no longer overwrite center retention settings.

## Verification

- Focused Go tests across service, repository, admin handlers, routes, middleware, server wiring and migrations passed. Commands used explicit `-run` filters and `-mod=readonly`.
- Full migration chain succeeded on two separately created empty PostgreSQL 18 databases. The final schema was tested through the real repository and worker paths.
- Three new PostgreSQL integration tests passed: totals/replay/late rows, queued jobs/cancellation, and decimal precision/rollback/stale-worker recovery. Protected billing evidence and current-month rows remained present.
- Frontend: `vue-tsc --noEmit`, focused ESLint, 53 tests in 9 files, and a production Vite build passed. Build output is outside the repository in `D:/Codex_Program/Personal_Sub2/cleanup-validation-20261110/frontend-dist`.
- Playwright screenshots checked desktop 1440x1000 and mobile 390x844, including storage and compaction confirmation. No page errors or document horizontal overflow. Visual data were browser-only fixtures; no cleanup action was executed through production or an actual user session.
- `git diff --check` passed.
- Expanded repository test run has one known failure: `TestBuildOpsErrorLogsWhere_CyberPolicyStatusExemption` expects the previous error guard, while the parallel task added `downstream_write_error`. The retry task's implementation was not reverted or changed to satisfy that assertion.

## Local Preview

- Frontend: `http://127.0.0.1:3087/admin/data-cleanup`, proxying only to the local backend on 8085.
- Backend: `http://127.0.0.1:8085/health`; database `sub2_cleanup_preview` and Redis DB 2 are disposable local fixtures.
- Preview account: `cleanup-admin@fixture.invalid`; password `CleanupPreviewOnly-2026!`. These are fixture credentials, not production credentials.
- First login may require the user's deployment compliance acknowledgement; it was not accepted on the user's behalf. Visual QA used browser-only fixture state for that overlay.
- Screenshots and validation config are in `D:/Codex_Program/Personal_Sub2/cleanup-validation-20261110`.

## Release Boundary

- No production migration, backfill, compaction, deletion, maintenance, restart or configuration change was performed.
- New migration 250 has not been released. Once applied to any shared deployment its bytes must remain immutable; further SQL changes require a new migration.
- Existing history that was already deleted cannot be reconstructed without reliable backups.
- Review and back up the deployment before rollout. Start with read-only preview, reconcile totals, then explicitly enable completed-month compaction. Database deletion reuses space internally and does not promise immediate file shrinkage.
