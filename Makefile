.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical

FRONTEND_CRITICAL_VITEST := \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/api/__tests__/channelMonitorV2.spec.ts \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/views/user/__tests__/ChannelStatusView.mode.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorFormat.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorZoom.spec.ts

# v0.1.179-v0.1.183 P1 merge regression specs
FRONTEND_CRITICAL_VITEST += \
	src/components/account/__tests__/CNProviderBalanceCell.spec.ts \
	src/components/account/__tests__/CNProviderQuotaCell.spec.ts \
	src/components/account/__tests__/CnBaseUrlPresets.spec.ts \
	src/components/account/__tests__/credentialsBuilder.cnAdaptive.spec.ts \
	src/components/account/__tests__/longContextBilling.spec.ts \
	src/components/admin/monitor/__tests__/MonitorPrimaryModelCell.spec.ts \
	src/components/admin/user/__tests__/UserEditModal.spec.ts \
	src/components/modelPlaza/__tests__/PlazaGroupSection.spec.ts \
	src/constants/__tests__/platforms.spec.ts \
	src/views/admin/__tests__/AccountsView.priorityColumn.spec.ts \
	src/views/admin/__tests__/ChannelMonitorView.checkModeBadge.spec.ts \
	src/views/admin/__tests__/GroupsView.compositePlatforms.spec.ts \
	src/views/admin/__tests__/PluginsView.spec.ts \
	src/views/admin/__tests__/ProxiesView.ipv6.spec.ts \
	src/views/admin/__tests__/channelPlatformOptions.spec.ts \
	src/views/admin/__tests__/platformFilterCatalogUsage.spec.ts \
	src/views/admin/ops/components/__tests__/OpsErrorDetailModal.spec.ts

# 一键编译前后端
build: build-backend build-frontend

# 编译后端（复用 backend/Makefile）
build-backend:
	@$(MAKE) -C backend build

# 编译前端（需要已安装依赖）
build-frontend:
	@pnpm --dir frontend run build

# 运行测试（后端 + 前端）
test: test-backend test-frontend

test-backend:
	@$(MAKE) -C backend test

test-frontend:
	@pnpm --dir frontend run lint:check
	@pnpm --dir frontend run typecheck
	@$(MAKE) test-frontend-critical

test-frontend-critical:
	@pnpm --dir frontend exec vitest run $(FRONTEND_CRITICAL_VITEST)
