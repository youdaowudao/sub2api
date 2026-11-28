.PHONY: build build-backend build-frontend test test-backend test-frontend test-frontend-critical

FRONTEND_CRITICAL_VITEST := \
	src/features/guide/__tests__/GuideHost.spec.ts \
	src/features/guide/__tests__/GuideSettingsCard.spec.ts \
	src/features/guide/__tests__/QuickStartArticle.spec.ts \
	src/features/guide/__tests__/attention.spec.ts \
	src/features/guide/__tests__/businessEvents.spec.ts \
	src/features/guide/__tests__/config.spec.ts \
	src/features/guide/__tests__/controller.spec.ts \
	src/features/guide/__tests__/driver.spec.ts \
	src/features/guide/__tests__/flow.spec.ts \
	src/features/guide/__tests__/integrationSurface.spec.ts \
	src/features/guide/__tests__/quickStartContent.spec.ts \
	src/features/guide/__tests__/registration.spec.ts \
	src/features/guide/__tests__/sharedComponents.contract.spec.ts \
	src/features/guide/__tests__/steps.spec.ts \
	src/features/guide/__tests__/tourSession.spec.ts \
	src/views/auth/__tests__/RegisterView.spec.ts \
	src/views/auth/__tests__/EmailVerifyView.spec.ts \
	src/views/user/__tests__/CustomPageView.spec.ts \
	src/views/user/__tests__/UserOrdersView.guide.spec.ts \
	src/components/common/__tests__/DataTable.spec.ts \
	src/components/layout/__tests__/AppSidebar.spec.ts \
	src/i18n/__tests__/localeKeyCompleteness.spec.ts \
	src/api/__tests__/client.spec.ts \
	src/api/__tests__/tokenRefresh.spec.ts \
	src/api/__tests__/keys.bulkUpdate.spec.ts \
	src/composables/__tests__/useModelWhitelist.spec.ts \
	src/components/account/__tests__/OpenAIReferralCell.spec.ts \
	src/components/account/__tests__/OpenAIReferralCell.transport.spec.ts \
	src/components/account/__tests__/OpenAIQuotaResetCell.spark_shadow.spec.ts \
	src/constants/__tests__/platforms.spec.ts \
	src/components/account/__tests__/credentialsBuilder.platformCatalog.spec.ts \
	src/components/account/__tests__/CNProviderQuotaCell.spec.ts \
	src/components/account/__tests__/CreateAccountModal.spec.ts \
	src/components/account/__tests__/EditAccountModal.spec.ts \
	src/components/account/__tests__/credentialsBuilder.spec.ts \
	src/components/account/__tests__/OpenCodeGoProtocolRulesEditor.spec.ts \
	src/components/keys/__tests__/BulkEditKeysModal.spec.ts \
	src/components/admin/user/__tests__/UserPlatformQuotaModal.spec.ts \
	src/views/user/__tests__/KeysView.spec.ts \
	src/api/__tests__/channelMonitorV2.spec.ts \
	src/views/auth/__tests__/LinuxDoCallbackView.spec.ts \
	src/views/auth/__tests__/WechatCallbackView.spec.ts \
	src/views/user/__tests__/PaymentView.spec.ts \
	src/views/user/__tests__/PaymentResultView.spec.ts \
	src/views/user/__tests__/ChannelStatusView.mode.spec.ts \
	src/components/user/profile/__tests__/ProfileInfoCard.spec.ts \
	src/components/user/profile/__tests__/ProfileBalanceNotifyCard.spec.ts \
	src/views/user/__tests__/ProfileView.spec.ts \
	src/components/user/profile/__tests__/ProfileIdentityBindingsSection.spec.ts \
	src/views/admin/__tests__/SettingsView.spec.ts \
	src/views/admin/__tests__/DashboardView.spec.ts \
	src/views/admin/__tests__/GroupsView.displayOrder.spec.ts \
	src/api/__tests__/admin.groups.displayOrder.spec.ts \
	src/components/common/__tests__/Select.keyboard.spec.ts \
	src/views/admin/__tests__/UsageView.spec.ts \
	src/views/admin/__tests__/AccountsView.lite.spec.ts \
	src/views/admin/__tests__/AccountsView.cacheHitRate.spec.ts \
	src/components/admin/usage/__tests__/UsageStatsCards.spec.ts \
	src/views/admin/ops/components/__tests__/OpsErrorDetailModal.spec.ts \
	src/components/account/__tests__/AccountScheduleTimer.spec.ts \
	src/api/__tests__/accounts.scheduleTimer.spec.ts \
	src/components/admin/usage/__tests__/UsageFilters.spec.ts \
	src/components/admin/usage/__tests__/AccountPerformancePanel.spec.ts \
	src/composables/__tests__/useAdminUserHistory.spec.ts \
	src/views/admin/ops/__tests__/OpsDashboard.spec.ts \
	src/views/admin/ops/components/__tests__/OpsErrorRecordsPanel.interactions.spec.ts \
	src/views/admin/ops/components/__tests__/OpsRequestDetailsModal.spec.ts \
	src/components/admin/dashboard/__tests__/AdminDashboardUserRanking.spec.ts \
	src/components/admin/dashboard/__tests__/AdminDashboardGroupCacheRate.spec.ts \
	src/components/charts/__tests__/TokenUsageTrend.spec.ts \
	src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorFormat.spec.ts \
	src/features/channel-monitor-v2/__tests__/monitorZoom.spec.ts \
	src/components/admin/channel/__tests__/PricingEntryCard.modelDefaultPrice.spec.ts

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
