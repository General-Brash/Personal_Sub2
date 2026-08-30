# Personal_Sub2

Personal_Sub2 是基于官方 Sub2API `v0.1.178` 开发并独立维护的个人版本，并融合截至 `v0.1.183` 的官方更新；本次融合版本为 `v0.1.183-P1`。

[English](README.md) | 中文 | [日本語](README_JA.md)

## 个人版内容

- **临时额度**：在永久余额之外，记录具有有效期的临时额度发放、消费和可用额度。
- **每日签到**：按可配置规则向用户发放临时额度奖励，并提供签到状态和记录。
- **银行**：支持预支临时额度、将永久额度兑换为临时额度，并提供可配置的限额、结算规则和流水记录。
- **安全审计二次审核**：改进 ASCII 关键词边界匹配；命中后可交给独立的 `intent-classifier` 服务二次判定，支持 `off`、`shadow`、`enforce` 模式，以及模型包校验、激活和回滚。

仓库不包含正式模型权重。启用模型二次判定前，请按 [`MODEL_PACKAGE.md`](services/intent-classifier/MODEL_PACKAGE.md) 准备并激活模型包。

## 官方 v0.1.178–v0.1.183 融合内容

- **上游 URL 安全**：转发前校验客户端可控的 OpenAI Responses 子路径、Gemini 模型/操作路径和 Grok 视频请求 ID，拒绝可能改变上游 URL 结构的路径片段。
- **运行时定价与计费**：Docker 和 GoReleaser 镜像构建现会携带运行时回退定价资源，并修正 GPT-5.6 Luna/Terra 费率与 GLM-5.2 回退定价。
- **容器权限加固**：所有官方 Compose 配置均启用 `no-new-privileges`，防止应用进程在运行时获得额外权限。
- **代理断流熔断容错**：当所有候选账号共用被隔离代理时，OpenAI 代理断流隔离会自动放行（fail-open）；短时间内的集中断流合并为一次事件，并提供显式禁用开关。
- **路由与调度正确性**：在保持普通分组隔离的同时，Composite 分组会展示已配置的具体模型平台；令牌刷新会跳过不可调度账号。
- **协议与界面修复**：生成符合标准的 SMTP 邮件，改进 Anthropic 分类器/令牌计数兼容性和 Qwen3Guard 辅助字段处理，并修正订阅到期标签及长套餐名称显示。

### 官方 v0.1.179–v0.1.183 新增内容

- **协议与媒体扩展（v0.1.179）**：支持 OpenAI Responses 文件/图片输入和 `file_search` 兼容，增加 Kimi、智谱、DeepSeek 自适应协议路由及多协议 Base URL，改进 Claude Code 分析块、Web Search 和 Grok 媒体处理，并扩大 Composite 平台支持。
- **定价、路由与插件（v0.1.180）**：增加渠道价格倍率与时间定价、Composite 国产平台路由、1M 上下文和 service tier 计费支持、插件管理，以及相关网关调度和协议修复。
- **Responses 与可靠性（v0.1.181–v0.1.183）**：改进 Responses Lite/工具兼容、Token 与缓存计费，区分 OAuth 配额耗尽和瞬时失败，增加 Kimi 并发冷却与会话粘性，支持自定义工具/Tool Search，修复 Composite 监控聚合并刷新支付成功后的余额。

## 安装与升级

脚本安装适用于已运行 PostgreSQL 和 Redis 的 Linux amd64/arm64 服务器，并需要 root 权限：

```bash
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash
```

安装后可访问 `http://服务器地址:8080` 完成首次设置。常用命令：

```bash
# 查看状态和日志
sudo systemctl status sub2api
sudo journalctl -u sub2api -f

# 升级到个人仓库的最新 Release
curl -sSL https://raw.githubusercontent.com/General-Brash/Personal_Sub2/main/deploy/install.sh | sudo bash -s -- upgrade
```

也可以在管理后台使用版本检测和升级功能。执行升级前请备份数据库、配置文件和数据目录。

个人版容器镜像发布到：

```text
ghcr.io/general-brash/personal_sub2
```

部署文件及运行参数见 [`deploy/`](deploy/)；使用容器部署时，请确认应用镜像明确指向上述个人版镜像，避免混用其他版本。

## 从源码构建

环境要求：Go 1.27.0、Node.js 20+、pnpm 9、PostgreSQL 和 Redis。

```bash
git clone https://github.com/General-Brash/Personal_Sub2.git
cd Personal_Sub2

cd frontend
pnpm install --frozen-lockfile
pnpm run build

cd ../backend
go build -tags embed -ldflags="-X main.Version=$(./scripts/resolve-version.sh)" -o sub2api ./cmd/server
./sub2api
```

首次启动后访问 `http://localhost:8080`，按设置向导配置数据库、Redis 和管理员账号。

## 开发与验证

```bash
# 后端测试
cd backend
make test-unit

# 前端检查
cd ../frontend
pnpm run lint:check
pnpm run typecheck
pnpm run test:run
```

更多仓库内开发约定见 [`DEV_GUIDE.md`](DEV_GUIDE.md)。

## 安全与使用责任

- 使用前请确认符合所在国家或地区的法律法规，以及所接入服务的条款。
- 生产环境应使用独立强密码和固定密钥，并限制管理端和数据库的网络暴露范围。
- 不要提交或公开 API Key、访问令牌、数据库密码、`.env` 和 `config.yaml` 中的敏感信息。
- 升级、迁移或调整安全策略前，请先备份并在非生产环境验证。
- 本项目按现状提供；使用者自行承担账号、服务、数据和合规风险。

## ❤️ 赞助商

<table>

<tr>
<td width="180"><a href="https://go.apimart.ai/gh-sub2api"><img src="assets/partners/logos/apimart.jpg" alt="APIMart" width="150"></a></td>
<td>感谢 APIMart 赞助了本项目！<a href="https://go.apimart.ai/gh-sub2api">APIMart</a> 是专注于 AI 图片/视频生成的低价 API 平台，GPT-Image-2 低至 $0.006/张，1 美元可生成 160+ 张图片。图片、视频一套异步 API 通吃：提交任务获取 ID，通过轮询或回调获取结果；批量生成上万张图片也不会超时，切换模型无需修改代码。按量付费、无月费，通过<a href="https://go.apimart.ai/gh-sub2api">此注册链接</a>注册即可开始使用。</td>
</tr>

</table>

## 许可证

本项目按 [GNU Lesser General Public License v3.0](LICENSE)（或更高版本）授权。
