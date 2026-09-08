# Validation 0.2.1 code-first evidence

- 日期：2026-09-07（Asia/Shanghai）
- 负责人：validation
- 范围：A0-A7；不执行 B 阶段数据库验收
- 工作区：`D:/Codex_Program/Personal_Sub2/178-p1`
- 官方源码：`D:/Codex_Program/Personal_Sub2/sub2api-0.2.1/sub2api-0.2.1`
- 约束：未连接 DB/Redis，未运行容器、业务栈、自动迁移或真实上游支付；未提交、未推送；保留其他 Agent 与用户既有未提交差异。

## 1. 入口分类与安全命令

### CODE_ONLY

仅允许命中特定纯逻辑、SQL 静态合同、SQLmock 和目标保护测试。推荐命令（工作目录为 `backend`）：

```powershell
$env:SUB2API_TEST_TARGET = $null
$env:SUB2API_ALLOW_DB_EXECUTION = $null
go test ./internal/repository -run 'Test(ValidationHarness|IsMigrationChecksumCompatible|ValidateMigrationExecutionMode|ApplyMigrationsFS_UpstreamRequestIDIndexMigration_DropsInvalidIndexBeforeRetry)$' -count=1
```

集中脚本：

```powershell
powershell -NoProfile -File .\scripts\validation-code-only.ps1
```

脚本在发现 DB 目标或 DB 执行授权环境变量时立即拒绝；不执行 Docker、Compose、server、Redis、数据库或真实上游请求。

### DB_REQUIRED（本轮只编译，不运行）

- 所有 `//go:build integration` 测试。
- `backend/internal/repository/integration_harness_test.go` 的 `TestMain`、`testcontainers` PostgreSQL/Redis、`ApplyMigrations`、`integrationDB`、`integrationRedis`、`testEntClient`、`testEntTx`。
- 所有直接调用 `integrationDB`、`testEntClient`、`testRedis`、自动迁移、建表、清表/清理 SQL 的 repository integration 测试。
- 安全门禁：必须同时设置 `SUB2API_TEST_TARGET=dedicated-test-db` 与 `SUB2API_ALLOW_DB_EXECUTION=ALLOW` 才允许未来真实 DB 运行；缺失或错误目标会在 `TestMain` 退出码 2 拒绝。

### APP_REQUIRED / EXTERNAL_AUTH_REQUIRED

- APP_REQUIRED：server 启动、自动迁移、Compose、真实 Redis-backed 应用流、插件进程联调。
- EXTERNAL_AUTH_REQUIRED：真实 OAuth、支付 provider、回调、上游网络请求。

危险入口包括：`go test ./...`、无窄 `-run` 的 repository 测试、`go test -tags integration ...` 的实际执行、Docker/testcontainers/Compose、server 启动、自动迁移、真实 DB/Redis/支付/上游调用。

## 2. 逐文件裁决

| 文件 | 裁决 | 结果/依据 |
|---|---|---|
| `validation-scope.json` | 新增 | 固化唯一写入域、入口类别、危险入口和 DB compile-only 边界。 |
| `backend/migrations/232_add_usage_log_upstream_request_id.sql` | 官方原字节复制 | 与官方文件 354 bytes，逐字节一致；历史 SQL 未改。 |
| `backend/migrations/233_add_usage_log_upstream_request_id_index_notx.sql` | 官方原字节复制 | 与官方文件 241 bytes，逐字节一致；保留 `CREATE INDEX CONCURRENTLY`。 |
| `backend/migrations/234_channel_max_reasoning_effort_multiplier.sql` | 官方原字节复制 | 与官方文件 614 bytes，逐字节一致；保留约束与注释。 |
| `backend/migrations/234_group_codex_models_manifest_config.sql` | 官方原字节复制 | 与官方文件 392 bytes，逐字节一致。 |
| `backend/internal/repository/migrations_runner.go` | 最小追加 | 保留已有 226 逻辑；新增 233 索引常量及 `prepareNonTransactionalMigration` INVALID 索引恢复分支。 |
| `backend/internal/repository/migrations_runner_notx_test.go` | 最小追加 | 新增 233 INVALID 索引删除后重建的完整 SQLmock 路径；保留已有 migration mock。 |
| `backend/internal/repository/integration_harness_test.go` | 最小追加 | `TestMain` 启动任何容器/DB 前执行显式目标和执行授权门禁。 |
| `backend/internal/repository/migrations_runner_extra_test.go` | 最小追加 | 新增 232 与两条 234 事务 SQLmock 路径，和 233 非事务恢复路径组成四路径 SQLmock。 |
| `backend/internal/repository/validation_harness_test.go` | 新集中纯 mock helper/test | 覆盖缺失/错误目标拒绝、正确目标+显式授权接受、四条官方 SQL 合同片段。 |
| `backend/scripts/validation-code-only.ps1` | 新增安全脚本 | 先拒绝 DB 环境变量，再运行窄范围 CODE_ONLY 测试和 integration 测试二进制编译。 |
| `reports/validation.md` | 新增 | 本报告；记录分类、映射、验证、阻断和未完成项。 |
| `backend/internal/repository/fixtures_integration_test.go` | 保留现状 | 未做无必要改动；现有 fixture/helper 归 DB_REQUIRED，报告已完成夹具分类。 |

