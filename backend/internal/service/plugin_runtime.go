package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type pluginLifecycleClient interface {
	GetInfo(context.Context, *pluginv1.GetInfoRequest, ...grpc.CallOption) (*pluginv1.GetInfoResponse, error)
	Health(context.Context, *pluginv1.HealthRequest, ...grpc.CallOption) (*pluginv1.HealthResponse, error)
	ValidateConfig(context.Context, *pluginv1.ValidateConfigRequest, ...grpc.CallOption) (*pluginv1.ValidateConfigResponse, error)
	ApplyConfig(context.Context, *pluginv1.ApplyConfigRequest, ...grpc.CallOption) (*pluginv1.ApplyConfigResponse, error)
	TestConfig(context.Context, *pluginv1.TestConfigRequest, ...grpc.CallOption) (*pluginv1.TestConfigResponse, error)
	InitHostServices(context.Context, *pluginv1.InitHostServicesRequest, ...grpc.CallOption) (*pluginv1.InitHostServicesResponse, error)
}

type pluginRuntime struct {
	installation      *PluginInstallation
	client            *hcplugin.Client
	api               pluginv1.TransportPluginClient
	backend           pluginv1.BackendPluginClient
	access            *pluginRuntimeAccess
	backgroundRunning atomic.Bool
	inFlight          atomic.Int64
	draining          atomic.Bool
	done              chan struct{}
	doneOnce          sync.Once
}

func startPluginRuntime(ctx context.Context, installation *PluginInstallation, startTimeout time.Duration, socketDir string, hostServices pluginv1.HostServiceServer) (*pluginRuntime, error) {
	if installation == nil {
		return nil, errors.New("插件安装记录为空")
	}
	binaryHash := installation.BinarySHA256
	if installation.Manifest.IsGeneric() {
		entry, ok := installation.Manifest.Runtimes[installation.Manifest.RuntimeKey()]
		if !ok || entry.Path == "" {
			return nil, errors.New("纯前端插件没有后台运行时")
		}
		binaryHash = installation.Manifest.Files[entry.Path]
	}
	checksum, err := hex.DecodeString(binaryHash)
	if err != nil || len(checksum) != sha256.Size {
		return nil, errors.New("插件二进制哈希无效")
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), installation.BinaryPath) //nolint:gosec // G702: 路径来自已验证清单的安装记录，SecureConfig 在启动前核对 SHA256；不经 shell、不拼接请求参数。
	pluginMap := pluginv1.ClientPluginMap()
	var dialOptions []grpc.DialOption
	if installation.Manifest.IsGeneric() {
		configureGenericPluginProcess(cmd)
		pluginMap = pluginv1.BackendClientPluginMap()
		dialOptions = []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(pluginv1.GenericMaxMessageBytes), grpc.MaxCallSendMsgSize(pluginv1.GenericMaxMessageBytes))}
	}
	client := hcplugin.NewClient(&hcplugin.ClientConfig{
		HandshakeConfig:  pluginv1.HandshakeConfig,
		Plugins:          pluginMap,
		GRPCDialOptions:  dialOptions,
		Cmd:              cmd,
		AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolGRPC},
		StartTimeout:     startTimeout,
		SecureConfig: &hcplugin.SecureConfig{
			Checksum: checksum,
			Hash:     sha256.New(),
		},
		Logger:           hclog.NewNullLogger(),
		SyncStdout:       io.Discard,
		SyncStderr:       io.Discard,
		UnixSocketConfig: &hcplugin.UnixSocketConfig{TempDir: socketDir},
		SkipHostEnv:      true,
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("启动插件进程: %w", err)
	}
	name := pluginv1.TransportPluginName
	if installation.Manifest.IsGeneric() {
		name = pluginv1.BackendPluginName
	}
	dispensed, err := rpcClient.Dispense(name)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("获取插件能力 %s: %w", name, err)
	}
	runtime := &pluginRuntime{installation: installation, client: client, done: make(chan struct{})}
	if server, ok := hostServices.(*pluginHostServiceServer); ok {
		runtime.access = server.access
	}
	var broker *hcplugin.GRPCBroker
	if installation.Manifest.IsGeneric() {
		backend, ok := dispensed.(*pluginv1.BackendClient)
		if !ok || backend.BackendPluginClient == nil {
			runtime.kill()
			return nil, errors.New("插件未实现 Backend gRPC 客户端")
		}
		runtime.backend, broker = backend.BackendPluginClient, backend.Broker
	} else {
		transport, ok := dispensed.(*pluginv1.TransportClient)
		if !ok || transport.TransportPluginClient == nil {
			runtime.kill()
			return nil, errors.New("插件未实现传输 gRPC 客户端")
		}
		runtime.api, broker = transport.TransportPluginClient, transport.Broker
	}
	api := runtime.lifecycle()
	infoCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	info, err := api.GetInfo(infoCtx, &pluginv1.GetInfoRequest{})
	if err != nil {
		runtime.kill()
		return nil, fmt.Errorf("读取插件信息: %w", err)
	}
	if info == nil || info.PluginId != installation.PluginKey || info.PluginVersion != installation.Version || info.ProtocolVersion != pluginv1.ProtocolVersion {
		runtime.kill()
		return nil, errors.New("插件运行时信息与已校验清单不一致")
	}
	if installation.Manifest.IsGeneric() {
		if info.TransportApiVersion != 0 || info.BackendApiVersion != pluginv1.BackendAPIVersion || info.HostServiceApiVersion != pluginv1.GenericHostServiceAPIVersion || !pluginRuntimeCapabilitiesMatch(info.Capabilities, installation.Manifest) {
			runtime.kill()
			return nil, errors.New("插件后台协议或能力与已校验清单不一致")
		}
	} else if info.TransportApiVersion != pluginv1.TransportAPIVersion {
		runtime.kill()
		return nil, errors.New("插件运行时信息与已校验清单不一致")
	}
	health, err := api.Health(infoCtx, &pluginv1.HealthRequest{})
	if err != nil || health == nil || !health.Healthy {
		runtime.kill()
		if err != nil {
			return nil, fmt.Errorf("插件健康检查失败: %w", err)
		}
		if health == nil {
			return nil, errors.New("插件未返回健康结果")
		}
		return nil, fmt.Errorf("插件不健康: %s", health.Message)
	}
	// 可选地把宿主服务（HostService）反向暴露给插件。这是叠加在传输契约之上的能力：
	// 老插件不实现 InitHostServices（返回 Unimplemented），此处静默跳过，绝不阻断启动。
	if installation.Manifest.IsGeneric() {
		if err := offerBackendHostServices(ctx, runtime.backend, broker, hostServices, startTimeout); err != nil {
			runtime.kill()
			return nil, err
		}
	} else {
		offerPluginHostServices(ctx, installation, runtime.api, broker, hostServices, startTimeout)
	}
	return runtime, nil
}

