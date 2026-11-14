# Legacy Dashboard Usage Recovery

## Scope

- Fix the admin dashboard regression introduced when lifetime totals switched
  from `usage_dashboard_daily` to raw requests plus durable compaction history.
- Preserve previously deleted requests' numeric totals still present in the old
  daily table. Do not restore or invent request identities or user dimensions.
- No production connection, deployment, database write or service restart.
- Existing `user_repo.go` and migration 255 changes belong to other work and
  were preserved.

## Implementation

Migration `256_preserve_legacy_dashboard_usage.sql` captures completed legacy
days once, subtracting the same whole-day `usage_statistics_range` source used
by current statistics. This includes uncompacted requests and archived request
counts, but excludes protected raw rows already represented by history.

Only positive missing request counts are recovered. Token and duration deltas
must be nonnegative; inconsistent data aborts and rolls back capture instead of
being clamped. Fees retain their signed decimal differences. Source and
represented numeric snapshots are retained for auditing.

The capture marker makes direct replay safe even after cache changes or an
initial capture with no missing days. Compaction and legacy cache writes cannot
race capture. The new durable table is independent of cache rebuild/retention.
Later legitimate backdated requests still add to totals, and subsequent
compaction does not change the recovered contribution.

Admin lifetime and range totals include the recovered delta. Historical range
queries include whole intersecting legacy days, like existing archived buckets.
Today's counters, active users, RPM/TPM, per-request records, user/API-key/model
breakdowns and balances are unchanged. Old site-only totals cannot be attributed
to those dimensions or reconstructed as exact hourly data.

## Rollout

Back up the deployment before upgrading. The migration runs automatically on
startup and must run before legacy daily buckets are rebuilt or deleted.

Use the original dashboard writer's timezone. `InitEnt` supplies the configured
application timezone to the migration connection; the supplied audit reconciles
the affected history with Asia/Shanghai boundaries. The old daily table itself
does not record its timezone, so a changed historical timezone cannot be inferred
from its dates. If the deployment timezone changed, apply the migration through
the runner on a connection with an explicit verified override first:

```sql
SET sub2api.legacy_dashboard_timezone = 'Asia/Shanghai';
```

Archived days must have the same actual start/end boundaries, irrespective of
timezone aliases. A boundary mismatch aborts capture and requires reconciliation
before retrying. Existing migration checksums are never changed.

After the upgrade, inspect the recovered contribution:

```sql
SELECT MIN(bucket_date), MAX(bucket_date), SUM(total_requests) AS requests,
       SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) AS tokens
FROM usage_dashboard_legacy_daily;
```

The supplied audit's expected missing contribution is 126,354 requests and
12,682,544,131 tokens. Add this to the current canonical totals; the audit's
approximately 46.91B figure is time-specific, not a hardcoded target.
Dashboard Redis cache may retain the old display until normal refresh.

Restoring original raw requests from backups later requires explicit
reconciliation of the corresponding fixed deltas; do not import both blindly.

## Verification

All database validation targeted newly created disposable local PostgreSQL 18
and Redis 8 containers bound to loopback, never production or an existing DB.

- Migration/target-gate unit tests passed.
- Focused repository/service dashboard and compaction unit tests passed.
- Repository integration tests compile with Go 1.27.4.
- Main recovery test reproduces the audited 126,354 requests / 12,682,544,131
  tokens, partial-day overlap, multi-request archive buckets, protected raw rows,
  signed decimal costs, site-only scope, late requests, replay, compaction,
  aggregate rebuild and retention.
- Safety subtests cover already represented/stale/open days, empty-capture
  replay, timezone aliases, original-timezone override, mismatched boundaries,
  rollback of invalid deltas and 23/25-hour daylight-saving days.
- Existing compaction replay/late-row and precision/rollback/worker-recovery
  tests passed alongside the new tests. The three existing dashboard regression
  tests passed on a separate clean database. Running them after committed decimal
  compaction fixtures exposed an existing exact-float equality assertion
  (`3.0200000004999996` versus `3.0200000005`); the assertion was not changed.
- SQL migration files remain LF-only; `git diff --check` passed.

Focused commands (Go 1.27.4, explicit isolated target gate for DB execution):

```powershell
go test -mod=readonly -p=2 ./migrations ./internal/repository/validation -count=1
go test -mod=readonly -p=2 -tags=unit ./internal/repository ./internal/service -run 'DashboardAggregation|UsageCompaction|UsageLogRepositoryGetStatsWithFilters' -count=1
go test -mod=readonly -p=2 -tags=integration -c -o repository.test.exe ./internal/repository
./repository.test.exe '-test.run=^TestDashboardLegacy|^TestUsageCompactionPreservesTotalsReplayAndLateRows$|^TestUsageCompactionPrecisionRollbackAndLeaseRecovery$' '-test.v=true' '-test.timeout=3m'
./repository.test.exe '-test.run=^TestUsageLogRepoSuite$/^TestDashboard' '-test.v=true' '-test.timeout=3m'
```

Production backfill and the observed dashboard display remain unverified until
the updated application is deployed.
