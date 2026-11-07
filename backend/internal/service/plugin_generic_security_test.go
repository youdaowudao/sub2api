package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type genericSecurityRepo struct {
	PluginRepository
	mu               sync.Mutex
	installation     *PluginInstallation
	bindings         []PluginBinding
	configWriteError error
}

func (r *genericSecurityRepo) GetByID(ctx context.Context, id int64) (*PluginInstallation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.installation == nil || r.installation.ID != id {
		return nil, errors.New("missing")
	}
	copy := *r.installation
	return &copy, nil
}
func (r *genericSecurityRepo) List(ctx context.Context) ([]*PluginInstallation, error) {
	r.mu.Lock()
	installation := r.installation
	r.mu.Unlock()
	if installation == nil {
		return []*PluginInstallation{}, nil
	}
	value, err := r.GetByID(ctx, installation.ID)
	if err != nil {
		return nil, err
	}
	return []*PluginInstallation{value}, nil
}
func (r *genericSecurityRepo) change(fn func(*PluginInstallation)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.installation)
}

func genericSecurityFixture(t *testing.T) (*PluginManager, *pluginHostServiceServer, *genericSecurityRepo) {
	t.Helper()
	manifest := extensionContractManifest(t, "backend")
	manifest.Capabilities = append(manifest.Capabilities, PluginCapability{ID: PluginCapabilityStorageRead}, PluginCapability{ID: PluginCapabilityStorageWrite}, PluginCapability{ID: PluginCapabilityUsersCreatedRead}, PluginCapability{ID: PluginCapabilityBackendBackground})
	installation := &PluginInstallation{ID: 171, PluginKey: manifest.ID, Version: manifest.Version, Manifest: manifest, BinarySHA256: strings.Repeat("a", 64), State: PluginStateEnabled}
	repo := &genericSecurityRepo{installation: installation}
	manager := &PluginManager{repo: repo, hostInfo: PluginHostInfo{Version: "0.1.179"}, kvStore: newFakePluginKVStore(), runtimes: make(map[int64]*pluginRuntime)}
	host, ok := manager.buildHostServices(installation).(*pluginHostServiceServer)
	require.True(t, ok)
	return manager, host, repo
}

func genericSecurityGrant(t *testing.T, host *pluginHostServiceServer, caller PluginCaller, access string, userScoped, background bool) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	token, _, revoke, err := host.access.issue(ctx, caller, access, userScoped, background)
	require.NoError(t, err)
	cleanup := func() { revoke(); cancel() }
	t.Cleanup(cleanup)
	return token, cleanup
}

func TestPluginGenericGrantScopesAndRevocation(t *testing.T) {
	manager, host, repo := genericSecurityFixture(t)
	reader, _ := genericSecurityGrant(t, host, PluginCaller{UserID: 7, Role: RoleUser}, "read", true, false)
	_, err := host.KVSet(context.Background(), &pluginv1.KVSetRequest{Namespace: "state", Key: "key", Value: []byte("forbidden"), AccessToken: reader})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	writer, revoke := genericSecurityGrant(t, host, PluginCaller{UserID: 7, Role: RoleAdmin}, "write", true, false)
	_, err = host.KVSet(context.Background(), &pluginv1.KVSetRequest{Namespace: "state", Key: "key", Value: []byte("private"), AccessToken: writer})
	require.NoError(t, err)
	value, found, err := manager.kvStore.Get(context.Background(), repo.installation.PluginKey, "user.7.state", "key")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "private", string(value))
	_, found, err = manager.kvStore.Get(context.Background(), repo.installation.PluginKey, "shared.state", "key")
	require.NoError(t, err)
	require.False(t, found)
	other, _ := genericSecurityGrant(t, host, PluginCaller{UserID: 8, Role: RoleUser}, "read", true, false)
	result, err := host.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: other})
	require.NoError(t, err)
	require.False(t, result.Found)
	shared, _ := genericSecurityGrant(t, host, PluginCaller{UserID: 7, Role: RoleAdmin}, "write", false, false)
	_, err = host.KVSet(context.Background(), &pluginv1.KVSetRequest{Namespace: "state", Key: "key", Value: []byte("shared"), AccessToken: shared})
	require.NoError(t, err)
	revoke()
	_, err = host.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: writer})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	otherHost, ok := manager.buildHostServices(repo.installation).(*pluginHostServiceServer)
	require.True(t, ok)
	_, err = otherHost.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: shared})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	repo.change(func(value *PluginInstallation) { value.State = PluginStateDisabled })
	_, err = host.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: shared})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	repo.change(func(value *PluginInstallation) {
		value.State = PluginStateEnabled
		value.BinarySHA256 = strings.Repeat("b", 64)
	})
	_, err = host.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: shared})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

