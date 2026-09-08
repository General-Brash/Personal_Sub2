# Protocol Fusion Report — 2026-09-07

## 范围与边界

- 工作树：`D:/Codex_Program/Personal_Sub2/178-p1`
- 依据：`D:/Codex_Program/Personal_Sub2/178-p1/docs/migration/fusion-178-p1-0.2.1-code-first-fusion-and-db-validation-plan.md` 的 WP-03/WP-04/WP-05/WP-08 转发侧，以及
  `D:/Codex_Program/Personal_Sub2/fusion-0.2.1-code-evidence/run-2026-09-07T05-24-25-962Z/protocol-scope.json`
- 官方参考：`D:/Codex_Program/Personal_Sub2/sub2api-0.2.1/sub2api-0.2.1`
- 本轮未启动子 agent、未提交、未启动业务栈、未连接 DB/Redis/容器、未调用真实外部 API/支付。
- 未修改 `backend/internal/service/account.go`、`backend/internal/service/group_codex_models_manifest.go`、DTO/domain/schema/Ent/Wire/go.mod 及 usage 计费实现域。
- `upstream_request_id.go` 及 usage 字段由 billing 负责人提供；本轮仅在 ForwardResult/转发结果上保留直接响应 headers 与 tier/metadata 接口，不重复实现其采集契约。

## 已完成的逐文件决策与实现

### 1. ForwardResult 与协议结果合同

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/gateway_service.go`
  - 在 `ForwardResult` 增加 `UpstreamHeaders`、`UpstreamResponseServiceTier`、`ServiceTier`，保留 Personal 的模型映射、reasoning、图片生成与音频字段。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_gateway_service.go`
  - 在 `OpenAIForwardResult` 增加 `UpstreamHeaders`、`UpstreamResponseServiceTier`；保留 `Usage.ImageInputTokens`、最终 reasoning/service tier 与 WS/响应终端字段。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/gateway_forward.go`
  - 非 OpenAI 转发结果保存 `resp.Header.Clone()`，并回填响应观察到的 service tier；不替换现有 Personal forwarding/billing 上下文。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_gateway_forward.go`
  - HTTP OpenAI/Responses 路径保存 headers 与 observed tier；在获取 token 后设置 attempt-scoped `SetOpsUpstreamModel`，保留已有 compact、legacy Responses ingress、reasoning policy、图片输入 token 和 fallback 逻辑。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_ws_forwarder_v2.go`
  - WS v2 入口设置实际映射模型；结果保留 upstream response service tier。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_ws_v2_passthrough_adapter.go`
  - 首轮及每个接受的 `response.create` 更新 Ops 上游模型；结果保留 observed response tier，未删除现有 WS replay、policy、Personal turn 计费上下文。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_ws_http_bridge.go`
  - bridge 结果回填 upstream response tier，并在模型映射完成后设置 Ops 上游模型。

### 2. Ops upstream attribution / upstream_errors 闭环

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/ops_upstream_context.go`
  - 闭合 `OpsUpstreamModelKey`、`SetOpsUpstreamModel`、`ClearOpsUpstreamModel`。
  - `OpsUpstreamErrorEvent` 增加持久化的 `ProxyID`/`ProxyName` 与 `DroppedEarlierAttempts` 关联字段。
  - 增加 `opsUpstreamProxyAttribution`、`opsUpstreamProxyID`、`opsUpstreamProxyName`、WS proxy attribution、legacy event normalization。
  - 归因不变量：`proxy_id == null` 时只允许 `direct/no_proxy` 或 `unknown`；不记录 proxy URL/用户名/密码；历史事件读取和新事件追加都会归一化。
  - 保留 `OpsStreamError.UpstreamErrors` 与 `OpsUpstreamErrorsKey` 的 turn 级关联。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/ops_service.go`
  - 将 upstream error queue sanitize 补为官方 0.2.1 的窗口/字节预算/最大事件数策略；旧事件只保留标量信息，新事件保留受限 body/detail；最老保留事件记录 `DroppedEarlierAttempts`。
  - 归一化代理归因后再序列化，避免把伪造 proxy name 与 null ID 一起持久化。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/ops_upstream_context_test.go`
  - 增加 proxy attribution、历史归一化、Ops model set/clear、WS payload string view 的纯测试。

