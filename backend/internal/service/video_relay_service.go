package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

const (
	videoRelayNotSentMessage     = "The video request was not sent. The frozen amount was returned."
	videoRelayRejectedMessage    = "The video request was rejected. The frozen amount was returned."
	videoRelayFailedMessage      = "Video generation failed. The frozen amount was returned."
	videoRelayUnconfirmedMessage = "The video request could not be confirmed. The frozen amount was returned."
	videoRelayDownloadMessage    = "Video content is temporarily unavailable. Retry the download later."
	videoRelayResponseLimit      = 1024 * 1024
)

type VideoRelayService struct {
	repo    VideoRelayRepository
	gateway *OpenAIGatewayService
	keys    *APIKeyService
}

func VideoRelayFailureMessage(reason string) string {
	switch reason {
	case VideoRelayNotSent:
		return videoRelayNotSentMessage
	case VideoRelayUpstreamRejected:
		return videoRelayRejectedMessage
	case VideoRelayUnconfirmed:
		return videoRelayUnconfirmedMessage
	case VideoRelayTrackingExpired:
		return "Video tracking expired. The frozen amount was returned. Submit a new request if you still need the video."
	case VideoRelayAccountDeleted:
		return "The video route was removed. The frozen amount was returned. Submit a new request if you still need the video."
	default:
		return videoRelayFailedMessage
	}
}

func NewVideoRelayService(repo VideoRelayRepository, gateway *OpenAIGatewayService, keys *APIKeyService) *VideoRelayService {
	return &VideoRelayService{repo: repo, gateway: gateway, keys: keys}
}

func (s *VideoRelayService) CheckSubmitMode(key *APIKey) *VideoRelayError {
	if key == nil || key.Group == nil || key.Group.IsSubscriptionType() {
		return &VideoRelayError{Status: http.StatusForbidden, Code: "permission_error", Message: "Video generation requires a balance-billed group."}
	}
	if s.gateway.cfg != nil && s.gateway.cfg.RunMode == config.RunModeSimple {
		return &VideoRelayError{Status: http.StatusForbidden, Code: "permission_error", Message: "Video generation requires standard billing mode."}
	}
	return nil
}

func (s *OpenAIGatewayService) videoRelayPrice(ctx context.Context, key *APIKey, model string, seconds int, resolution string) (*CostBreakdown, float64, *VideoRelayError) {
	missing := invalidVideoRelayRequest("This model has no video price for the requested resolution.")
	resolved := s.resolveOpenAIChannelPricing(ctx, model, key)
	if resolved == nil || resolved.Source != PricingSourceGroup || resolved.Mode != BillingModeVideo {
		return nil, 0, missing
	}
	group := key.Group
	multiplier := resolveVideoRateMultiplier(key, group.RateMultiplier)
	if !group.VideoRateIndependent {
		multiplier = s.ResolveUserGroupRateMultiplier(ctx, key.UserID, group.ID, group.RateMultiplier)
	}
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: model, GroupID: &group.ID, Group: group, UsageUnits: float64(seconds), SizeTier: resolution,
		RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
	})
	if err != nil || cost.TotalCost <= 0 || math.IsNaN(cost.TotalCost) || math.IsInf(cost.TotalCost, 0) ||
		math.IsNaN(cost.ActualCost) || math.IsInf(cost.ActualCost, 0) || cost.ActualCost < 0 {
		return nil, 0, missing
	}
	cost.ActualCost = QuantizeUsageBillingAmount(cost.ActualCost)
	return cost, multiplier, nil
}

