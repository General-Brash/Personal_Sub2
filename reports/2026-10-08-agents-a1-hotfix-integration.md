# P4.2：合入服务器 Agents-A1 Chat 转发热补丁

日期：2026-10-08
目标分支：`codex/feature-4.2`；基线 HEAD：`6d7bf0555`。
状态：已合入当前工作树并通过定向回归；未暂存、未提交、未推送、未部署。

## 来源及真实性核对

按用户提供的 SSH 连接只读获取服务器上的既有补丁，未读取私钥内容、敏感配置备份、数据库或用户请求数据。

- 服务器来源目录：`/home/taffy/session/sub2-hotfixes/agents-a1-20261007/`
- 已读取：README、build-metadata、agents-a1.patch，以及 source 中对应生产文件和 agents_a1_test.go。
- 原补丁基线提交：`99993a62b7147b6ff826d0ccb33c7c8a7f6c553f`。
- 原补丁版本：`0.2.1-P4.1.2-agents-a1-hotfix.1`。
- 服务器 README 记载 2026-10-07 15:32 已部署；此为历史记录，本轮没有重新验证生产部署或调用真实上游。

远端 `sha256sum` 与下载文件的本地 SHA-256 一致：

| 文件 | SHA-256 |
| --- | --- |
| agents-a1.patch | `f0bdf44da801f7a6b2135a883e8f1205238c2f6a17611814c8e916d3dd77fcb4` |
| openai_gateway_responses_chat_fallback.go | `8693867da08e32bc352c9515b9eb7a1dee625a16724dc3096ab6128db723f45a` |
| agents_a1_test.go | `5c03a155a31819f5ed3a194fba2a9ed313ad5083fd12d7a43906cec9d14ad931` |

通过 `git apply --check` 后，仅选取生产消息补丁和 Agents-A1 测试应用；应用后的这两个文件与服务器源码哈希完全一致。没有应用原包中的 intern-s2 定价测试／SQL，也没有重复修改价格或账户映射。

## 行为与改动

- `backend/internal/service/openai_gateway_responses_chat_fallback.go`：在账户映射及上游模型归一化之后调用 `normalizeAgentsA1SystemMessages`。
- 仅最终模型精确等于 `Agents-A1` 时，将文本 system 消息按原指令顺序汇总到唯一首条 system；包括中途 developer 转换出的 system。
- 全部非 system 消息保持原样，包括工具调用、结果、参数及 reasoning；工具声明、reasoning effort、parallel_tool_calls 不改变。
- 其他模型、映射离开 Agents-A1 的请求不合并。原生 Responses 路径不改变。
- 非文本 system 或带额外消息元数据的情况沿用原补丁保护：不强行合并，避免丢弃内容。因此不承诺该类输入一定符合 Agents-A1 上游限制。
- 晚到系统指令被移到开头，是该上游单首条 system 协议要求下的兼容行为，不是全局消息转换规则。

新增测试：

1. `backend/internal/service/agents_a1_test.go`：原补丁测试，覆盖纯文本／数组文本、顺序和空白、非文本保护、非目标模型不变、Auto 映射及完整出站请求比对。
2. `backend/internal/service/agents_a1_roundtrip_test.go`：额外组合回归，JSON／SSE 两种模式分别执行两轮 Forward；第一轮返回两个 namespace 的同名工具及一个 custom tool，第二轮实际回填返回项和工具结果，并在调用与结果之间加入 developer 指令。检查系统合并、完整出站请求、namespace/name/call_id、参数大整数、custom input、usage 及流结束标记。

没有修改原有 namespace、定价、权限、前端或生成代码。开始时已有的 61 个已修改／未跟踪文件逐个 SHA-256 比对均保持不变。

## 验证

在 backend 目录使用项目工具链运行，以下最终命令通过：

```powershell
go test -mod=readonly -tags=unit ./internal/service ./internal/pkg/apicompat -run 'NormalizeAgentsA1|TestForwardResponses_|TestForwardResponsesChatCompletionsFallbackKeepsFunctionArgumentsSingle|APIKeyNamespaceRoundTrip|TestResponsesToChatCompletionsRequest_|TestResponsesInputToChatMessages_|TestChatCompletions(ResponseToResponses|ChunkToResponsesEvents)_|TestGolden_|TestRequest_SequentialToolCallsStaySeparate|TestNormalize_DropsOrphanToolReply|TestStream_|DefaultModelPricing|BillingPricingPreflight' -count=1 -timeout=180s
```

- 两个测试包均通过，包含新增用例和相关既有转发／工具／定价回归。
- 原补丁测试在加入组合用例前单独通过。
- 新增 SSE 用例第一次运行因合成 data 行包含多行 JSON 而失败；修正测试样例的单行 SSE framing 后通过，未因此改动生产解析逻辑。
- 三个 Go 文件的 `gofmt -l` 无输出；`git diff --check` 通过。
- 没有执行全仓、前端、数据库集成测试或真实供应商请求。测试使用内存上游替身，不对真实钱包扣费。

## 发布边界

当前工作树已包含原先仅存在服务器独立镜像中的兼容逻辑。后续从包含本次改动的源码构建 P4.2，才会保留该行为；本轮没有构建／替换线上镜像，没有重启应用，没有修改服务器文件、价格或路由配置。部署后的真实 Auto 调用仍需按正常发布流程验收。