type genericSecurityCreations struct {
	includeEmail bool
	limit        int
}

func (*genericSecurityCreations) RecordCompleted(context.Context, int64) error { return nil }
func (s *genericSecurityCreations) List(_ context.Context, after, through int64, limit int, email bool) (*PluginUserCreationPage, error) {
	s.includeEmail = email
	s.limit = limit
	return &PluginUserCreationPage{Items: []PluginUserCreation{{EventID: "user.creation_completed:5", UserID: 5, Email: "private@example.test", CreatedAt: time.Unix(1, 0), CompletedAt: time.Unix(2, 0)}}, NextUserID: 5, ThroughUserID: 5, Done: true}, nil
}

func TestPluginGenericCreationObservationRequiresRoleAndEmailGrant(t *testing.T) {
	_, host, _ := genericSecurityFixture(t)
	store := &genericSecurityCreations{}
	host.creations = store
	user, _ := genericSecurityGrant(t, host, PluginCaller{UserID: 7, Role: RoleUser}, "read", true, false)
	_, err := host.ListUserCreations(context.Background(), &pluginv1.ListUserCreationsRequest{AccessToken: user})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	admin, _ := genericSecurityGrant(t, host, PluginCaller{UserID: 1, Role: RoleAdmin}, "read", false, false)
	page, err := host.ListUserCreations(context.Background(), &pluginv1.ListUserCreationsRequest{AccessToken: admin, Limit: 999})
	require.NoError(t, err)
	require.False(t, store.includeEmail)
	require.Equal(t, 100, store.limit)
	require.Empty(t, page.Items[0].Email)
	host.access.manifest.Capabilities = append(host.access.manifest.Capabilities, PluginCapability{ID: PluginCapabilityUsersEmailRead})
	background, _ := genericSecurityGrant(t, host, PluginCaller{}, "write", false, true)
	page, err = host.ListUserCreations(context.Background(), &pluginv1.ListUserCreationsRequest{AccessToken: background})
	require.NoError(t, err)
	require.True(t, store.includeEmail)
	require.Equal(t, "private@example.test", page.Items[0].Email)
}

type genericSecurityBackend struct {
	pluginv1.BackendPluginClient
	invoke func(context.Context, *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error)
	run    func(context.Context, *pluginv1.RunOnceRequest) (*pluginv1.RunOnceResponse, error)
}

func (b *genericSecurityBackend) Invoke(ctx context.Context, req *pluginv1.InvokeRequest, _ ...grpc.CallOption) (*pluginv1.InvokeResponse, error) {
	return b.invoke(ctx, req)
}
func (b *genericSecurityBackend) RunOnce(ctx context.Context, req *pluginv1.RunOnceRequest, _ ...grpc.CallOption) (*pluginv1.RunOnceResponse, error) {
	return b.run(ctx, req)
}
func (b *genericSecurityBackend) Health(context.Context, *pluginv1.HealthRequest, ...grpc.CallOption) (*pluginv1.HealthResponse, error) {
	return &pluginv1.HealthResponse{Healthy: true}, nil
}