func (s *VideoRelayService) selectRoute(ctx context.Context, key *APIKey, input VideoRelayRequest) (*videoRelayRoute, *VideoRelayTask, func(), *VideoRelayError) {
	ctx = WithOpenAIProfitControlSuppressed(ctx)
	mapping, _ := s.gateway.ResolveChannelMappingAndRestrict(ctx, key.GroupID, input.Model)
	specs := &videoRelaySelectionSpecs{seconds: input.Seconds, resolution: input.Resolution}
	ctx = context.WithValue(ctx, videoRelayChannelModelKey{}, videoRelayChannelModel{
		requested: input.Model,
		mapping:   mapping,
		specs:     specs,
	})
	excluded := make(map[int64]struct{})
	for {
		selection, _, err := s.gateway.SelectAccountWithSchedulerForCapability(ctx, key.GroupID, "", "", mapping.MappedModel,
			excluded, OpenAIUpstreamTransportHTTPSSE, OpenAIEndpointCapabilityVideos, false, false, false)
		if err != nil || selection == nil || selection.Account == nil {
			if specs.mismatch.Load() {
				return nil, nil, nil, invalidVideoRelayRequest("No available video route supports these seconds and resolution.")
			}
			return nil, nil, nil, &VideoRelayError{Status: http.StatusServiceUnavailable, Code: "upstream_error", Message: "No video route is available. Try again later."}
		}
		account := selection.Account
		excluded[account.ID] = struct{}{}
		release := selection.ReleaseFunc
		settings, validSettings := account.OpenAIVideoRelaySettings()
		if !validSettings || !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos) {
			releaseVideoRelaySlot(release)
			continue
		}
		seconds, resolution, allowed := settings.resolve(input)
		if !allowed {
			specs.mismatch.Store(true)
			releaseVideoRelaySlot(release)
			continue
		}
		mapped := strings.TrimSpace(account.GetMappedModel(mapping.MappedModel))
		if mapped == "" || utf8.RuneCountInString(mapped) > 100 {
			releaseVideoRelaySlot(release)
			continue
		}
		route, err := s.gateway.videoRelayRoute(account)
		if err != nil {
			releaseVideoRelaySlot(release)
			continue
		}
		cost, multiplier, priceError := s.gateway.videoRelayPrice(ctx, key, input.Model, seconds, resolution)
		if priceError != nil {
			releaseVideoRelaySlot(release)
			return nil, nil, nil, priceError
		}
		if !selection.Acquired {
			slot, err := s.gateway.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
			if err != nil || !slot.Acquired {
				continue
			}
			release = slot.ReleaseFunc
		}
		// Use 128 random bits for the ID and encode them in the public task format.
		var randomID [16]byte
		if _, err := rand.Read(randomID[:]); err != nil {
			releaseVideoRelaySlot(release)
			return nil, nil, nil, videoRelayBillingUnavailable()
		}
		task := &VideoRelayTask{
			TaskID: "video_" + hex.EncodeToString(randomID[:]), UserID: key.UserID, APIKeyID: key.ID, GroupID: key.Group.ID, AccountID: account.ID,
			Model: input.Model, UpstreamModel: mapped, Seconds: seconds, Resolution: resolution,
			TotalCost: cost.TotalCost, HoldAmount: cost.ActualCost, RateMultiplier: multiplier,
			SiteFingerprint: route.site, CredentialFingerprint: route.credential, RouteFingerprint: route.fingerprint,
		}
		return route, task, release, nil
	}
}

type videoRelaySelectionSpecs struct {
	seconds    int
	resolution string
	mismatch   atomic.Bool
}

func videoRelaySpecsMismatch(ctx context.Context, account *Account) bool {
	selection, ok := ctx.Value(videoRelayChannelModelKey{}).(videoRelayChannelModel)
	if !ok || selection.specs == nil {
		return false
	}
	settings, valid := account.OpenAIVideoRelaySettings()
	if !valid {
		return false
	}
	_, _, allowed := settings.resolve(VideoRelayRequest{Seconds: selection.specs.seconds, Resolution: selection.specs.resolution})
	if !allowed {
		selection.specs.mismatch.Store(true)
	}
	return !allowed
}

func releaseVideoRelaySlot(release func()) {
	if release != nil {
		release()
	}
}

func videoRelayBillingUnavailable() *VideoRelayError {
	return &VideoRelayError{Status: http.StatusServiceUnavailable, Code: "billing_error", Message: "Video billing is temporarily unavailable. Check the video status if you have a task ID."}
}

func (s *VideoRelayService) Submit(c *gin.Context, key *APIKey, input VideoRelayRequest) (*VideoRelayTask, *VideoRelayError) {
	if modeError := s.CheckSubmitMode(key); modeError != nil {
		return nil, modeError
	}
	ctx := c.Request.Context()
	route, task, release, selectionError := s.selectRoute(ctx, key, input)
	if selectionError != nil {
		return nil, selectionError
	}
	return s.submitSelected(c, input, route, task, release)
}