// offerPluginHostServices 在 go-plugin broker 上启动一个宿主服务实例，并通过
// InitHostServices 把 broker 流 id 交给插件。服务生命周期与插件进程绑定：client.Kill()
// 会关闭 broker，AcceptAndServe 随之 GracefulStop，无需手动清理。整个过程尽力而为，
// 任何失败都只记录日志、不影响插件转发能力。
func offerPluginHostServices(
	ctx context.Context,
	installation *PluginInstallation,
	api pluginv1.TransportPluginClient,
	broker *hcplugin.GRPCBroker,
	hostServices pluginv1.HostServiceServer,
	startTimeout time.Duration,
) {
	if broker == nil || hostServices == nil || api == nil {
		return
	}
	brokerID := broker.NextId()
	go broker.AcceptAndServe(brokerID, func(opts []grpc.ServerOption) *grpc.Server {
		server := grpc.NewServer(opts...)
		pluginv1.RegisterHostServiceServer(server, hostServices)
		return server
	})
	initCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	resp, err := api.InitHostServices(initCtx, &pluginv1.InitHostServicesRequest{
		HostServiceId:         brokerID,
		HostServiceApiVersion: pluginv1.HostServiceAPIVersion,
	})
	pluginKey := ""
	if installation != nil {
		pluginKey = installation.PluginKey
	}
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			slog.Debug("plugin_host_services_unimplemented", "plugin", pluginKey)
		} else {
			slog.Warn("plugin_host_services_init_failed", "plugin", pluginKey, "error", err)
		}
		return
	}
	if resp != nil && !resp.Ready {
		slog.Debug("plugin_host_services_declined", "plugin", pluginKey, "message", resp.Message)
	}
}

