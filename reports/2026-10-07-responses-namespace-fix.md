# Responses 历史工具调用 namespace 修复

日期：2026-10-07
状态：代码修复与本地回归完成；未部署、未进行线上 Ableton MCP 验收。

## 问题与根因

依据用户提供的对照实验摘要，并独立核对当前仓库：
`shouldKeepOpenAIResponsesToolCallNamespaces` 仅允许 OpenAI OAuth 保留历史工具调用的
`namespace`，导致 API-key 请求在公共 `Forward` 入口被提前删除该字段。
API-key 可连接支持 namespace 的自定义上游，认证类型不能作为字段不受支持的依据。
该清理发生在普通 Responses、HTTP passthrough 和 Chat Completions 桥接分流之前。

用户引用的完整诊断报告位于另一台机器的 macOS 路径，本次未读取该文件；账号编号、
线上二进制哈希及真实上游实验属于用户提供的证据，本次没有重新连接线上核验。

## 修改范围

- `backend/internal/service/openai_responses_namespace.go`：普通、非摊平请求对 OpenAI
  API-key 与 OAuth 一样保留工具调用 namespace，不依赖当前请求是否包含 tools 声明。
- 修正三处将 API-key 删除 namespace 当成成功条件的旧测试。
- 增加三轮 HTTP 回放测试：普通/透传 × JSON/SSE，使用实际返回的 output 构造下一轮
  input；两个 namespace 下使用同名工具，第三轮带八个历史调用，检查完整调用身份与参数。
- 补充无关字段重试、明确 namespace 拒绝后的定点重试、Chat Completions 映射，以及
  WS→HTTP bridge 的 namespace 历史重建断言。

保留既有边界：compact 清理、显式 OAuth namespace 摊平、非调用项直属 namespace
清理、嵌套字段保留和原生 WS 行为均未修改。没有改动账号配置、容器、数据库或 Ableton 工程。

## 实际验证

使用仓库要求的 Go 1.27.0 工具链。

1. 修复前，新增 API-key 保留策略的四个用例失败（期望 true，实际 false），复现缺陷。
2. 修复后，以下定向回归命令通过（在 backend 目录）：

```powershell
go test -tags=unit ./internal/service ./internal/pkg/apicompat -run 'Namespace|Namespaced|RejectedFieldRetry|BridgeKeepsContinuationFrames|OAuthStoreFalseByDefault|StoreDisabledFunctionCallOutputSkipsAutoAttachWhenToolCallContextPresent|BridgeTurnAPIKey' -count=1 -timeout=180s
```

3. 修改的 Go 文件已 gofmt；`git diff --check` 通过。

测试使用内存替身与本机回环连接，不访问真实上游。没有执行全仓测试、数据库/Redis
集成测试、线上请求或发布构建；不能据此宣称线上故障已消除。

## 发布与验收建议

- 部署前保留当前镜像/二进制摘要及可回滚版本，按现有发布流程构建修复版本。
- 先灰度命中报告中的同一实际上游，完成至少三轮 Ableton MCP 调用与结果回传。
  验收必须检查每轮 namespace、name、call_id 和参数，不能只看 HTTP 200 或流正常结束。
- 对不支持 namespace 的兼容上游另做验证：已有明确拒绝重试仅适用于普通 HTTP，
  且最多六次；passthrough 和 WS→HTTP bridge 不新增自动重试。本修复不扩大兼容重试策略。
- 若灰度出现新的上游 schema 拒绝或调用身份不匹配，恢复发布前的镜像/二进制；
  本次无数据库或配置迁移，不需要反向迁移。
