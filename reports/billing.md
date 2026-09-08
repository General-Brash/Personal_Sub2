# Billing / Personal 融合报告

- 日期：2026-09-07
- 工作区：`D:/Codex_Program/Personal_Sub2/178-p1`
- 范围：WP05 usage upstream request id、WP06 service-tier/reasoning/pricing/usage、WP07 支付补偿，以及允许范围内的 0.2.0 缺口。
- 运行限制：未连接 DB/Redis，未启动容器/应用栈，未执行真实外部请求或真实支付；未提交。
- 基线证据：`D:/Codex_Program/Personal_Sub2/fusion-0.2.1-code-evidence/run-2026-09-07T05-24-25-962Z`

## 逐文件裁决与实现

| 文件 | 裁决 / 实际修改 |
|---|---|
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/service_tier_billing.go` | MERGE_REQUIRED：补齐 OpenAI OAuth-like `default` observed 非权威例外；补充 `OpenAIFastTierUltrafast = "ultrafast"` 基础常量；保留 requested/observed/billable 的不升价规则。由于协议负责人尚未提供 `ForwardResult` 的 observed tier 字段，本文件未伪造共享结构字段。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/billing_service.go` | MERGE_REQUIRED：`CostInput.ReasoningEffort` 传递；`ModelPricing.MaxReasoningEffortMultiplier`；max 只在最终 effort 为 `max` 时应用一次；nil/1/>0/NaN/Inf 运行时防御；统一/legacy/no-resolver 路径均接入。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/billing_token_cost_request.go` | MERGE_REQUIRED：`TokenCostRequest.ReasoningEffort` 写入 `CostInput`，无 resolver 路径也应用 max multiplier。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_gateway_usage.go` | MERGE_REQUIRED：最终 `result.ReasoningEffort` 传入 OpenAI token 成本计算；HTTP 响应按账户配置头名提取 `UpstreamRequestID`；WS 轮次保持 nil。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/gateway_usage_billing.go` | MERGE_REQUIRED：通用 Gateway token 计费请求传递最终 reasoning effort；保留现有 Personal 余额/quota/usage dedup 流程。由于 `ForwardResult.UpstreamHeaders` 尚未由协议负责人提供，generic upstream request id 写入仍待协议契约闭合。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/channel.go` | MERGE_REQUIRED：补充 `MaxReasoningEffortMultiplier` service 层字段。未修改 DTO/handler/schema。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/model_pricing_resolver.go` | MERGE_REQUIRED：渠道 max multiplier 进入 resolved base pricing。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/channel_service.go` | MERGE_REQUIRED：校验 fast/flex/max multiplier；nil 合法，1 和正数合法，0/负数/NaN/Inf 非法。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/billing_pricing_preflight.go` | MERGE_REQUIRED：预检 token pricing 时校验 max multiplier，拒绝非法数值。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/pricing_service.go` | MERGE_REQUIRED：增加 fallback/override 联合指纹、hash check 周期热重载、完整快照重建。文件删除在下一次 hash check 时按空层重建并移除条目；文件存在但 JSON 非法时拒绝提交新快照并保留旧有效快照。override 支持字段浅合并及 null 删除。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/usage_log.go` | MERGE_REQUIRED：增加 `UpstreamRequestID *string`，保留 requested/effective effort 与现有 usage 字段。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/upstream_request_id.go` | MERGE_REQUIRED：只读取账户明确配置的响应头名；未配置为空；截断至 128 字节且保持 UTF-8；WS 返回 nil；校验 header 名称。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/repository/usage_log_repo_insert.go` | MERGE_REQUIRED：raw insert、批量 insert、best-effort insert、prepared args 全部加入 `upstream_request_id`，并保持参数/列顺序一致。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/repository/usage_log_repo_query.go` | MERGE_REQUIRED：raw select columns、scan、UsageLog 回填加入 `upstream_request_id`。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/repository/usage_log_repo_upstream_request_id_unit_test.go` | TEST_ONLY_REQUIRED：纯 shape 测试，验证 insert 参数和 select 列存在且唯一；已修复 PowerShell `` `n`` / `$got` 残留。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/pricing_service_hot_reload_test.go` | TEST_ONLY_REQUIRED：纯临时文件测试删除层移除条目、非法 JSON 保留旧快照。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/billing_max_reasoning_effort_multiplier_test.go` | TEST_ONLY_REQUIRED：覆盖最终 effort、nil/1/>0/0/负数/NaN/Inf。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/upstream_request_id_test.go` | TEST_ONLY_REQUIRED：覆盖显式 header、未配置、UTF-8 截断、WS nil、header 名校验。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/payment_order_lifecycle.go` | MERGE_REQUIRED：增加统一 `ReconcilePendingPaymentOrders`，查询 Alipay 与 Wxpay；保留旧 `ReconcilePendingWxpayOrders` 包装入口；未删除 Personal 取消、支付实例绑定、库存/商城履约、退款/审计语义。 |
| `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/payment_order_expiry_service.go` | MERGE_REQUIRED：到期调度切换到统一 Alipay/Wxpay 主动补偿。 |