func (r *pluginRuntime) validateAndApplyConfig(ctx context.Context, configJSON []byte) error {
	_, err := r.validateAndApplyNormalizedConfig(ctx, configJSON)
	return err
}

func (r *pluginRuntime) validateAndApplyNormalizedConfig(ctx context.Context, configJSON []byte) (result []byte, resultErr error) {
	defer func() {
		if resultErr != nil && r.installation != nil && r.installation.Manifest.IsGeneric() {
			resultErr = errors.New(pluginSafeErrorText(resultErr.Error(), 500, ""))
		}
	}()
	validation, err := r.lifecycle().ValidateConfig(ctx, &pluginv1.ValidateConfigRequest{ConfigJson: configJSON})
	if err != nil {
		return nil, fmt.Errorf("插件配置校验失败: %w", err)
	}
	if validation == nil {
		return nil, errors.New("插件配置校验没有返回结果")
	}
	if !validation.Valid {
		return nil, fmt.Errorf("插件配置无效: %s", validation.Message)
	}
	if len(validation.NormalizedConfigJson) > 0 {
		configJSON = validation.NormalizedConfigJson
	}
	if len(configJSON) == 0 || len(configJSON) > pluginConfigMaxBytes || !json.Valid(configJSON) {
		return nil, errors.New("插件返回的规范化配置不是有效且大小受限的 JSON")
	}
	var normalized any
	if err := json.Unmarshal(configJSON, &normalized); err != nil {
		return nil, fmt.Errorf("解析插件规范化配置: %w", err)
	}
	if normalized == nil {
		return nil, errors.New("插件返回的规范化配置根节点必须是对象")
	}
	if _, ok := normalized.(map[string]any); !ok {
		return nil, errors.New("插件返回的规范化配置根节点必须是对象")
	}
	configJSON, err = json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("序列化插件规范化配置: %w", err)
	}
	applied, err := r.lifecycle().ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: configJSON})
	if err != nil {
		return nil, fmt.Errorf("应用插件配置失败: %w", err)
	}
	if applied == nil {
		return nil, errors.New("应用插件配置没有返回结果")
	}
	if !applied.Applied {
		return nil, fmt.Errorf("插件拒绝应用配置: %s", applied.Message)
	}
	return configJSON, nil
}

func (r *pluginRuntime) checkHealth(ctx context.Context) error {
	if r == nil || r.lifecycle() == nil || r.client == nil || r.client.Exited() {
		return errors.New("插件进程已退出")
	}
	health, err := r.lifecycle().Health(ctx, &pluginv1.HealthRequest{})
	if err != nil {
		return fmt.Errorf("插件健康检查失败: %w", err)
	}
	if health == nil || !health.Healthy {
		message := "插件报告不健康"
		if health != nil && strings.TrimSpace(health.Message) != "" {
			message = "插件不健康: " + health.Message
		}
		return errors.New(message)
	}
	return nil
}

// status returns the plugin's passive Health response, including any status_json
// blob it exposes for the config UI. It performs no config apply and no upstream
// call, so it is safe to serve from a lightweight, ungated status endpoint.
func (r *pluginRuntime) status(ctx context.Context) (*pluginv1.HealthResponse, error) {
	if r == nil || r.lifecycle() == nil || r.client == nil || r.client.Exited() {
		return nil, errors.New("插件进程已退出")
	}
	health, err := r.lifecycle().Health(ctx, &pluginv1.HealthRequest{})
	if err != nil {
		return nil, fmt.Errorf("插件状态查询失败: %w", err)
	}
	if health == nil {
		return nil, errors.New("插件未返回状态")
	}
	return health, nil
}

func (r *pluginRuntime) beginRequest() bool {
	if r == nil || r.draining.Load() {
		return false
	}
	r.inFlight.Add(1)
	if r.draining.Load() {
		r.finishRequest()
		return false
	}
	return true
}

func (r *pluginRuntime) finishRequest() {
	if r.inFlight.Add(-1) == 0 && r.draining.Load() {
		r.doneOnce.Do(func() { close(r.done) })
	}
}

func (r *pluginRuntime) drain(timeout time.Duration) {
	if r == nil {
		return
	}
	if r.access != nil {
		r.kill()
		return
	}
	r.draining.Store(true)
	if r.inFlight.Load() == 0 {
		r.doneOnce.Do(func() { close(r.done) })
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.done:
	case <-timer.C:
	}
	r.kill()
}

