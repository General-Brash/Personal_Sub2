# contracts 跨层融合报告

- 工作区：`D:/Codex_Program/Personal_Sub2/178-p1`
- 日期：2026-09-07
- 参考：官方 `D:/Codex_Program/Personal_Sub2/sub2api-0.2.1/sub2api-0.2.1`，旧基线 `D:/Codex_Program/Personal_Sub2/sub2api-0.2.0`
- 证据：`D:/Codex_Program/Personal_Sub2/fusion-0.2.1-code-evidence/run-2026-09-07T05-24-25-962Z`
- 数据库状态：未连接、未执行迁移、未启动 Redis/容器/业务服务、未发起真实外部请求。

## 本轮裁决

1. `UpstreamModelMetadata`、`UpstreamModelMetadataSnapshot`、`UpstreamModelCatalog`、Account Get/Set 统一写入现有 `backend/internal/service/upstream_models.go`，没有新建重复 metadata 文件。
2. `schema/group.go` 已冻结；已保留 `codex_models_manifest_config` JSON 字段，后续不再修改。Ent 生成由主负责人执行，未手改 generated Ent。
3. `service/account.go`、`domain/reasoning_effort.go` 及主负责人独占文件未由本轮写入。
4. 未新增任何 `SettingKey`，未修改 `backend/internal/service/domain_constants.go`。
5. `GrokSupportsXHighReasoningEffort` 按要求不落在 protocol 文件；当前 Codex 调用仍等待协议负责人/主负责人提供该接口。
6. `OpenAIFastTierUltrafast` 按要求不落本域，等待 billing 负责人提供常量。

## 已写入/合并文件

### 基础契约与 Codex service

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/upstream_models.go`
  - 合入官方 0.2.1 的 metadata/snapshot/catalog 类型。
  - 合入 Account metadata Get/Set 方法。
  - 合入上游 catalog producer、models.dev metadata enrichment、能力字段归一化、请求端点兼容和 catalog extraction。
  - 保留当前 Personal 工作树中已有的上游请求/模型提取差异。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_codex_models_service.go`
  - 合入官方 Codex descriptor/composite/metadata 生成闭环及 API key manifest metadata 补全。
  - 保留当前 Personal 的 Codex 过滤、图片模型排除、responses-lite 约束等行为。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_codex_model_metadata.go`
  - 与 descriptor/composite metadata 类型完成调用闭环。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/openai_codex_models_pinned.go`
  - 新增固定账号 Codex manifest 路径实现。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/domain/codex_models_manifest_config.go`
  - 新增固定账号清单配置类型。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/group_codex_models_manifest.go`
  - 新增分组配置归一化与固定账号校验逻辑。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/group.go`
  - 增加 Group 对 manifest 配置的 domain alias 与字段。

### Group / DTO / repository / cache / API

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/schema/group.go`
  - 已包含 `codex_models_manifest_config` JSON schema 字段；本轮确认后冻结，不再继续修改。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/repository/group_repo.go`
  - create/update 写入 manifest 配置调用。
  - 读取路径等待 Ent 0.14.5 生成字段完成后复核。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/api_key_auth_cache.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/api_key_auth_cache_impl.go`
  - 认证快照透传 manifest 配置。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/handler/dto/types.go`
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/handler/dto/mappers.go`
  - Group manifest DTO 映射。
  - Account lite/list DTO 与敏感字段裁剪路径已存在并完成映射合并。
  - Usage DTO 已包含 `upstream_request_id`。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/handler/admin/group_handler.go`
  - create/update manifest 请求字段接线。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/handler/admin/account_handler.go`
  - lite account list envelope 与 upstream request-id extra 校验接线。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/handler/openai_codex_models_handler.go`
  - Codex models handler 官方契约增量合并。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/handler/composite_platform.go`
  - composite 模型清单/descriptor 调用链官方增量合并。

### 纯辅助闭环

- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/pkg/claude/effort_catalog.go`
  - 新增 `claude.EffortLevelsForModel`，只做纯模型 effort catalog，不连接外部服务。
- `D:/Codex_Program/Personal_Sub2/178-p1/backend/internal/service/models_list_response_limit.go`
  - 新增 `resolveModelsListReadLimit`，读取已有配置默认值，不执行 I/O。

## 实测验证

### 已执行

- `gofmt`：已对本轮新增/合并 Go 文件执行。
- `git diff --check`：未发现本轮明确文件的空白错误；输出中的换行风格 warning 来自工作区大量既有差异，不是业务失败。
- 静态符号核对：metadata、snapshot、catalog、manifest config、DTO `upstream_request_id`、Group mapper、repo setter 均可检索到。
- `go test ./internal/service -run '^TestDoesNotExist$' -count=0`：**未通过/未完成编译门禁**。该命令仅用于编译，不执行测试体；失败原因见下节。
- `go test ./internal/handler/dto ./internal/handler/admin ./internal/handler -run '^TestDoesNotExist$' -count=0`：同样被 service 包编译阻断。

### 未执行

- 未执行任何 DB/Redis/integration/e2e 测试。
- 未启动 server、Docker/Compose 或真实 provider。
- 未执行 Ent/Wire 生成；主负责人正在执行 Ent 0.14.5 生成。

## 未完成项 / 阻塞项

1. **主负责人生成 Ent 后复核**：
   - 目标：`D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/group.go`
   - 目标：`D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/group_create.go`
   - 目标：`D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/group_update.go`
   - 目标：`D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/group_query.go`
   - 目标：`D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/group/where.go`
   - 目标：`D:/Codex_Program/Personal_Sub2/178-p1/backend/ent/migrate/schema.go`
   - 本轮禁止手改上述 generated 文件。
2. `OpenAIFastTierUltrafast`：Codex service 当前引用该官方契约常量，但按用户裁决等待 billing 负责人提供；本轮未在 settings/billing 域补常量。
3. `GrokSupportsXHighReasoningEffort`：Codex service 当前需要该 protocol helper；按用户裁决本轮未修改 `openai_gateway_grok.go`，等待协议负责人协调后提供。
4. 当前工作树还存在主负责人/协议域已有未完成文件，导致 service 包在更早阶段继续报缺失符号，例如：
   - `openai_encrypted_content_lineage.go` 依赖 `OpenAIWSStateStore` 的 encrypted-content 方法；
   - `openai_gateway_request_body.go` 依赖 response schema/compaction helper；
   - `openai_gateway_usage.go` 依赖 service-tier billing helper；
   - 这些不属于本轮 contracts 写入域，未擅自补写。
5. Ent 生成完成且上述跨域接口提供后，需要重新运行 service/handler CODE_ONLY 编译，再补充针对 manifest、metadata、lite DTO、usage upstream ID 的最小纯逻辑测试证据。

## SettingKey 清单裁决

本轮新增代码没有引用新的 `SettingKey`，所以无需向主负责人申请新增 settings 常量；已保留现有 Personal 页面开关、Mall 常量和既有 Codex setting keys。
