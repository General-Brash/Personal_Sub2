# P4.2 全局默认计费标准修复报告

日期：2026-10-08
计划基线：`docs/feature-P4.2/实施文档.md`；工作区 HEAD 保持 `6d7bf0555`，分支保持 `codex/feature-4.2`。

## 结果

已完成计划 S1–S6 的业务代码、管理交互和针对性验证。所有改动保留在工作区，**未暂存、未提交、未推送**。没有部署应用，没有修改现有运行数据库或线上价格。

### 默认价持久化与生效

- 新增独立的 `DefaultModelPricingService`，复用 settings 的 `model_default_pricing_overrides` 键，不把价格塞进广场展示配置。
- 单模型白名单 patch，明确区分省略、数字 `0` 和 `null`。新 Token 标准要求明确输入/输出单价；图片和视频要求本模式的完整档位。
- model ID 归一化但保留 `/`、`.`、`:`、版本后缀；精确人工记录先于计费别名；系统精确标准、别名/家族匹配、人工覆盖分开返回。
- 数据库 CAS 成功后才发布不可变快照；补齐首次并发初始化的 conflict-safe insert，旧版本返回独立 409。
- 启动读取/校验失败阻止启动。每 3 秒轮询数据库，健康状态按约 5 秒收敛设计；无效配置或读取失败保留旧快照，管理端区分持久化/本实例加载版本。
- 恢复会撤成无价时拒绝操作；数据库回退 revision 后要求重启，不允许运行中的旧高版本快照被静默回写。

### 实际计费与报价

- 管理员显式字段叠加在系统有效默认价之后，保留代码 fallback、未修改的缓存和高级规则。
- Token 解析继续保持分组、渠道、默认价的优先级；分组命中不再叠加渠道价。预检传入请求已有的完整分组上下文，避免与结算用不同价卡。
- 补齐缓存写入、图片输入/输出 Token 的显式零消费；报价不再把合法零价显示成缺价。
- 对没有完整系统 Token 基准的新标准，未设置的缓存写入/读取回退普通输入价，1h 写入回退 5m，避免仅因缓存字段为空而免费；保留显式零/折扣，不回写为人工字段，已有完整系统基准的缓存语义不变。页面在保存前明确显示该规则及有效值预览。
- 按次、按张和按秒分别接入消费链。人工图片/视频默认值不会被 Grok 代码默认价抢先。
- 补齐网关媒体分流：显式人工 Token 模式可用于图片 Token 结算；全局按次模式不把图片张数或视频秒数误当请求次数。独立音频/搜索收费未改写。
- 旧长上下文阈值的两段计算共用一次取得的有效 Token 价，按次标准不会被拆成多次计费。
- 补齐图片输入/输出 Token 的公共报价字段、显式零展示和报价版本；旧后台自动填充接口返回实际生效的图片 Token 回退价，不把缺省值误填成免费。
- 明确的 Grok 视频别名继承 canonical 人工标准，但精确人工记录仍优先；未来版本不按前缀误继承其他型号的人工价。别名恢复后的响应也使用已提交快照中的实际继承价。
- 图片报价使用 `per_image`，新全局视频标准使用 `per_second`；跨计量模式的专用分组媒体价按条件基准价展示，不混入 Token 样本比价。
- 后台默认价查询、模型广场 V2、旧版广场及可用渠道的相关展示路径接入同一默认层；供应商/系统参考价不冒充人工价格。

### 管理界面、权限与审计

- 新增专用默认价弹窗：已有模型编辑、手填新 model ID、系统/人工/有效值对照、单位换算、恢复继承、显式免费提示。
- 人工标准列表支持搜索和分页，包括尚未进入能力目录的模型；保存价格不创建路由或调用能力。
- 价格与隐藏/置顶/排序草稿分开保存。保存后独立刷新人工列表和报价，失败提示区分“未保存”与“已保存但刷新失败”。
- 409 保留草稿、读取最新版本并显示字段差异；显式确认后才允许重试。已按真实 API 客户端的 `{status, code}` 错误格式验证。
- 新增 `models.pricing.read/write`，登记 Go 权限清单、精确路由映射和数据库权限目录。迁移 247 只注册定义，不自动给已有展示管理员授予改价权限。
- 复用管理员认证、合规和审计链路；审计记录操作者、单模型价格前后值、旧/新版本和结果，不记录整张 settings 表。
- 中英文文案、Wire 依赖、刷新任务启动/停止、配置示例和备份恢复说明已补齐。

## 已执行验证

### 后端

实际模块工具链为 Go **1.27.0**。在 `D:/Codex_Program/Personal_Sub2/main/backend` 执行：