func (s *VideoRelayService) submitSelected(c *gin.Context, input VideoRelayRequest, route *videoRelayRoute, task *VideoRelayTask, release func()) (*VideoRelayTask, *VideoRelayError) {
	ctx := c.Request.Context()
	var once sync.Once
	releaseSlot := func() { once.Do(func() { releaseVideoRelaySlot(release) }) }
	defer releaseSlot()
	if err := s.repo.Reserve(ctx, task); err != nil {
		if errors.Is(err, ErrBatchImageInsufficientBalance) {
			return nil, &VideoRelayError{Status: http.StatusPaymentRequired, Code: "insufficient_balance", Message: "Your available balance is too low for this video. The request was not sent."}
		}
		return nil, videoRelayBillingUnavailable()
	}
	s.invalidateFunds(ctx, task, false)

	submitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 120*time.Second)
	defer cancel()
	stopRelease := context.AfterFunc(submitCtx, releaseSlot)
	defer stopRelease()
	sent, err := s.repo.AcquireSend(submitCtx, task)
	if err != nil {
		current := task
		if !errors.Is(err, ErrVideoRelaySendBlocked) {
			current = s.readPersisted(ctx, task)
		}
		// This is the sole submission path and it hasn't entered the client.
		refunded := s.refund(ctx, current, VideoRelayNotSent, videoRelayNotSentMessage)
		if refunded.BillingState != VideoRelayRefunded {
			return nil, videoRelayBillingUnavailable()
		}
		if errors.Is(err, ErrVideoRelaySendBlocked) {
			return nil, &VideoRelayError{Status: http.StatusTooManyRequests, Code: "rate_limit_error", Message: "This video route is confirming an earlier submission. This request was not sent. Try again shortly."}
		}
		return nil, &VideoRelayError{Status: http.StatusServiceUnavailable, Code: "upstream_error", Message: videoRelayNotSentMessage}
	}
	var gotConn atomic.Bool
	traceCtx := httptrace.WithClientTrace(submitCtx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) {
		gotConn.Store(true)
	}})
	req, err := route.submitRequest(traceCtx, input, sent)
	if err != nil || submitCtx.Err() != nil {
		return s.finishUnsent(ctx, sent)
	}
	resp, err := s.gateway.httpUpstream.Do(req, route.proxyURL, route.account.ID, route.account.Concurrency)
	if err != nil {
		_ = s.gateway.handleOpenAIUpstreamTransportError(submitCtx, c, route.account, err, false)
		traced, ok := s.gateway.httpUpstream.(interface{ SupportsVideoRelayConnectionTrace() bool })
		if ok && traced.SupportsVideoRelayConnectionTrace() && !gotConn.Load() {
			return s.finishUnsent(ctx, sent)
		}
		return s.recordSubmission(ctx, sent, "", ""), nil
	}
	defer func() { _ = resp.Body.Close() }()
	requestID := videoRelayRequestID(resp)
	current := sent
	if requestID != "" {
		current = s.recordSubmission(ctx, sent, requestID, "")
	}
	body, bodyErr := readVideoRelayBody(resp.Body, videoRelayResponseLimit)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.recordUpstreamError(ctx, c, route, resp, body, current.UpstreamModel, "http_error")
		if bodyErr == nil && requestID != "" && current.UpstreamRequestID == requestID &&
			explicitVideoRelayRejection(resp.StatusCode, requestID, body) {
			refunded := s.refund(ctx, current, VideoRelayUpstreamRejected, videoRelayRejectedMessage)
			if refunded.BillingState == VideoRelayRefunded && refunded.FailureReason == VideoRelayUpstreamRejected {
				return nil, videoRelayUpstreamError(c, route.account, resp.StatusCode, body, videoRelayRejectedMessage)
			}
			return refunded, nil
		}
		if requestID == "" {
			current = s.recordSubmission(ctx, sent, "", "")
		}
		return current, nil
	}
	reply, parseErr := parseVideoRelayReply(body, "")
	if bodyErr != nil || parseErr != nil || !ValidVideoRelayUpstreamTaskID(reply.TaskID) {
		if requestID == "" {
			current = s.recordSubmission(ctx, sent, "", "")
		}
		return current, nil
	}
	current = s.recordSubmission(ctx, sent, requestID, reply.TaskID)
	if current.BillingState != VideoRelayHeld || current.UpstreamTaskID != reply.TaskID {
		return current, nil
	}
	if reply.Status == VideoRelayStatusCompleted || reply.Status == VideoRelayStatusFailed {
		current = s.applyReply(ctx, current, reply)
	}
	return current, nil
}