## 费用可复算样例

### max multiplier

以基础 token 费用 `input=1.00`、`output=2.00`、`total=3.00` 为例：

- 最终 effort=`high`：倍率不触发，费用 `3.00`。
- 最终 effort=`max`，配置 nil：倍率不触发，费用 `3.00`。
- 最终 effort=`max`，配置 `1`：费用 `3.00`。
- 最终 effort=`max`，配置 `2.5`：费用 `3.00 × 2.5 = 7.50`。
- 配置 `0`、负数、NaN、Inf：预检拒绝；运行时防御回退为 `1.0`，不产生异常账单。
- 已应用 max multiplier 后不会再次叠加到同一 breakdown。

### service tier

以 requested=`priority`、observed=`default` 为例：

- API key：billable=`default`，允许降价。
- OpenAI OAuth-like/Codex：observed `default` 视为非权威，billable 保持 requested=`priority`。
- observed=`priority` 或未知 tier：不提升原始账单。
- `ultrafast` 现作为独立合法 tier 常量，具体 ForwardResult observed/response 贯通待协议负责人字段落地。

### pricing hot reload

- override 存在且合法：覆盖目录/fallback 同名字段。
- override 文件删除：下一次 hash check 重新构建完整快照，覆盖条目回到目录/fallback值或被移除。
- override 文件存在但 JSON 非法：本次 reload 不提交新快照，保留旧有效数据。

## 测试真实结果

### PASS

```text
go test -mod=readonly ./internal/pkg/usagestats -run '^Test' -count=1
ok   github.com/Wei-Shaw/sub2api/internal/pkg/usagestats  0.230s
```

```text
gofmt -d <本轮实际修改文件>
git diff --check
```

两项均无格式错误或 diff whitespace 错误；Git 的 CRLF/LF warning 来自既有工作树文件，不是测试失败。

### BLOCKED

```text
go test -mod=readonly ./internal/service -run '^TestDoesNotExist$' -count=1
```

该命令用于安全编译审查，未运行测试；仍被工作区既存、范围外基础契约阻断，包括：

- `observedUpstreamResponseServiceTier`
- `GrokSupportsXHighReasoningEffort`
- `OpenAIWSStateStore.MarkSessionInvalidEncryptedContent`
- `OpenAIWSStateStore.HasAnySessionInvalidEncryptedContent`
- `shouldStripOpenAIResponsesNonPairCallID`
- `normalizeOpenAIResponseFormatSchemas`
- `sanitizeOpenAIResponsesToolSchemasForPlatform`
- `NormalizeCompactionTriggerInputOrder`

这些属于协议/Gateway/WS/非 billing 基础契约，用户明确要求不要越界修复，因此 service/repository package 级测试不能真实执行。未把“编译启动”或“未看到本轮错误”冒充 PASS。

## 未完成项 / 需要主负责人或协议负责人闭合

1. `ForwardResult` 尚未提供协议负责人承诺的 `UpstreamRequestID`、最终 effort、observed tier/headers 字段；因此 generic Gateway 的 upstream request id 采集和 service-tier response reconciliation 不能安全伪造字段完成。
2. 当前 package 仍受上述范围外基础契约缺失影响，新增纯 mock 测试已写入但无法运行；入口审查负责人需要先分类/闭合这些契约后再执行 service/repository 单测。
3. 未执行 DB migration、SQL 集成测试、Redis、容器、应用启动、真实支付/外部调用；WP05 schema/index 与数据库对账仍属于后续集中验收。
4. Personal FEFO、永久余额、图片/long-context、不重复计费的既有路径未被重构；本轮只在允许 billing service/usage/payment 生命周期边界补入契约，完整账务对账仍需测试专用数据库阶段。

## 安全边界确认

- 未修改 `go.mod`、schema、migration、DTO、handler、Gateway forward 共享结构、Wire。
- 未覆盖 Personal 分叉整文件。
- 未删除测试文件。
- 未提交、未推送、未创建分支。
