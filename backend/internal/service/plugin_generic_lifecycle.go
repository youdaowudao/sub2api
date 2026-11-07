package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
)

// Diagnostic/backoff is local runtime information, never desired persistent
// enablement. A crashed generic plugin therefore remains enabled for recovery.
type pluginGenericRuntimeState struct {
	message   string
	failures  int
	nextRetry time.Time
}

func (m *PluginManager) genericFailureLocked(id int64, message string) {
	if m.genericStates == nil {
		m.genericStates = make(map[int64]pluginGenericRuntimeState)
	}
	state := m.genericStates[id]
	state.failures++
	if state.failures > 5 {
		state.failures = 5
	}
	delay := time.Second * time.Duration(1<<uint(state.failures-1))
	state.message = pluginSafeErrorText(message, 500, "")
	state.nextRetry = time.Now().Add(delay)
	m.genericStates[id] = state
}

func (m *PluginManager) recordGenericFailure(id int64, message string) {
	m.mu.Lock()
	m.genericFailureLocked(id, message)
	m.mu.Unlock()
}

func (m *PluginManager) failGenericRuntime(id int64, runtime *pluginRuntime, message string) {
	m.mu.Lock()
	if m.runtimes[id] != runtime {
		m.mu.Unlock()
		return
	}
	delete(m.runtimes, id)
	m.genericFailureLocked(id, message)
	m.mu.Unlock()
	runtime.kill()
}

func (m *PluginManager) stopGenericRuntimes(message string) {
	m.mu.Lock()
	var stale []*pluginRuntime
	for id, runtime := range m.runtimes {
		if runtime.installation != nil && runtime.installation.Manifest.IsGeneric() {
			stale = append(stale, runtime)
			delete(m.runtimes, id)
			m.genericFailureLocked(id, message)
		}
	}
	m.mu.Unlock()
	for _, runtime := range stale {
		runtime.kill()
	}
}

// Called under operationMu by the existing reconciliation loop. Transport route
// publication is deliberately absent from this entire generic lifecycle.
func (m *PluginManager) reconcileGenericLocked(ctx context.Context, installations []*PluginInstallation) {
	current := make(map[int64]*PluginInstallation)
	for _, installation := range installations {
		if installation != nil && installation.Manifest.IsGeneric() {
			current[installation.ID] = installation
		}
	}
	m.mu.Lock()
	var stale []*pluginRuntime
	for id, runtime := range m.runtimes {
		if runtime.installation == nil || !runtime.installation.Manifest.IsGeneric() {
			continue
		}
		installation := current[id]
		if installation == nil || installation.State != PluginStateEnabled || !installation.Manifest.HasRuntime() || installation.BinarySHA256 != runtime.installation.BinarySHA256 || installation.ConfigEncrypted != runtime.installation.ConfigEncrypted || !EvaluatePluginCompatibility(installation.Manifest, m.hostInfo).Compatible {
			delete(m.runtimes, id)
			stale = append(stale, runtime)
		}
	}
	for id := range m.genericStates {
		if installation := current[id]; installation == nil || installation.State != PluginStateEnabled {
			delete(m.genericStates, id)
		}
	}
	m.mu.Unlock()
	for _, runtime := range stale {
		runtime.kill()
	}
	for _, installation := range installations {
		if installation == nil || !installation.Manifest.IsGeneric() {
			continue
		}
		if installation.State == PluginStateStarting && m.startingStateExpired(installation) {
			_ = m.repo.UpdateState(ctx, installation.ID, PluginStateDisabled, "插件启动超时，已自动恢复为停用状态", nil, installation.BinarySHA256, PluginStateStarting)
			continue
		}
		if installation.State != PluginStateEnabled {
			continue
		}
		compatibility := EvaluatePluginCompatibility(installation.Manifest, m.hostInfo)
		if !compatibility.Compatible {
			m.recordGenericFailure(installation.ID, compatibility.Message)
			continue
		}
		if !installation.Manifest.HasRuntime() {
			continue
		}
		m.mu.Lock()
		runtime := m.runtimes[installation.ID]
		diagnostic := m.genericStates[installation.ID]
		m.mu.Unlock()
		if runtime != nil && runtime.alive() {
			healthCtx, cancel := context.WithTimeout(ctx, time.Second)
			err := runtime.checkHealth(healthCtx)
			cancel()
			if err != nil {
				m.failGenericRuntime(installation.ID, runtime, "插件健康检查失败，将重试恢复："+err.Error())
			}
			continue
		}
		if runtime != nil {
			m.failGenericRuntime(installation.ID, runtime, "插件进程已退出，将重试恢复")
			continue
		}
		if time.Now().Before(diagnostic.nextRetry) {
			continue
		}
		local, err := m.ensureLocalInstallation(ctx, installation)
		if err != nil {
			m.recordGenericFailure(installation.ID, "恢复插件文件失败："+err.Error())
			continue
		}
		runtime, err = m.prepareRuntime(ctx, local, true)
		if err != nil {
			m.recordGenericFailure(installation.ID, "启动插件失败，将重试恢复："+err.Error())
			continue
		}
		latest, err := m.repo.GetByID(ctx, installation.ID)
		if err != nil || latest == nil || latest.State != PluginStateEnabled || !latest.Manifest.IsGeneric() || latest.BinarySHA256 != installation.BinarySHA256 || latest.ConfigEncrypted != installation.ConfigEncrypted {
			runtime.kill()
			if err != nil {
				m.recordGenericFailure(installation.ID, "插件状态无法确认，已停止本次恢复")
			}
			continue
		}
		m.mu.Lock()
		if m.runtimes == nil {
			m.runtimes = make(map[int64]*pluginRuntime)
		}
		m.runtimes[installation.ID] = runtime
		delete(m.genericStates, installation.ID)
		m.mu.Unlock()
	}
}