func videoRelayRequestID(resp *http.Response) string {
	values := resp.Header.Values("X-Oneapi-Request-Id")
	if len(values) != 1 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func (s *VideoRelayService) finishUnsent(ctx context.Context, sent *VideoRelayTask) (*VideoRelayTask, *VideoRelayError) {
	task := s.refund(ctx, sent, VideoRelayNotSent, videoRelayNotSentMessage)
	if task.BillingState == VideoRelayRefunded && task.FailureReason == VideoRelayNotSent {
		return nil, &VideoRelayError{Status: http.StatusServiceUnavailable, Code: "upstream_error", Message: videoRelayNotSentMessage}
	}
	return task, nil
}

func (s *VideoRelayService) readPersisted(ctx context.Context, fallback *VideoRelayTask) *VideoRelayTask {
	readCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	current, err := s.repo.Get(readCtx, fallback.TaskID, fallback.UserID, fallback.APIKeyID)
	if err == nil && current != nil {
		return current
	}
	return fallback
}

func (s *VideoRelayService) recordSubmission(ctx context.Context, sent *VideoRelayTask, requestID, upstreamTaskID string) *VideoRelayTask {
	writeCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	for range 2 {
		task, err := s.repo.RecordSubmission(writeCtx, sent, requestID, upstreamTaskID)
		if err == nil && task != nil {
			return task
		}
	}
	return s.readPersisted(writeCtx, sent)
}

func (s *VideoRelayService) GetOwned(ctx context.Context, key *APIKey, id string) (*VideoRelayTask, *VideoRelayError) {
	notFound := &VideoRelayError{Status: http.StatusNotFound, Code: "not_found_error", Message: "Video request not found"}
	if key == nil || !ValidVideoRelayTaskID(id) {
		return nil, notFound
	}
	task, err := s.repo.Get(ctx, id, key.UserID, key.ID)
	if errors.Is(err, ErrVideoRelayNotFound) {
		return nil, notFound
	}
	if err != nil {
		return nil, videoRelayBillingUnavailable()
	}
	return task, nil
}

func (s *VideoRelayService) Lookup(c *gin.Context, key *APIKey, id string) (*VideoRelayTask, *VideoRelayError) {
	ctx := c.Request.Context()
	task, err := s.GetOwned(ctx, key, id)
	if err != nil {
		return nil, err
	}
	if task.CanLookupSubmission() {
		return s.lookupSubmission(ctx, c, task), nil
	}
	if task.BillingState == VideoRelayHeld && ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID) {
		task = s.lookupStatus(ctx, c, task)
	}
	return task, nil
}

func (s *VideoRelayService) lookupSubmission(ctx context.Context, c *gin.Context, task *VideoRelayTask) *VideoRelayTask {
	ticket, claimed, err := s.repo.ClaimLookup(ctx, task.TaskID)
	if err != nil {
		return s.readPersisted(ctx, task)
	}
	if !claimed {
		if ticket.BillingState == VideoRelayHeld && ValidVideoRelayUpstreamTaskID(ticket.UpstreamTaskID) {
			return s.lookupStatus(ctx, c, ticket)
		}
		return ticket
	}
	current := ticket
	route, release := s.originalRoute(ctx, ticket)
	if route != nil {
		defer releaseVideoRelaySlot(release)
		readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		logs, err := route.queryCharges(readCtx, s.gateway.httpUpstream)
		cancel()
		if err == nil && len(logs) != 0 {
			current, err = s.repo.ConfirmLookup(ctx, ticket, route.collection.EscapedPath(), logs)
			if err != nil {
				current = s.readPersisted(ctx, ticket)
			}
		}
	}
	if current.BillingState == VideoRelayHeld && !ValidVideoRelayUpstreamTaskID(current.UpstreamTaskID) &&
		!ticket.LastLookupAt.Before(ticket.SentAt.Add(VideoRelayConfirmationWindow)) {
		current = s.refund(ctx, ticket, VideoRelayUnconfirmed, videoRelayUnconfirmedMessage)
	}
	if current.BillingState == VideoRelayHeld && ValidVideoRelayUpstreamTaskID(current.UpstreamTaskID) &&
		route != nil && route.matches(current) {
		return s.lookupStatusOnRoute(ctx, c, current, route)
	}
	return current
}

func (s *VideoRelayService) originalRoute(ctx context.Context, task *VideoRelayTask) (*videoRelayRoute, func()) {
	account, err := s.gateway.accountRepo.GetByID(ctx, task.AccountID)
	if err != nil {
		return nil, nil
	}
	route, err := s.gateway.videoRelayRoute(account)
	if err != nil || !route.matches(task) {
		return nil, nil
	}
	slot, err := s.gateway.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
	if err != nil || !slot.Acquired {
		return nil, nil
	}
	return route, slot.ReleaseFunc
}