func genericSecurityRuntime(t *testing.T, manager *PluginManager, host *pluginHostServiceServer, backend pluginv1.BackendPluginClient) *pluginRuntime {
	t.Helper()
	installation, err := manager.repo.GetByID(context.Background(), 171)
	require.NoError(t, err)
	runtime := &pluginRuntime{installation: installation, backend: backend, access: host.access, done: make(chan struct{}), client: hcplugin.NewClient(&hcplugin.ClientConfig{HandshakeConfig: pluginv1.HandshakeConfig, Plugins: pluginv1.BackendClientPluginMap(), AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolGRPC}})}
	manager.runtimes[171] = runtime
	t.Cleanup(runtime.kill)
	return runtime
}

func TestPluginGenericInvokeUsesTrustedCallerAndRevokesGrant(t *testing.T) {
	manager, host, _ := genericSecurityFixture(t)
	var token string
	backend := &genericSecurityBackend{invoke: func(ctx context.Context, req *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error) {
		require.Equal(t, int64(7), req.Caller.UserId)
		require.Equal(t, RoleUser, req.Caller.Role)
		require.JSONEq(t, `{"user_id":999,"role":"admin"}`, string(req.PayloadJson))
		token = req.AccessToken
		require.NotEmpty(t, token)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 10*time.Second)
		_, err := host.KVGet(ctx, &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: token})
		require.NoError(t, err)
		_, err = host.KVSet(ctx, &pluginv1.KVSetRequest{Namespace: "state", Key: "key", AccessToken: token})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
		return &pluginv1.InvokeResponse{PayloadJson: []byte(`{"ok":true}`)}, nil
	}}
	genericSecurityRuntime(t, manager, host, backend)
	result, err := manager.Invoke(context.Background(), 171, "echo", "read", json.RawMessage(`{"user_id":999,"role":"admin"}`), PluginCaller{UserID: 7, Role: RoleUser})
	require.NoError(t, err)
	require.NotEmpty(t, result.RequestID)
	require.JSONEq(t, `{"ok":true}`, string(result.Payload))
	_, err = host.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "key", AccessToken: token})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestPluginGenericInvokeACLAndWriteUncertainty(t *testing.T) {
	manager, host, repo := genericSecurityFixture(t)
	repo.change(func(value *PluginInstallation) {
		value.Manifest.Backend.Methods = append(value.Manifest.Backend.Methods, PluginBackendMethod{Name: "change", Role: RoleAdmin, Access: "write"})
	})
	calls := 0
	backend := &genericSecurityBackend{invoke: func(context.Context, *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error) {
		calls++
		return nil, status.Error(codes.Unavailable, "password=must-not-leak")
	}}
	genericSecurityRuntime(t, manager, host, backend)
	_, err := manager.Invoke(context.Background(), 171, "change", "write", json.RawMessage(`{}`), PluginCaller{UserID: 7, Role: RoleUser})
	var failure *PluginExtensionError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "PLUGIN_FORBIDDEN", failure.Reason)
	require.Zero(t, calls)
	require.False(t, failure.OutcomeUnknown)
	_, err = manager.Invoke(context.Background(), 171, "change", "read", json.RawMessage(`{}`), PluginCaller{UserID: 1, Role: RoleAdmin})
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "PLUGIN_FORBIDDEN", failure.Reason)
	require.Zero(t, calls)
	_, err = manager.Invoke(context.Background(), 171, "change", "write", json.RawMessage(`{}`), PluginCaller{UserID: 1, Role: RoleAdmin})
	require.ErrorAs(t, err, &failure)
	require.Equal(t, 1, calls)
	require.True(t, failure.OutcomeUnknown)
	require.False(t, failure.Retryable)
	require.NotContains(t, err.Error(), "must-not-leak")
	_, err = manager.Invoke(context.Background(), 171, "echo", "read", json.RawMessage(`[]`), PluginCaller{UserID: 7, Role: RoleUser})
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "PLUGIN_INVALID_REQUEST", failure.Reason)
	require.Equal(t, 1, calls)
	repo.change(func(value *PluginInstallation) { value.State = PluginStateDisabled })
	_, err = manager.Invoke(context.Background(), 171, "echo", "read", json.RawMessage(`{}`), PluginCaller{UserID: 7, Role: RoleUser})
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "PLUGIN_DISABLED", failure.Reason)
	require.Equal(t, 1, calls)
}

