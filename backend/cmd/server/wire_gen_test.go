package main

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	require.NotPanics(t, testServerCleanup(nil, nil))
}

type videoRelayCleanupProbe struct {
	service.VideoRelayRepository
	started, canceled, released chan struct{}
}

func (probe *videoRelayCleanupProbe) ClaimDue(ctx context.Context) (*service.VideoRelayTask, error) {
	close(probe.started)
	<-ctx.Done()
	close(probe.canceled)
	<-probe.released
	return nil, ctx.Err()
}

type videoRelayCleanupDriver struct {
	dialect.Driver
	closed chan struct{}
}

func (driver *videoRelayCleanupDriver) Close() error {
	close(driver.closed)
	return nil
}

func TestProvideCleanupWaitsForVideoTrackingBeforeClosingEnt(t *testing.T) {
	probe := &videoRelayCleanupProbe{
		started: make(chan struct{}), canceled: make(chan struct{}), released: make(chan struct{}),
	}
	driver := &videoRelayCleanupDriver{closed: make(chan struct{})}
	tracker := service.ProvideVideoRelayTracker(service.NewVideoRelayService(probe, nil, nil))
	released := false
	t.Cleanup(func() {
		if !released {
			close(probe.released)
		}
		tracker.Stop()
	})
	cleanup := testServerCleanup(ent.NewClient(ent.Driver(driver)), tracker)
	select {
	case <-probe.started:
	case <-time.After(time.Second):
		t.Fatal("video tracking did not start its database call")
	}
	done := make(chan struct{})
	go func() {
		cleanup()
		close(done)
	}()
	select {
	case <-probe.canceled:
	case <-time.After(time.Second):
		t.Fatal("the cleanup entry did not cancel video tracking")
	}
	select {
	case <-driver.closed:
		t.Fatal("cleanup closed the database before the video call returned")
	case <-time.After(20 * time.Millisecond):
	}
	close(probe.released)
	released = true
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not return after video tracking exited")
	}
	select {
	case <-driver.closed:
	default:
		t.Fatal("cleanup did not close the database")
	}
}

func testServerCleanup(entClient *ent.Client, tracker *service.VideoRelayTracker) func() {
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	codexVersionSyncSvc := service.NewOpenAICodexVersionSyncService(nil, nil, nil, time.Second)
	claudeCodeVersionSyncSvc := service.NewClaudeCodeVersionSyncService(nil, nil, nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)

	return provideCleanup(
		entClient,
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		nil, // opsService
		nil, // opsIngressRejectAggregator
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		nil, // cnProviderBalanceCheck
		codexVersionSyncSvc,
		claudeCodeVersionSyncSvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		service.NewGeminiCachedContentService(nil, nil, cfg),
		&service.BatchImageCleanupService{},
		nil, // batchImageWorker
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		nil, // usageBillingLanes
		service.ProvideBalanceNotifyService(nil, nil, nil, nil),
		service.NewDigestSessionStore(),
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // channelMonitorRunner
		nil, // channelMonitorV2Aggregator
		nil, // quotaFlusher
		nil, // upstreamBillingProbe
		nil, // ollamaCloudUsage
		nil, // opencodeGoUsage
		nil, // auditLog
		nil, // openAIAutoReset
		nil, // promptAudit
		nil, // pluginManager
		tracker,
		nil, // userAccountBlocks
	)
}