func (s *VideoRelayService) lookupStatus(ctx context.Context, c *gin.Context, task *VideoRelayTask) *VideoRelayTask {
	route, release := s.originalRoute(ctx, task)
	if route == nil {
		return task
	}
	defer releaseVideoRelaySlot(release)
	return s.lookupStatusOnRoute(ctx, c, task, route)
}

func (s *VideoRelayService) lookupStatusOnRoute(ctx context.Context, c *gin.Context, task *VideoRelayTask, route *videoRelayRoute) *VideoRelayTask {
	req, err := route.taskRequest(ctx, task.UpstreamTaskID, false)
	if err != nil {
		return task
	}
	resp, err := s.gateway.httpUpstream.Do(req, route.proxyURL, route.account.ID, route.account.Concurrency)
	if err != nil {
		_ = s.gateway.handleOpenAIUpstreamTransportError(ctx, c, route.account, err, false)
		return task
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readVideoRelayBody(resp.Body, videoRelayResponseLimit)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.recordUpstreamError(ctx, c, route, resp, body, task.UpstreamModel, "http_error")
		return task
	}
	if err != nil {
		s.recordUpstreamError(ctx, c, route, resp, nil, task.UpstreamModel, "invalid_response")
		return task
	}
	reply, err := parseVideoRelayReply(body, task.UpstreamTaskID)
	if err != nil {
		s.recordUpstreamError(ctx, c, route, resp, body, task.UpstreamModel, "invalid_response")
		return task
	}
	return s.applyReply(ctx, task, reply)
}

func (s *VideoRelayService) applyReply(ctx context.Context, task *VideoRelayTask, reply videoRelayReply) *VideoRelayTask {
	switch reply.Status {
	case VideoRelayStatusCompleted:
		return s.charge(ctx, task)
	case VideoRelayStatusFailed:
		return s.refund(ctx, task, VideoRelayUpstreamFailed, videoRelayFailedMessage)
	case VideoRelayStatusQueued, VideoRelayStatusInProgress:
		updated, err := s.repo.UpdateProgress(ctx, task, reply.Status, reply.Progress)
		if err == nil && updated != nil {
			return updated
		}
		return s.readPersisted(ctx, task)
	}
	return task
}

func (s *VideoRelayService) refund(ctx context.Context, task *VideoRelayTask, reason, message string) *VideoRelayTask {
	return s.settleVideoRelayForCustomer(ctx, task, VideoRelaySettlement{FailureReason: reason, ErrorMessage: message})
}

func (s *VideoRelayService) charge(ctx context.Context, task *VideoRelayTask) *VideoRelayTask {
	return s.settleVideoRelayForCustomer(ctx, task, VideoRelaySettlement{Completed: true})
}

func (s *VideoRelayService) settleVideoRelayForCustomer(ctx context.Context, task *VideoRelayTask, change VideoRelaySettlement) *VideoRelayTask {
	writeCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	updated, _, err := s.settleVideoRelay(writeCtx, writeCtx, task, change)
	if err != nil {
		logger.LegacyPrintf("service.video_relay", "settlement pending for task %s: %v", task.TaskID, err)
		return s.readPersisted(ctx, task)
	}
	return updated
}

func (s *VideoRelayService) videoRelayBillingParams(task *VideoRelayTask, sources *VideoRelayBillingSources) *postUsageBillingParams {
	return &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: task.HoldAmount, TotalCost: task.TotalCost},
		User: &sources.User, APIKey: &sources.Key, Account: &sources.Account, APIKeyService: s.keys,
		AccountRateMultiplier: sources.Account.BillingRateMultiplier(), Platform: PlatformOpenAI,
	}
}

func (s *VideoRelayService) settleVideoRelay(ctx, cleanup context.Context, task *VideoRelayTask, change VideoRelaySettlement) (*VideoRelayTask, *UsageBillingApplyResult, error) {
	var params *postUsageBillingParams
	if change.Completed {
		change.BuildBilling = func(sources *VideoRelayBillingSources) UsageBillingCommand {
			params = s.videoRelayBillingParams(task, sources)
			return *buildUsageBillingCommand(task.UsageRequestID(), nil, params)
		}
	}
	updated, result, err := s.repo.Settle(ctx, task, change)
	if err != nil || result == nil || !result.Applied {
		return updated, result, err
	}
	charged := updated.BillingState == VideoRelayCharged
	s.invalidateFunds(cleanup, task, charged)
	if charged && params != nil && VideoRelayTrackingCanceled(cleanup) == nil {
		s.finishCharge(cleanup, params, result)
	}
	return updated, result, nil
}