func (r *pluginRuntime) kill() {
	if r == nil {
		return
	}
	r.draining.Store(true)
	if r.access != nil {
		r.access.close()
	}
	if r.client != nil {
		r.client.Kill()
	}
}

func (r *pluginRuntime) alive() bool {
	return r != nil && r.client != nil && !r.client.Exited() && !r.draining.Load()
}

func (r *pluginRuntime) roundTrip(ctx context.Context, request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if request == nil || request.URL == nil || account == nil {
		return nil, errors.New("插件出站请求参数不完整")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := r.api.Forward(streamCtx)
	if err != nil {
		cancel()
		return nil, normalizePluginRPCError(ctx, "创建插件转发流", err, false)
	}
	requestID := strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(account.ID, 36)
	if err := stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_Start{Start: &pluginv1.ForwardRequestStart{
		RequestId:          requestID,
		Method:             request.Method,
		Url:                request.URL.String(),
		Host:               request.Host,
		Headers:            headersToPlugin(request.Header),
		ProxyUrl:           proxyURL,
		AccountId:          account.ID,
		AccountConcurrency: int32(account.Concurrency),
		Platform:           account.Platform,
		AccountType:        account.Type,
		ContentLength:      request.ContentLength,
		HasBody:            request.Body != nil && request.Body != http.NoBody,
	}}}); err != nil {
		cancel()
		// gRPC Send 返回错误时无法证明服务端没有收到元数据，必须禁止自动重放。
		return nil, normalizePluginRPCError(ctx, "发送插件请求元数据", err, true)
	}
	sendErr := make(chan error, 1)
	go func() {
		err := sendPluginRequestBody(stream, request.Body)
		sendErr <- err
		if err != nil {
			cancel()
		}
	}()

	first, err := stream.Recv()
	if err != nil {
		cancel()
		select {
		case bodyErr := <-sendErr:
			if bodyErr != nil {
				return nil, normalizePluginRPCError(ctx, "发送插件请求体", bodyErr, true)
			}
		default:
		}
		return nil, normalizePluginRPCError(ctx, "接收插件响应头", err, true)
	}
	if frameError := first.GetError(); frameError != nil {
		cancel()
		return nil, &PluginTransportError{Code: frameError.Code, Message: frameError.Message, RequestSent: frameError.RequestSent}
	}
	start := first.GetStart()
	if start == nil || start.StatusCode < 100 || start.StatusCode > 599 {
		cancel()
		return nil, &PluginTransportError{
			Code:        "PLUGIN_INVALID_RESPONSE",
			Message:     "插件未返回有效的 HTTP 响应头",
			RequestSent: true,
		}
	}
	pipeReader, pipeWriter := io.Pipe()
	body := &pluginResponseBody{
		reader: pipeReader,
		cancel: cancel,
		done:   r.finishRequest,
	}
	go receivePluginResponseBody(stream, pipeWriter, sendErr)
	return &http.Response{
		Status:        start.Status,
		StatusCode:    int(start.StatusCode),
		Proto:         start.Protocol,
		ProtoMajor:    int(start.ProtocolMajor),
		ProtoMinor:    int(start.ProtocolMinor),
		Header:        headersFromPlugin(start.Headers),
		Body:          body,
		ContentLength: start.ContentLength,
		Request:       request,
	}, nil
}

type PluginTransportError struct {
	Code        string
	Message     string
	RequestSent bool
}

func (e *PluginTransportError) Error() string {
	if e == nil {
		return "插件传输失败"
	}
	code := strings.Map(func(value rune) rune {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-' || value == '.' {
			return value
		}
		return -1
	}, e.Code)
	if len(code) > 64 {
		code = code[:64]
	}
	return fmt.Sprintf("插件传输失败 [%s]: %s", code, sanitizeUpstreamErrorMessage(e.Message))
}

func normalizePluginRPCError(ctx context.Context, operation string, err error, requestMayHaveBeenSent bool) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return &PluginTransportError{
		Code:        "PLUGIN_RPC_ERROR",
		Message:     fmt.Sprintf("%s: %v", operation, err),
		RequestSent: requestMayHaveBeenSent,
	}
}