未修改业务 repo/service/schema/DTO；未修改全局 `go.mod`/`go.sum`；未安装系统工具；未提交或推送。

## 3. WP-10 / WP-12 结果

### WP-10

- 四个 SQL 已按官方源码原字节复制。
- 233 `idx_usage_logs_upstream_request_id` 已纳入非事务 migration 预处理：发现 `pg_index.indisvalid=false` 时先 `DROP INDEX CONCURRENTLY IF EXISTS`，随后执行官方 CREATE，再登记 migration。
- 四路径 SQLmock 覆盖：232 事务列新增、233 INVALID 索引删除后重建、234 channel multiplier 事务变更、234 group manifest 事务变更；每条均覆盖 checksum 查询、执行、记录 migration、提交/非事务登记与释放 advisory lock。
- 既有 226 两索引恢复逻辑及测试保留；未改历史 SQL。

### WP-12

- 已分类 `TestMain`、`init`、build tags、testcontainers、DSN/连接、自动迁移、schema create、清理 SQL、Redis helper。
- 集中入口拒绝缺失或错误显式目标；额外要求显式 `SUB2API_ALLOW_DB_EXECUTION=ALLOW`。
- DB_REQUIRED 测试只允许 `go test -c -tags integration` 编译，不允许运行。
- V00-V18 映射和夹具状态见下表；没有用 skip 冒充通过。

## 4. V00-V18 现有测试映射与夹具准备

| V-ID | 现有代码/夹具映射 | 本轮状态 |
|---|---|---|
| V00 | migration runner checksum/execution-mode、四 SQL contract、SQLmock；空库/故障副本/恢复需 DB | CODE_ONLY 部分完成；DB_REQUIRED 编译受共享依赖阻断 |
| V01 | Ent/schema/raw migration 与 repository integration fixtures | DB_REQUIRED；夹具入口已分类，未执行 |
| V02 | account/group/channel repository integration、`fixtures_integration_test.go` | DB_REQUIRED；未执行 |
| V03 | scheduler/account selection integration 与 Redis helper | DB_REQUIRED；未执行 |
| V04 | group manifest/account fixture 与 repository integration | DB_REQUIRED；未执行 |
| V05 | upstream request-id 纯逻辑/SQL contract、usage-log repository | CODE_ONLY contract 已准备；持久化路径 DB_REQUIRED |
| V06 | gateway/protocol/stream/WS tests 与 usage fixture | APP/DB_REQUIRED；未执行 |
| V07 | continuation/session/replay/slot release tests | APP/DB_REQUIRED；未执行 |
| V08 | upstream ID header、usage-log SQL、查询/复制 integration | CODE_ONLY SQL/runner 已准备；真实持久化 DB_REQUIRED |
| V09 | pricing/service pure tests、channel pricing migration、usage stats | CODE_ONLY 取决于 service 编译；DB 字段验收未执行 |
| V10 | Personal billing/temporary credit/usage repository fixtures | APP/DB_REQUIRED；未执行 |
| V11 | payment/refund/rebate/check-in fixtures；真实 provider 禁止 | EXTERNAL_AUTH_REQUIRED/DB_REQUIRED；未执行 |
| V12 | image safety pure tests 与 gateway/upstream integration | CODE_ONLY 可独立；上游/APP 路径未执行 |
| V13 | plugin protocol/bridge tests 与进程联调 | APP_REQUIRED/EXTERNAL_AUTH_REQUIRED；未执行 |
| V14 | classifier config/Compose/health dependency tests | APP_REQUIRED；未执行 |
| V15 | frontend/UI/API persistence path | APP_REQUIRED/DB_REQUIRED；未执行 |
| V16 | all migrations/Ent/catalog/constraints/indexes | DB_REQUIRED；只保留代码准备，未执行 |
| V17 | transaction failure、226/233 INVALID index、cleanup/retry | 233 SQLmock 已准备；真实故障副本未执行 |
| V18 | advisory lock/restart/recovery/candidate consistency | DB_REQUIRED/APP_REQUIRED；未执行 |

