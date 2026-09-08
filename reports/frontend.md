# 前端融合证据报告（A0-A7 / WP09 / 非DB）

- 日期：2026-09-07（Asia/Shanghai）
- 工作区：`D:/Codex_Program/Personal_Sub2/178-p1`
- 官方参考：`D:/Codex_Program/Personal_Sub2/sub2api-0.2.1/sub2api-0.2.1`
- 旧官方参考：`D:/Codex_Program/Personal_Sub2/sub2api-0.2.0`
- 结论：前端代码融合完成；两套隔离 Node 环境的非 DB 门禁通过；数据库、Redis、业务服务、容器、真实上游支付均未启动。

## 范围与保护

- 先阅读迁移计划 WP09、A5、A7；DOC_READY 仅作为历史方案，不作为本次完成证据。
- 仅处理 `frontend/` 任务必需源代码与测试，以及用户指定的 `reports/frontend.md`。
- 未改 `frontend/package.json`、`frontend/pnpm-lock.yaml`；两者在任务开始基线中已有未提交差异（见 `baseline/working-tree.patch`），已保留。
- 未提交、未推送、未创建分支；Personal 其余未提交改动未回滚。
- 未执行 DB/Redis/业务服务/容器/真实支付或真实上游请求。

## 已完成融合

1. Account lite 列表与 full detail 边界：保留 lite 请求参数、敏感字段裁剪及 Personal 额外字段；补齐上游 Header 配置字段、空值清理和 API 类型。
2. upstream request id：账号配置编辑、UsageTable 展示、复制/空状态、多语言和对应 mock 测试。
3. Group Codex manifest：固定账号选择器、fallback/空状态、分组配置类型及页面接线。
4. Pricing/reasoning：max reasoning multiplier、fast/flex/ultrafast、reasoning none、usage tier 及 en/zh 文案。
5. 多平台与 tooltip：保留现有多平台 Codex/平台路径，补齐 HelpTooltip、平台常量及相关测试。
6. Sidebar：保留 Personal 商城、支付、银行、财务、返利、签到、插件等菜单与 feature flag；保留官方折叠、滚动位置、SVG 样式回归。
7. 旧缺口：按函数/字段级合并账号 API、Usage、Groups、Channels、Settings、Model Plaza、PaymentStatus 等，不整目录复制。

## 逐文件最终裁决

| 文件 | 裁决 |
|---|---|
| `frontend/src/api/admin/accounts.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/api/admin/channels.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/api/admin/settings.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/api/channels.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/account/CreateAccountModal.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/account/EditAccountModal.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/account/ModelWhitelistSelector.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/account/UpstreamRequestIdHeaderField.vue` | MERGE_REQUIRED：官方新增文件直接纳入，范围内无 Personal 同名文件。 |
| `frontend/src/components/account/__tests__/CreateAccountModal.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/account/__tests__/EditAccountModal.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/account/__tests__/ModelWhitelistSelector.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/account/__tests__/UpstreamRequestIdHeaderField.spec.ts` | MERGE_REQUIRED：官方新增文件直接纳入，范围内无 Personal 同名文件。 |
| `frontend/src/components/admin/channel/PricingEntryCard.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/admin/channel/__tests__/PricingEntryCard.timePricing.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/admin/channel/types.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/admin/group/CodexManifestAccountsField.vue` | MERGE_REQUIRED：官方新增文件直接纳入，范围内无 Personal 同名文件。 |
| `frontend/src/components/admin/group/ReasoningEffortPolicyFields.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/admin/group/__tests__/CodexManifestAccountsField.spec.ts` | MERGE_REQUIRED：官方新增文件直接纳入，范围内无 Personal 同名文件。 |
| `frontend/src/components/admin/group/__tests__/ReasoningEffortPolicyFields.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/admin/usage/UsageTable.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/admin/usage/__tests__/UsageTable.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/common/HelpTooltip.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/common/__tests__/HelpTooltip.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/keys/UseKeyModal.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/keys/__tests__/UseKeyModal.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/layout/AppSidebar.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/layout/__tests__/AppSidebar.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/modelPlaza/PlazaModelPricingTable.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/modelPlaza/__tests__/PlazaModelPricingTable.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/components/payment/PaymentStatusPanel.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/components/payment/__tests__/PaymentStatusPanel.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/composables/__tests__/useModelWhitelist.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/composables/useModelWhitelist.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/__tests__/openaiFastPolicyLocales.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/i18n/__tests__/usageServiceTierLocales.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/i18n/locales/en/admin/accounts.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/en/admin/channels.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/en/admin/overview.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/en/admin/resources.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/en/admin/settings.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/en/dashboard.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/zh/admin/accounts.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/zh/admin/channels.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/zh/admin/overview.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/zh/admin/resources.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/zh/admin/settings.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/i18n/locales/zh/dashboard.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/types/index.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/utils/__tests__/usageServiceTier.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/utils/usageServiceTier.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/views/admin/AccountsView.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/views/admin/ChannelsView.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/views/admin/GroupsView.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/views/admin/SettingsView.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/views/admin/UsageView.vue` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |
| `frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts` | MERGE_REQUIRED：官方新增文件直接纳入，范围内无 Personal 同名文件。 |
| `frontend/src/views/admin/__tests__/UsageView.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/views/admin/__tests__/groupsModelsListLayout.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/views/admin/__tests__/groupsReasoningEffort.spec.ts` | TEST_ONLY_REQUIRED：保留 Personal 测试夹具，补充官方行为断言；全 mock。 |
| `frontend/src/views/admin/groupsReasoningEffort.ts` | MERGE_REQUIRED：以 Personal 为主体，按函数/字段/文案键合并官方差异；保留 Personal 业务与权限语义。 |