func (m *PluginManager) enableGenericLocked(ctx context.Context, installation *PluginInstallation, acceptUntested bool) (*PluginInstallation, error) {
	compatibility := EvaluatePluginCompatibility(installation.Manifest, m.hostInfo)
	if !compatibility.Compatible {
		err := m.repo.UpdateState(ctx, installation.ID, PluginStateIncompatible, compatibility.Message, nil, installation.BinarySHA256, installation.State)
		return nil, errors.Join(errors.New(compatibility.Message), err)
	}
	if !compatibility.Tested && !acceptUntested {
		return nil, errors.New("插件未声明已测试当前 Sub2API 版本，需要管理员确认后启用")
	}
	local, err := m.ensureLocalInstallation(ctx, installation)
	if err != nil {
		return nil, err
	}
	if !installation.Manifest.HasRuntime() {
		if installation.State != PluginStateEnabled {
			now := time.Now()
			if err := m.repo.UpdateState(ctx, installation.ID, PluginStateEnabled, "", &now, installation.BinarySHA256, installation.State); err != nil {
				return nil, err
			}
		}
		return m.Get(ctx, installation.ID)
	}
	m.mu.Lock()
	existing := m.runtimes[installation.ID]
	m.mu.Unlock()
	if installation.State == PluginStateEnabled && existing != nil && existing.alive() && existing.installation.BinarySHA256 == installation.BinarySHA256 && existing.installation.ConfigEncrypted == installation.ConfigEncrypted {
		return m.Get(ctx, installation.ID)
	}
	wasEnabled := installation.State == PluginStateEnabled
	if !wasEnabled {
		if err := m.repo.BeginEnable(ctx, installation.ID, installation.BinarySHA256, installation.State); err != nil {
			return nil, err
		}
	}
	runtime, err := m.prepareRuntime(ctx, local, true)
	if err != nil {
		m.recordGenericFailure(installation.ID, "启动插件失败："+err.Error())
		if !wasEnabled {
			stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			stateErr := m.repo.UpdateState(stateCtx, installation.ID, PluginStateError, pluginSafeErrorText(err.Error(), 500, ""), nil, installation.BinarySHA256, PluginStateStarting)
			cancel()
			err = errors.Join(err, stateErr)
		}
		return nil, err
	}
	if err := m.repo.MarkRuntimeHealthy(ctx, installation.ID, installation.BinarySHA256, installation.ConfigEncrypted); err != nil {
		runtime.kill()
		if !wasEnabled && !errors.Is(err, ErrPluginStateChanged) {
			stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			stateErr := m.repo.UpdateState(stateCtx, installation.ID, PluginStateError, "启用插件状态未能确认保存，请刷新后核对", nil, installation.BinarySHA256, PluginStateStarting)
			cancel()
			return nil, errors.Join(err, stateErr)
		}
		return nil, err
	}
	m.mu.Lock()
	old := m.runtimes[installation.ID]
	if m.runtimes == nil {
		m.runtimes = make(map[int64]*pluginRuntime)
	}
	m.runtimes[installation.ID] = runtime
	delete(m.genericStates, installation.ID)
	m.mu.Unlock()
	if old != nil && old != runtime {
		old.kill()
	}
	return m.Get(ctx, installation.ID)
}