夹具结论：现有 `fixtures_integration_test.go` 与 `integration_harness_test.go` 已作为 DB_REQUIRED 入口纳入集中分类；本轮不新增会连接数据库的空壳测试，不把 `Skip` 或未运行当作通过。

## 5. 真实验证记录

### 已执行

1. 官方四个 SQL 与工作区目标文件逐字节比较：**PASS**。
   - 232：354 / 354 bytes，ByteExact=True。
   - 233：241 / 241 bytes，ByteExact=True。
   - 234 channel：614 / 614 bytes，ByteExact=True。
   - 234 group：392 / 392 bytes，ByteExact=True。
2. `go test ./migrations -run 'TestDoesNotExist' -count=1`：**PASS**（包编译通过，无匹配测试执行）。
3. `gofmt`：**PASS**，目标 Go 文件格式化完成。
4. `git diff --check`（本轮目标 Go 文件）：**PASS**。

### 未通过/被阻断

1. 最新 `go test -c -tags integration ./internal/repository -o <temp binary>` 在包 setup 阶段先被共享工作区文件 `backend/internal/repository/usage_log_repo_upstream_request_id_unit_test.go:4` 的语法错误阻断：`expected ';', found `n\t"strings"``。该文件不属于本 validation 写入域，未修改。

2. `go test ./internal/repository -run 'Test(ValidationHarness|IsMigrationChecksumCompatible|ValidateMigrationExecutionMode|ApplyMigrationsFS_UpstreamRequestIDIndexMigration_DropsInvalidIndexBeforeRetry)$' -count=1`：**BLOCKED**，未进入测试执行；共享工作区 service 编译错误：
   - `codexModelMetadataOverride` 未定义；
   - `UpstreamModelMetadata` 未定义；
   - `configuredCodexModelDescriptor` 未定义；
   - `OpsUpstreamErrorEvent.ProxyID/ProxyName` 缺失；
   - `opsUpstreamProxyAttribution` 未定义；
   - `classifyUpstreamTransportError` 未定义。
3. `go test -run '^$' ./internal/repository -count=1`：**BLOCKED**，同上；`-run '^$'` 仍须先编译整个包，不能绕过缺失依赖。
4. `go test -c -tags integration ./internal/repository -o <temp binary>`：**BLOCKED**，同上；只尝试编译，未运行、未启动 TestMain、未启动容器/DB/Redis。

阻断来自共享工作区其他 Agent 的未完成 service 融合，不在本 validation 写入域内；本轮未擅自修改这些 service 文件。

## 6. 未完成项与边界

- 真实 DB migration、空库、故障副本、恢复、重启/锁、Redis、APP、Compose、支付和上游测试均未执行，必须由 B 阶段在明确测试目标和授权后执行。
- repository 纯 mock 测试因共享 service 编译错误尚未获得运行时 PASS；其测试代码已写入并经过 gofmt，不能宣称已通过。
- V00-V18 只有标为 CODE_ONLY 的静态准备/可编译意图，DB_REQUIRED、APP_REQUIRED、EXTERNAL_AUTH_REQUIRED 项均未宣称通过。
- 工作区仍存在其他 Agent 的大量 tracked/untracked 差异；本轮未覆盖、回滚、整理、提交或推送。