func (s *VideoRelayService) invalidateFunds(ctx context.Context, task *VideoRelayTask, charged bool) {
	if task.HoldAmount == 0 {
		return
	}
	var cacheCtx context.Context
	var cancel context.CancelFunc
	if ctx.Value(videoRelayTrackingContextKey{}) == nil {
		cacheCtx, cancel = detachedBillingContext(ctx)
	} else {
		cacheCtx, cancel = context.WithTimeout(ctx, postUsageBillingTimeout)
	}
	defer cancel()
	if cache := s.gateway.billingCacheService; cache != nil && VideoRelayTrackingCanceled(cacheCtx) == nil {
		_ = cache.InvalidateUserBalance(cacheCtx, task.UserID)
		if charged && VideoRelayTrackingCanceled(cacheCtx) == nil {
			_ = cache.InvalidateAPIKeyRateLimit(cacheCtx, task.APIKeyID)
		}
	}
	if s.keys != nil && VideoRelayTrackingCanceled(cacheCtx) == nil {
		s.keys.InvalidateAuthCacheByUserID(cacheCtx, task.UserID)
	}
}

func (s *VideoRelayService) finishCharge(ctx context.Context, params *postUsageBillingParams, result *UsageBillingApplyResult) {
	deps := s.gateway.billingDeps()
	if deps.deferredService != nil {
		deps.deferredService.ScheduleLastUsedUpdate(params.Account.ID)
	}
	if cost := params.Cost.ActualCost; cost > 0 && deps.userPlatformQuotaRepo != nil && deps.billingCacheService != nil {
		if deps.billingCacheService.HasUserPlatformQuotaLimit(ctx, params.User.ID, params.Platform) && VideoRelayTrackingCanceled(ctx) == nil {
			if ctx.Value(videoRelayTrackingContextKey{}) == nil {
				deps.billingCacheService.IncrementUserPlatformQuotaUsage(params.User.ID, params.Platform, cost)
			} else {
				deps.billingCacheService.incrementUserPlatformQuotaUsage(ctx, params.User.ID, params.Platform, cost)
			}
			if (s.gateway.cfg == nil || !s.gateway.cfg.Database.UserPlatformQuotaFlusherEnabled) && VideoRelayTrackingCanceled(ctx) == nil {
				persistPostUsagePlatformQuota(ctx, deps.userPlatformQuotaRepo, params.User.ID, params.Platform, cost)
			}
		}
	}
	if deps.balanceNotifyService != nil && VideoRelayTrackingCanceled(ctx) == nil {
		deps.balanceNotifyService.submitUsageNotifications(params, deps, result)
	}
}

func (s *VideoRelayService) recordUpstreamError(ctx context.Context, c *gin.Context, route *videoRelayRoute, resp *http.Response, body []byte, upstreamModel, kind string) {
	account := route.account
	s.gateway.handleOpenAIAccountUpstreamError(ctx, account, resp.StatusCode, resp.Header, body, upstreamModel)
	message := sanitizeUpstreamErrorMessage(extractUpstreamErrorMessage(body))
	if kind == "invalid_response" && message == "" {
		message = "Invalid upstream video response"
	}
	setOpsUpstreamError(c, resp.StatusCode, message, "")
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
		UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: videoRelayRequestID(resp),
		Kind: kind, Message: message,
	})
}

func videoRelayUpstreamError(c *gin.Context, account *Account, upstreamStatus int, body []byte, message string) *VideoRelayError {
	status, code, _, matched := applyErrorPassthroughRuleWithMessage(c, account.Platform, upstreamStatus, body,
		http.StatusBadGateway, "upstream_error", message, func([]byte) string { return message })
	if matched {
		if status >= 400 && status <= 599 {
			return &VideoRelayError{Status: status, Code: code, Message: message}
		}
	} else if !account.ShouldHandleErrorCode(upstreamStatus) {
		return &VideoRelayError{Status: http.StatusInternalServerError, Code: "upstream_error", Message: message}
	}
	return &VideoRelayError{Status: http.StatusBadGateway, Code: "upstream_error", Message: message, UpstreamStatus: upstreamStatus}
}