```powershell
go generate ./cmd/server

go test -tags=unit ./internal/service ./internal/handler ./internal/handler/admin ./internal/server/middleware ./internal/server/routes ./cmd/server -run 'DefaultModelPricing|DefaultPricingQuery|PricingOverride|PricingHotReload|Preflight|PriceQuote|TestResolve_|ModelPlaza|Plaza|ContextPricingSchedule|TestCalculate(Cost|TokenCost|ImageCost|VideoCost)|RecordUsage|AdminModelPricing|AdminPermissionInventory|ProvideCleanup|ModelCatalog' -count=1
```

**六个受影响包均通过**，包含新增服务、handler、认证/权限和审计测试，以及既有计费、预检、报价、用量记录、目录及生命周期回归。Wire 由项目生成命令产生，不是手工修改生成文件。生成命令补入了 Wire 工具依赖 `github.com/google/subcommands v1.2.0` 的两条 go.sum 校验值，go.mod 未改。

用真实 `RecordUsage` 入口和实际生成的 `UsageBillingCommand` 核对：输入 `$2/百万`、输出 `$8/百万`，1000 input + 500 output，倍率 1.5，得到 `TotalCost = 0.006`、`ActualCost = 0.009`、`BalanceCost = 0.009`。图片、视频、媒体 Token 和按次模式也验证了 usage/扣费命令，不仅验证 resolver 返回值。

### 隔离数据库

在临时 PostgreSQL **18.1** / Redis **8.4** 容器中运行，未连接现有开发或线上库：

```powershell
$env:CI='true'
$env:SUB2API_VALIDATION_MODE='ci-container'
$env:SUB2API_ALLOW_LEGACY_CI_CONTAINERS='ALLOW'
$env:TESTCONTAINERS_RYUK_DISABLED='true'
go test -tags=integration ./internal/repository -run 'DefaultModelPricingCompareAndSet|PersonalFeaturesSettingPolicyCAS' -count=1
```

**通过**：首次并发初始化只有一个成功写入，另一方正常返回冲突；旧值拒绝覆盖；事务回滚不留下新键、不改变原配置束。测试初始化也应用了新增权限迁移。

首次运行遇到本机 Testcontainers/Ryuk 启动故障，随后禁用该清理助手重跑通过。已核对会话标签并显式销毁本轮三个测试容器（含第一次失败留下的容器），最终容器清单为空。环境变量仅作用于该测试子进程。

### 前端

使用项目声明的 pnpm **9.15.9**（`corepack pnpm`），没有安装或升级业务依赖。在 `D:/Codex_Program/Personal_Sub2/main/frontend` 执行：

```powershell
corepack pnpm exec vitest run src/components/modelPlaza/__tests__/ModelDefaultPricingDialog.spec.ts src/components/modelPlaza/__tests__/ModelPlazaAdminPanel.spec.ts src/components/modelPlaza/__tests__/ModelPlazaV2Content.spec.ts src/components/modelPlaza/__tests__/PlazaModelPricingTable.spec.ts
corepack pnpm run typecheck
```

**4 个测试文件、42 项测试通过**；typecheck 通过；对本次改动的前端文件执行 ESLint，通过。最初 PATH 的 pnpm 11 与既有 node_modules 不兼容，已改用 Corepack 固定版本，不清空依赖目录。Browserslist 的旧数据提示未影响检查，未借此升级无关依赖。

### 工作区保护

- `git diff --check` 通过，暂存区为空，HEAD 未变化。
- 对开始时已有的五个 Responses 已修改文件、未跟踪的 roundtrip 测试及前一轮报告逐个核对 SHA-256，**全部保持原样**。
- 没有提交、推送、创建 PR、部署或改动线上价格；没有修改已有历史 SQL。

## 使用与边界

- 部署这批代码时需按项目正常启动迁移流程应用 **247_model_default_pricing_permissions.sql**；本轮只在隔离测试库验证，未对现有运行库执行迁移。
- 运行说明和恢复流程见 [全局默认计费标准说明](D:/Codex_Program/Personal_Sub2/main/deploy/model-default-pricing.md)。
- 没有进行浏览器人工联调、真实供应商请求或对真实用户余额扣费。用量测试核对的是实际生成的 usage 和扣费命令，**不是声称已经在真实钱包上扣款**。
- 保留既有分组/渠道、用户、时间及动态倍率规则，不重算历史账单。
- 未实现预检到结算的完整在途锁价；既有 OpenAI 缺价结算兼容分支仍在。不要将恢复保护或单次解析快照误当作全链路锁价/全链路 fail-closed。
- 审计沿用现有异步链路，不承诺与 settings CAS 同事务。数据库回滚恢复时应停流并重启所有实例，再核对版本和预检结果。