func (m *PluginManager) scheduleBackgroundRuns(ctx context.Context) {
	m.mu.Lock()
	var candidates []*pluginRuntime
	for _, runtime := range m.runtimes {
		if runtime.backend != nil && runtime.access != nil && runtime.alive() && runtime.access.manifest.HasCapability(PluginCapabilityBackendBackground) && runtime.backgroundRunning.CompareAndSwap(false, true) {
			candidates = append(candidates, runtime)
		}
	}
	for _, runtime := range candidates {
		m.backgroundWG.Add(1)
		go func(runtime *pluginRuntime) {
			defer m.backgroundWG.Done()
			defer runtime.backgroundRunning.Store(false)
			m.runBackgroundOnce(ctx, runtime)
		}(runtime)
	}
	m.mu.Unlock()
}

func (m *PluginManager) runBackgroundOnce(ctx context.Context, runtime *pluginRuntime) {
	if runtime == nil || runtime.access == nil || runtime.backend == nil || !runtime.access.manifest.HasCapability(PluginCapabilityBackendBackground) {
		return
	}
	m.mu.Lock()
	leader := m.leader
	m.mu.Unlock()
	// No coordination backend is an explicit fail-closed condition. Do not use
	// the unrelated best-effort singleton helper, which permits fallback.
	if leader == nil || ctx.Err() != nil {
		return
	}
	id := runtime.access.id
	owner, err := pluginRandomToken()
	if err != nil {
		return
	}
	key := "sub2api:plugin-background:" + strconv.FormatInt(id, 10)
	lockCtx, cancelLock := context.WithTimeout(ctx, 2*time.Second)
	acquired, err := leader.TryAcquireLeaderLock(lockCtx, key, owner, 30*time.Second)
	cancelLock()
	if err != nil || !acquired {
		return
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = leader.ReleaseLeaderLock(releaseCtx, key, owner)
	}()
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	installation, err := m.enabledGeneric(runCtx, id, "")
	if err != nil || installation.BinarySHA256 != runtime.access.revision || !runtime.alive() || !runtime.beginRequest() {
		return
	}
	defer runtime.finishRequest()
	token, callCtx, revoke, err := runtime.access.issue(runCtx, PluginCaller{}, "write", false, true)
	if err != nil {
		return
	}
	defer revoke()
	deadline, _ := callCtx.Deadline()
	response, err := runtime.backend.RunOnce(callCtx, &pluginv1.RunOnceRequest{RunId: owner, DeadlineUnixMs: deadline.UnixMilli(), AccessToken: token})
	runEnded := runCtx.Err() != nil || callCtx.Err() != nil
	// Revoke before inspecting or releasing the leader lease, including when an
	// RPC implementation ignores cancellation and keeps a goroutine alive.
	revoke()
	if err != nil || runEnded {
		message := "插件后台周期调用失败，将重试恢复"
		if runEnded {
			message = "插件后台工作已超时或取消，进程已回收"
		}
		// Kill blocks until the process exits. Releasing before this point could
		// let another replica run concurrently with an unresponsive old process.
		m.failGenericRuntime(id, runtime, message)
		runtime.kill()
		return
	}
	if response == nil {
		m.failGenericRuntime(id, runtime, "插件后台工作未返回结果，进程已回收")
		return
	}
	if response.Error == nil {
		m.mu.Lock()
		if state := m.genericStates[id]; state.failures == 0 {
			delete(m.genericStates, id)
		}
		m.mu.Unlock()
	}
	if response.Error != nil {
		m.mu.Lock()
		if m.genericStates == nil {
			m.genericStates = make(map[int64]pluginGenericRuntimeState)
		}
		state := m.genericStates[id]
		state.message = fmt.Sprintf("插件后台工作报告失败：%s", pluginSafeErrorText(response.Error.Message, 500, token))
		m.genericStates[id] = state
		m.mu.Unlock()
	}
}
