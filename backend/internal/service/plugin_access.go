package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A grant belongs to one runtime, one in-flight business RPC and its deadline.
// No grant is created by configuration or any other lifecycle RPC.
type pluginBusinessGrant struct {
	ctx        context.Context
	cancel     context.CancelFunc
	caller     PluginCaller
	access     string
	userScoped bool
	background bool
}

type pluginRuntimeAccess struct {
	mu       sync.Mutex
	closed   bool
	grants   map[string]*pluginBusinessGrant
	repo     PluginRepository
	id       int64
	revision string
	manifest PluginManifest
	hostInfo PluginHostInfo
}

func newPluginRuntimeAccess(repo PluginRepository, installation *PluginInstallation, host PluginHostInfo) *pluginRuntimeAccess {
	return &pluginRuntimeAccess{
		grants: make(map[string]*pluginBusinessGrant), repo: repo,
		id: installation.ID, revision: installation.BinarySHA256,
		manifest: installation.Manifest, hostInfo: host,
	}
}

func pluginRandomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (a *pluginRuntimeAccess) issue(ctx context.Context, caller PluginCaller, access string, userScoped, background bool) (string, context.Context, func(), error) {
	if a == nil || ctx == nil || ctx.Err() != nil {
		return "", nil, nil, status.Error(codes.Unavailable, "插件业务调用已结束")
	}
	if _, ok := ctx.Deadline(); !ok {
		return "", nil, nil, status.Error(codes.InvalidArgument, "插件业务授权必须有截止时间")
	}
	token, err := pluginRandomToken()
	if err != nil {
		return "", nil, nil, err
	}
	grantCtx, cancel := context.WithCancel(ctx)
	grant := &pluginBusinessGrant{ctx: grantCtx, cancel: cancel, caller: caller, access: access, userScoped: userScoped, background: background}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		cancel()
		return "", nil, nil, status.Error(codes.Unavailable, "插件正在停止")
	}
	a.grants[token] = grant
	a.mu.Unlock()
	var once sync.Once
	revoke := func() {
		once.Do(func() {
			a.mu.Lock()
			delete(a.grants, token)
			a.mu.Unlock()
			cancel()
		})
	}
	return token, grantCtx, revoke, nil
}

func (a *pluginRuntimeAccess) close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closed = true
	grants := a.grants
	a.grants = make(map[string]*pluginBusinessGrant)
	a.mu.Unlock()
	for _, grant := range grants {
		grant.cancel()
	}
}

// authorize rechecks persistent enablement and package revision on every reverse
// call. Tokens from another runtime or an earlier invocation never match here.
func (a *pluginRuntimeAccess) authorize(ctx context.Context, token, capability string, write bool) (*pluginBusinessGrant, context.Context, func(), error) {
	if a == nil || token == "" || len(token) > 256 {
		return nil, nil, nil, status.Error(codes.Unauthenticated, "宿主调用缺少本次业务授权")
	}
	a.mu.Lock()
	grant := a.grants[token]
	closed := a.closed
	a.mu.Unlock()
	if closed || grant == nil || grant.ctx.Err() != nil {
		return nil, nil, nil, status.Error(codes.PermissionDenied, "宿主业务授权已结束或不属于当前运行时")
	}
	if !a.manifest.HasCapability(capability) || (write && grant.access != "write") {
		return nil, nil, nil, status.Error(codes.PermissionDenied, "插件未获授本次宿主操作权限")
	}
	if a.repo == nil {
		return nil, nil, nil, status.Error(codes.Unavailable, "无法确认插件当前启用状态")
	}
	deadline, ok := grant.ctx.Deadline()
	if !ok || !time.Now().Before(deadline) {
		return nil, nil, nil, status.Error(codes.PermissionDenied, "宿主业务授权已过期")
	}
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	stop := context.AfterFunc(grant.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	current, err := a.repo.GetByID(callCtx, a.id)
	if err != nil {
		cleanup()
		return nil, nil, nil, status.Error(codes.Unavailable, "无法确认插件当前启用状态")
	}
	if current == nil || !current.Manifest.IsGeneric() || current.State != PluginStateEnabled || current.BinarySHA256 != a.revision || !EvaluatePluginCompatibility(current.Manifest, a.hostInfo).Compatible {
		cleanup()
		return nil, nil, nil, status.Error(codes.PermissionDenied, "插件已停用、替换或不再兼容")
	}
	if grant.ctx.Err() != nil || callCtx.Err() != nil {
		cleanup()
		return nil, nil, nil, status.Error(codes.PermissionDenied, "宿主业务授权已结束")
	}
	return grant, callCtx, cleanup, nil
}

func (s *pluginHostServiceServer) authorizeKV(ctx context.Context, token, namespace string, write bool) (context.Context, string, func(), error) {
	if s.access == nil {
		return ctx, namespace, func() {}, nil
	}
	capability := PluginCapabilityStorageRead
	if write {
		capability = PluginCapabilityStorageWrite
	}
	grant, callCtx, cleanup, err := s.access.authorize(ctx, token, capability, write)
	if err != nil {
		return nil, "", nil, err
	}
	if grant.userScoped {
		if grant.caller.UserID <= 0 {
			cleanup()
			return nil, "", nil, status.Error(codes.PermissionDenied, "用户存储授权缺少调用者")
		}
		namespace = "user." + strconv.FormatInt(grant.caller.UserID, 10) + "." + namespace
	} else {
		namespace = "shared." + namespace
	}
	return callCtx, namespace, cleanup, nil
}