### 3. WS replay / Responses / 图片与转发缺口

从官方 0.2.1 逐文件补入 scope 内当前缺失的实现/纯测试，未覆盖既存文件：

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/gateway_upstream_transport_error.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_compact_fallback.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_encrypted_content_lineage.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_images_b64_backfill.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_opencode_session.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_raw_stream_truncation.go`

图片回填实现沿用已融合的 `urlvalidator.IsBlockedHost`、public-host-only 出站标记、重定向逐跳约束、大小上限及 PNG/JPEG/WebP/GIF 字节嗅探；未放宽 allowlist，也未把响应头 MIME 当作唯一依据。

新增/补入的纯测试资产：

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/gateway_upstream_transport_error_test.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_compact_fallback_test.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_encrypted_content_lineage_test.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_images_b64_backfill_test.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_opencode_session_test.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_ws_replay_allocation_test.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/session_limit_release_test.go`

### 4. 零拷贝 WS payload view

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_ws_forwarder_payload.go`
  - 增加 `openAIWSPayloadStringView`，仅供不可变 replay payload 的 gjson 读取；空 payload 返回空字符串，避免对大 input 做整段复制。

### 5. 传输错误归因

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_upstream_transport_error.go`
  - 增加通用 gateway 传输错误分类适配 `classifyUpstreamTransportError`，复用已有 typed error / marker 分类，不改变 OpenAI 专属 eviction 语义。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/ops_upstream_context.go`
  - transport error 事件可带同一 account snapshot 的 proxy ID/name，避免转发路径与 Ops attribution 不一致。

## 实际验证

已完成：

1. 对本轮新增/修改 Go 文件运行 `gofmt`。
2. 对本轮相关 diff 运行 `git diff --check`，无本轮新增空白错误。
3. 静态核对 `SetOpsUpstreamModel`、`opsUpstreamProxyID/Name`、`openAIWSPayloadStringView`、`UpstreamHeaders`、`UpstreamResponseServiceTier` 的定义与调用点。
4. 审查了新增测试入口；新增测试均为内存/httptest/纯函数/协议替身，不主动初始化 DB、Redis、容器或业务完整服务。
5. 曾执行一次定向 `go test ./internal/service -run ...` 作为编译门禁探测；命令未通过，但失败首先来自当前工作树既存的跨负责人缺口，而非本轮报告所述实现：
   - `GroupCodexModelsManifestConfig` / `Group.CodexModelsManifestConfig` 尚未由 contracts/group 侧闭合；
   - `OpenAIFastTierUltrafast`、`GrokSupportsXHighReasoningEffort` 等基础 metadata/billing 合同仍在其他负责人工作中。

## 未完成项 / 集成依赖

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/upstream_request_id.go`：由 billing 负责人提供；待其提供后，需要把配置头名解析结果注入所有 HTTP/WS `ForwardResult`/usage 记录路径。当前已保留 `UpstreamHeaders` 作为直接响应头接口，未猜测 header 名，也未用下游 request ID 冒充上游 ID。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_codex_model_metadata.go` 依赖 contracts/group 侧未完成的 metadata 类型；本轮未越界修改 `group_codex_models_manifest.go` 或 account/domain。
- 当前整包编译仍受上述跨线缺口阻塞；按要求未通过修改禁区文件绕过。
- 未进行 DB/Redis/容器/真实外部 API/支付集成验证；这些属于用户明确禁止或计划中的集中验收范围。

## 风险与限制

- 本轮从官方补入的文件保持官方行为作为协议基线，但与当前工作树尚未闭合的 contracts/billing 分支需要一次集成编译；不能把当前状态表述为全包测试通过。
- WS `UpstreamRequestID` 仍遵循“允许为空”的协议契约，等待 billing 提供的显式配置头名采集实现；没有新增自动猜测链。
- 未修改 `account.go`，因此其中历史 GeminiGoogleOne/AdaptiveCN helper 缺口继续由主负责人处理。