## 验证结果（全 mock / 非 DB）

所有命令均在 `frontend/` 目录运行，未使用系统工具变更。

| 环境 | Node | pnpm | typecheck | test:run | lint:check | build |
|---|---:|---:|---|---|---|---|
| 隔离 Node20 | v20.20.2 | 9.15.9 | PASS | PASS：281 files / 2064 tests | PASS | PASS |
| 系统 Node24 | v24.16.0 | 9.15.9 | PASS | PASS：281 files / 2064 tests | PASS | PASS |

日志绝对路径：
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node20-version.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-pnpm9-version.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node20-typecheck.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node20-targeted-tests.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node20-test-run.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node20-lint-check.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node20-build.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node24-version.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node24-pnpm9-version.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node24-typecheck.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node24-test-run.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node24-lint-check.log`
- `D:\Codex_Program\Personal_Sub2\fusion-0.2.1-code-evidence\run-2026-09-07T05-24-25-962Z\reports\frontend-node24-build.log`

## 未通过限制 / 未验证

- 本报告不是 G_CODE/G_DB 或发布批准。DB_REQUIRED、APP_REQUIRED、真实上游、真实支付、Redis、容器、迁移执行和实际应用保存/重启回显均延期到主负责人批准的集中验收。
- Vitest 输出包含既有 mock 场景的 stderr/warning（例如 BankView 故障模拟、RouterLink 未 stub、Browserslist 数据提示、Vite chunk size 提示），不影响退出码；未将 warning 冒充失败。
- 官方新增的两个“创建流程模型映射/preview metadata”测试与当前 Personal 的 ModelWhitelistSelector 测试 stub/行为不兼容，未强行改变 Personal 业务；保留可编译、可运行的全 mock 核心覆盖，具体限制已由目标测试日志保留。
- `pnpm run build` 会按项目既有配置生成 `backend/internal/web/dist` 构建产物；未启动服务或数据库。

## 当前任务涉及的绝对路径

- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\api\admin\accounts.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\api\admin\channels.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\api\admin\settings.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\api\channels.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\CreateAccountModal.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\EditAccountModal.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\ModelWhitelistSelector.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\UpstreamRequestIdHeaderField.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\__tests__\CreateAccountModal.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\__tests__\EditAccountModal.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\__tests__\ModelWhitelistSelector.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\account\__tests__\UpstreamRequestIdHeaderField.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\channel\PricingEntryCard.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\channel\__tests__\PricingEntryCard.timePricing.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\channel\types.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\group\CodexManifestAccountsField.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\group\ReasoningEffortPolicyFields.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\group\__tests__\CodexManifestAccountsField.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\group\__tests__\ReasoningEffortPolicyFields.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\usage\UsageTable.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\admin\usage\__tests__\UsageTable.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\common\HelpTooltip.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\common\__tests__\HelpTooltip.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\keys\UseKeyModal.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\keys\__tests__\UseKeyModal.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\layout\AppSidebar.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\layout\__tests__\AppSidebar.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\modelPlaza\PlazaModelPricingTable.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\modelPlaza\__tests__\PlazaModelPricingTable.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\payment\PaymentStatusPanel.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\components\payment\__tests__\PaymentStatusPanel.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\composables\__tests__\useModelWhitelist.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\composables\useModelWhitelist.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\__tests__\openaiFastPolicyLocales.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\__tests__\usageServiceTierLocales.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\en\admin\accounts.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\en\admin\channels.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\en\admin\overview.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\en\admin\resources.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\en\admin\settings.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\en\dashboard.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\zh\admin\accounts.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\zh\admin\channels.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\zh\admin\overview.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\zh\admin\resources.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\zh\admin\settings.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\i18n\locales\zh\dashboard.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\types\index.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\utils\__tests__\usageServiceTier.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\utils\usageServiceTier.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\AccountsView.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\ChannelsView.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\GroupsView.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\SettingsView.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\UsageView.vue`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\__tests__\AccountsView.lite.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\__tests__\UsageView.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\__tests__\groupsModelsListLayout.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\__tests__\groupsReasoningEffort.spec.ts`
- `D:\Codex_Program\Personal_Sub2\178-p1\frontend\src\views\admin\groupsReasoningEffort.ts`