type genericSecurityLeader struct {
	acquire   bool
	err       error
	acquires  int
	releases  int
	ttl       time.Duration
	onRelease func()
}

func (l *genericSecurityLeader) TryAcquireLeaderLock(_ context.Context, key, owner string, ttl time.Duration) (bool, error) {
	l.acquires++
	l.ttl = ttl
	return l.acquire, l.err
}
func (l *genericSecurityLeader) ReleaseLeaderLock(context.Context, string, string) error {
	l.releases++
	if l.onRelease != nil {
		l.onRelease()
	}
	return nil
}

func TestPluginGenericBackgroundFailsClosedAndRevokesBeforeUnlock(t *testing.T) {
	manager, host, _ := genericSecurityFixture(t)
	var token string
	calls := 0
	backend := &genericSecurityBackend{run: func(ctx context.Context, req *pluginv1.RunOnceRequest) (*pluginv1.RunOnceResponse, error) {
		calls++
		token = req.AccessToken
		_, err := host.KVSet(ctx, &pluginv1.KVSetRequest{Namespace: "state", Key: "background", Value: []byte("ok"), AccessToken: token})
		require.NoError(t, err)
		return &pluginv1.RunOnceResponse{}, nil
	}}
	runtime := genericSecurityRuntime(t, manager, host, backend)
	manager.runBackgroundOnce(context.Background(), runtime)
	require.Zero(t, calls)
	leader := &genericSecurityLeader{acquire: true, err: errors.New("lock unavailable")}
	manager.leader = leader
	manager.runBackgroundOnce(context.Background(), runtime)
	require.Zero(t, calls)
	require.Zero(t, leader.releases)
	leader.err = nil
	leader.onRelease = func() {
		_, err := host.KVGet(context.Background(), &pluginv1.KVGetRequest{Namespace: "state", Key: "background", AccessToken: token})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	}
	manager.runBackgroundOnce(context.Background(), runtime)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, leader.releases)
	require.Equal(t, 30*time.Second, leader.ttl)
}