func sendPluginRequestBody(stream pluginv1.TransportPlugin_ForwardClient, body io.ReadCloser) error {
	if body != nil {
		defer func() { _ = body.Close() }()
		buffer := make([]byte, 32*1024)
		for {
			read, err := body.Read(buffer)
			if read > 0 {
				chunk := append([]byte(nil), buffer[:read]...)
				if sendErr := stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyChunk{BodyChunk: chunk}}); sendErr != nil {
					return sendErr
				}
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
	}
	if err := stream.Send(&pluginv1.ForwardRequest{Frame: &pluginv1.ForwardRequest_BodyEnd{BodyEnd: true}}); err != nil {
		return err
	}
	return stream.CloseSend()
}

func receivePluginResponseBody(stream pluginv1.TransportPlugin_ForwardClient, writer *io.PipeWriter, sendErr <-chan error) {
	defer func() { _ = writer.Close() }()
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			_ = writer.CloseWithError(normalizePluginRPCError(stream.Context(), "接收插件响应体", err, true))
			return
		}
		if chunk := frame.GetBodyChunk(); len(chunk) > 0 {
			if _, err := writer.Write(chunk); err != nil {
				return
			}
			continue
		}
		if frame.GetEnd() != nil {
			select {
			case err := <-sendErr:
				if err != nil {
					_ = writer.CloseWithError(normalizePluginRPCError(stream.Context(), "发送插件请求体", err, true))
				}
			default:
			}
			return
		}
		if frameError := frame.GetError(); frameError != nil {
			_ = writer.CloseWithError(&PluginTransportError{Code: frameError.Code, Message: frameError.Message, RequestSent: frameError.RequestSent})
			return
		}
	}
}

type pluginResponseBody struct {
	reader *io.PipeReader
	cancel context.CancelFunc
	done   func()
	once   sync.Once
}

func (b *pluginResponseBody) Read(data []byte) (int, error) {
	return b.reader.Read(data)
}

func (b *pluginResponseBody) Close() error {
	var err error
	b.once.Do(func() {
		b.cancel()
		err = b.reader.Close()
		b.done()
	})
	return err
}

func headersToPlugin(headers http.Header) map[string]*pluginv1.HeaderValues {
	out := make(map[string]*pluginv1.HeaderValues, len(headers))
	for key, values := range headers {
		out[key] = &pluginv1.HeaderValues{Values: append([]string(nil), values...)}
	}
	return out
}

func headersFromPlugin(headers map[string]*pluginv1.HeaderValues) http.Header {
	out := make(http.Header, len(headers))
	for key, values := range headers {
		if values != nil {
			out[key] = append([]string(nil), values.Values...)
		}
	}
	return out
}

func (r *pluginRuntime) lifecycle() pluginLifecycleClient {
	if r == nil {
		return nil
	}
	if r.backend != nil {
		return r.backend
	}
	return r.api
}

func pluginRuntimeCapabilitiesMatch(actual []string, manifest PluginManifest) bool {
	if len(actual) != len(manifest.Capabilities) {
		return false
	}
	seen := make(map[string]bool, len(actual))
	for _, id := range actual {
		if seen[id] || !manifest.HasCapability(id) {
			return false
		}
		seen[id] = true
	}
	return true
}

func offerBackendHostServices(ctx context.Context, api pluginv1.BackendPluginClient, broker *hcplugin.GRPCBroker, services pluginv1.HostServiceServer, timeout time.Duration) error {
	if api == nil || broker == nil || services == nil {
		return errors.New("通用后台宿主服务未配置，无法启动插件")
	}
	id := broker.NextId()
	go broker.AcceptAndServe(id, func(opts []grpc.ServerOption) *grpc.Server {
		opts = append(opts, grpc.MaxRecvMsgSize(pluginv1.GenericMaxMessageBytes), grpc.MaxSendMsgSize(pluginv1.GenericMaxMessageBytes))
		server := grpc.NewServer(opts...)
		pluginv1.RegisterHostServiceServer(server, services)
		return server
	})
	initCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := api.InitHostServices(initCtx, &pluginv1.InitHostServicesRequest{HostServiceId: id, HostServiceApiVersion: pluginv1.GenericHostServiceAPIVersion})
	if err != nil {
		return fmt.Errorf("连接后台插件宿主服务失败: %w", err)
	}
	if response == nil || !response.Ready {
		return errors.New("后台插件未确认宿主服务已就绪")
	}
	return nil
}