func TestPluginGenericBackgroundCancellationKillsBeforeUnlock(t *testing.T) {
	manager, host, _ := genericSecurityFixture(t)
	backend := &genericSecurityBackend{run: func(ctx context.Context, req *pluginv1.RunOnceRequest) (*pluginv1.RunOnceResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	runtime := genericSecurityRuntime(t, manager, host, backend)
	leader := &genericSecurityLeader{acquire: true, onRelease: func() {
		require.True(t, runtime.draining.Load(), "process cancellation must precede releasing the owner lease")
	}}
	manager.leader = leader
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	manager.runBackgroundOnce(ctx, runtime)
	require.Equal(t, 1, leader.releases)
	require.True(t, runtime.draining.Load())
	require.Nil(t, manager.runtimes[171])
}

func (r *genericSecurityRepo) GetByKey(ctx context.Context, key string) (*PluginInstallation, error) {
	r.mu.Lock()
	installation := r.installation
	r.mu.Unlock()
	if installation == nil || installation.PluginKey != key {
		return nil, sql.ErrNoRows
	}
	return r.GetByID(ctx, installation.ID)
}
func (r *genericSecurityRepo) Install(_ context.Context, installation *PluginInstallation, bindings []PluginBinding) (*PluginInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := *installation
	value.ID = 171
	if r.installation != nil {
		value.ConfigEncrypted = r.installation.ConfigEncrypted
	}
	r.installation = &value
	r.bindings = append([]PluginBinding{}, bindings...)
	copy := value
	return &copy, nil
}
func (r *genericSecurityRepo) UpdateState(_ context.Context, id int64, state, lastError string, enabledAt *time.Time, revision, expectedState string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.installation == nil || r.installation.ID != id || r.installation.BinarySHA256 != revision || (expectedState != "" && r.installation.State != expectedState) {
		return ErrPluginStateChanged
	}
	r.installation.State = state
	r.installation.LastError = lastError
	r.installation.EnabledAt = enabledAt
	return nil
}
func (r *genericSecurityRepo) UpdateBindingsAndState(ctx context.Context, id int64, bindings []PluginBinding, state, lastError string, enabledAt *time.Time, expectedState, revision string) error {
	if err := r.UpdateState(ctx, id, state, lastError, enabledAt, revision, expectedState); err != nil {
		return err
	}
	r.mu.Lock()
	r.bindings = append([]PluginBinding{}, bindings...)
	r.mu.Unlock()
	return nil
}
func (r *genericSecurityRepo) UpdateConfig(_ context.Context, id int64, value, revision string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.configWriteError != nil {
		return r.configWriteError
	}
	if r.installation == nil || r.installation.ID != id || r.installation.BinarySHA256 != revision {
		return ErrPluginStateChanged
	}
	r.installation.ConfigEncrypted = value
	return nil
}

func TestPluginGenericFrontendLifecycleNeverStartsRuntime(t *testing.T) {
	cfg := testPluginConfig(t.TempDir(), true)
	repo := &genericSecurityRepo{}
	manager := NewPluginManager(repo, pluginTokenEncryptor{}, cfg, PluginHostInfo{Version: "0.1.179"}, nil)
	manifest := extensionContractManifest(t, "frontend")
	installation, err := manager.Install(context.Background(), bytes.NewReader(buildPluginArchive(t, manifest, nil, "", nil)), nil)
	require.NoError(t, err)
	require.Empty(t, installation.BinaryPath)
	require.Empty(t, repo.bindings)
	installation, err = manager.Enable(context.Background(), 171, true, 0)
	require.NoError(t, err)
	require.Equal(t, PluginStateEnabled, installation.State)
	require.Empty(t, manager.runtimes)
	require.Nil(t, manager.route.Load())
	saved, err := manager.SaveConfig(context.Background(), 171, json.RawMessage(`{"color":"blue"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"color":"blue"}`, string(saved))
	require.Empty(t, manager.runtimes)
	result, err := manager.Test(context.Background(), 171)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Empty(t, manager.runtimes)
	entries, err := manager.ListEntries(context.Background(), PluginCaller{UserID: 7, Role: RoleUser})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.NotNil(t, entries[0].Methods)
	require.Empty(t, entries[0].Methods)
	_, err = manager.Disable(context.Background(), 171)
	require.NoError(t, err)
	entries, err = manager.ListEntries(context.Background(), PluginCaller{UserID: 7, Role: RoleUser})
	require.NoError(t, err)
	require.NotNil(t, entries)
	require.Empty(t, entries)
	_, err = manager.SaveConfig(context.Background(), 171, json.RawMessage(`{"color":"red"}`))
	require.NoError(t, err)
	require.Empty(t, manager.runtimes)
}

type genericRollbackBackend struct{ genericSecurityBackend }

func (*genericRollbackBackend) ValidateConfig(_ context.Context, req *pluginv1.ValidateConfigRequest, _ ...grpc.CallOption) (*pluginv1.ValidateConfigResponse, error) {
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: req.ConfigJson}, nil
}
func (*genericRollbackBackend) ApplyConfig(_ context.Context, req *pluginv1.ApplyConfigRequest, _ ...grpc.CallOption) (*pluginv1.ApplyConfigResponse, error) {
	if string(req.ConfigJson) == `{}` {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: "old config rejected"}, nil
	}
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}
func TestPluginGenericConfigRollbackFailureRevokesRuntime(t *testing.T) {
	manager, host, repo := genericSecurityFixture(t)
	manager.encryptor = pluginTokenEncryptor{}
	repo.configWriteError = errors.New("database unavailable")
	runtime := genericSecurityRuntime(t, manager, host, &genericRollbackBackend{})
	_, err := manager.SaveConfig(context.Background(), 171, json.RawMessage(`{"next":true}`))
	require.ErrorContains(t, err, "配置回滚失败")
	require.True(t, runtime.draining.Load())
	require.Nil(t, manager.runtimes[171])
	require.Empty(t, repo.installation.ConfigEncrypted)
}

// The second authoritative read represents a peer completing disable/replace/
// re-enable between initial ACL validation and dispatch to the old local runtime.
type genericReplacementGateRepo struct {
	*genericSecurityRepo
	reads int
}

func (r *genericReplacementGateRepo) GetByID(ctx context.Context, id int64) (*PluginInstallation, error) {
	r.reads++
	if r.reads == 2 {
		r.change(func(value *PluginInstallation) { value.BinarySHA256 = strings.Repeat("b", 64) })
	}
	return r.genericSecurityRepo.GetByID(ctx, id)
}
func TestPluginGenericInvokeRejectsReplacementAtDispatchGate(t *testing.T) {
	for _, access := range []string{"read", "write"} {
		t.Run(access, func(t *testing.T) {
			manager, host, repo := genericSecurityFixture(t)
			repo.change(func(value *PluginInstallation) {
				value.Manifest.Backend.Methods = append(value.Manifest.Backend.Methods, PluginBackendMethod{Name: "change", Role: RoleUser, Access: "write"})
			})
			calls := 0
			backend := &genericSecurityBackend{invoke: func(context.Context, *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error) {
				calls++
				return &pluginv1.InvokeResponse{PayloadJson: []byte(`{}`)}, nil
			}}
			genericSecurityRuntime(t, manager, host, backend)
			manager.repo = &genericReplacementGateRepo{genericSecurityRepo: repo}
			method := "echo"
			if access == "write" {
				method = "change"
			}
			_, err := manager.Invoke(context.Background(), 171, method, access, json.RawMessage(`{}`), PluginCaller{UserID: 7, Role: RoleUser})
			var failure *PluginExtensionError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, "PLUGIN_UNAVAILABLE", failure.Reason)
			require.Contains(t, failure.Message, "当前操作未执行")
			require.False(t, failure.OutcomeUnknown)
			require.Zero(t, calls)
		})
	}
}

func TestPluginGenericInvokeUsesAuditRequestID(t *testing.T) {
	manager, host, _ := genericSecurityFixture(t)
	const requestID = "http-plugin-call-171"
	calls := 0
	backend := &genericSecurityBackend{invoke: func(_ context.Context, request *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error) {
		calls++
		require.Equal(t, requestID, request.RequestId)
		return &pluginv1.InvokeResponse{PayloadJson: []byte(`{}`)}, nil
	}}
	genericSecurityRuntime(t, manager, host, backend)
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, requestID)
	result, err := manager.Invoke(ctx, 171, "echo", "read", json.RawMessage(`{}`), PluginCaller{UserID: 7, Role: RoleUser})
	require.NoError(t, err)
	require.Equal(t, requestID, result.RequestID)
	require.Equal(t, 1, calls)
	_, err = manager.Invoke(ctx, 171, "echo", "read", json.RawMessage(`[]`), PluginCaller{UserID: 7, Role: RoleUser})
	var failure *PluginExtensionError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, requestID, failure.RequestID)
	require.Equal(t, 1, calls)
}
