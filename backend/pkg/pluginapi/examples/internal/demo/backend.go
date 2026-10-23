// Package demo implements only example-owned behavior. It is compiled into
// independently installed packages and is never imported by the host.
package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

// The packager sets identity and declared capabilities on the example binary.
// The host discovers both from the signed manifest and verifies GetInfo.
var PluginID = "example.backend"
var PluginVersion = "1.0.0"
var CapabilitiesCSV = "backend.invoke.v1,backend.background.v1,storage.read.v1,storage.write.v1,users.created.read.v1"

type Settings struct {
	BackgroundDelayMS int  `json:"background_delay_ms"`
	IgnoreCancel      bool `json:"ignore_cancel"`
	CrashOnRun        bool `json:"crash_on_run"`
}

type Backend struct {
	pluginv1.UnimplementedBackendPluginServer
	mu         sync.Mutex
	broker     *hcplugin.GRPCBroker
	conn       *grpc.ClientConn
	host       pluginv1.HostServiceClient
	settings   Settings
	runCount   int64
	activeRuns int
}

func Serve() { pluginv1.ServeBackend(&Backend{}) }

func (b *Backend) SetHostBroker(broker *hcplugin.GRPCBroker) { b.broker = broker }
func (b *Backend) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{PluginId: PluginID, PluginVersion: PluginVersion, ProtocolVersion: 1, BackendApiVersion: 1, HostServiceApiVersion: 3, Capabilities: strings.Split(CapabilitiesCSV, ",")}, nil
}
func (b *Backend) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	b.mu.Lock()
	snapshot, err := json.Marshal(map[string]any{"run_count": b.runCount, "active_runs": b.activeRuns, "process_id": os.Getpid()})
	b.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &pluginv1.HealthResponse{Healthy: true, Message: "example backend ready", StatusJson: string(snapshot)}, nil
}
func parseSettings(raw []byte) (Settings, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return Settings{}, errors.New("configuration must be a JSON object")
	}
	var cfg Settings
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, errors.New("invalid example test configuration")
	}
	if cfg.BackgroundDelayMS < 0 || cfg.BackgroundDelayMS > 60000 {
		return cfg, errors.New("background_delay_ms must be between 0 and 60000")
	}
	return cfg, nil
}
func (*Backend) ValidateConfig(_ context.Context, req *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	_, err := parseSettings(req.ConfigJson)
	if err != nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: err.Error()}, nil
	}
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: req.ConfigJson}, nil
}
func (b *Backend) ApplyConfig(_ context.Context, req *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	settings, err := parseSettings(req.ConfigJson)
	if err != nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: err.Error()}, nil
	}
	b.mu.Lock()
	b.settings = settings
	b.mu.Unlock()
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}
func (*Backend) TestConfig(_ context.Context, req *pluginv1.TestConfigRequest) (*pluginv1.TestConfigResponse, error) {
	_, err := parseSettings(req.ConfigJson)
	if err != nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: err.Error()}, nil
	}
	return &pluginv1.TestConfigResponse{Success: true, Message: "configuration is valid; no background work was started"}, nil
}
func (b *Backend) InitHostServices(_ context.Context, req *pluginv1.InitHostServicesRequest) (*pluginv1.InitHostServicesResponse, error) {
	if req.HostServiceApiVersion != 3 || b.broker == nil {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "Host API 3 required"}, nil
	}
	conn, err := pluginv1.DialGenericHostServices(b.broker, req.HostServiceId)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.conn, b.host = conn, pluginv1.NewHostServiceClient(conn)
	b.mu.Unlock()
	return &pluginv1.InitHostServicesResponse{Ready: true}, nil
}
func (b *Backend) hostClient() pluginv1.HostServiceClient {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.host
}
func result(value any) (*pluginv1.InvokeResponse, error) {
	raw, err := json.Marshal(value)
	return &pluginv1.InvokeResponse{PayloadJson: raw}, err
}
func (b *Backend) Invoke(ctx context.Context, req *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error) {
	host := b.hostClient()
	if host == nil || req.Caller == nil {
		return nil, errors.New("host invocation is not ready")
	}
	switch req.Method {
	case "echo":
		var input any
		if err := json.Unmarshal(req.PayloadJson, &input); err != nil {
			return nil, err
		}
		return result(map[string]any{"caller": map[string]any{"user_id": req.Caller.UserId, "role": req.Caller.Role}, "input": input})
	case "read_note":
		value, err := host.KVGet(ctx, &pluginv1.KVGetRequest{AccessToken: req.AccessToken, Namespace: "notes", Key: "value"})
		if err != nil {
			return nil, err
		}
		return result(map[string]any{"found": value.Found, "value": string(value.Value)})
	case "write_note":
		var input struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(req.PayloadJson, &input); err != nil || len(input.Value) > 1024 {
			return nil, errors.New("value must be a string of at most 1024 bytes")
		}
		_, err := host.KVSet(ctx, &pluginv1.KVSetRequest{AccessToken: req.AccessToken, Namespace: "notes", Key: "value", Value: []byte(input.Value)})
		if err != nil {
			return nil, err
		}
		return result(map[string]bool{"saved": true})
	case "event_list":
		keys, err := host.KVList(ctx, &pluginv1.KVListRequest{AccessToken: req.AccessToken, Namespace: "events", KeyPrefix: "event.", Limit: 100})
		if err != nil {
			return nil, err
		}
		items := make([]json.RawMessage, 0, len(keys.Keys))
		for _, key := range keys.Keys {
			value, err := host.KVGet(ctx, &pluginv1.KVGetRequest{AccessToken: req.AccessToken, Namespace: "events", Key: key})
			if err != nil {
				return nil, err
			}
			if value.Found && json.Valid(value.Value) {
				items = append(items, value.Value)
			}
		}
		return result(map[string]any{"items": items, "stored_keys": len(items)})
	case "host_probe":
		_, writeErr := host.KVSet(ctx, &pluginv1.KVSetRequest{AccessToken: req.AccessToken, Namespace: "notes", Key: "forbidden", Value: []byte("read calls must not write")})
		_, observeErr := host.ListUserCreations(ctx, &pluginv1.ListUserCreationsRequest{AccessToken: req.AccessToken, Limit: 1})
		return result(map[string]bool{"write_denied": writeErr != nil, "observation_denied": observeErr != nil})
	case "wait":
		var input struct {
			Milliseconds int `json:"milliseconds"`
		}
		if err := json.Unmarshal(req.PayloadJson, &input); err != nil || input.Milliseconds < 0 || input.Milliseconds > 60000 {
			return nil, errors.New("milliseconds must be between 0 and 60000")
		}
		select {
		case <-time.After(time.Duration(input.Milliseconds) * time.Millisecond):
			return result(map[string]bool{"finished": true})
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case "crash":
		os.Exit(7) // Deliberate, administrator-only local acceptance fixture.
	}
	return nil, fmt.Errorf("unknown example method %q", req.Method)
}

func (b *Backend) RunOnce(ctx context.Context, req *pluginv1.RunOnceRequest) (*pluginv1.RunOnceResponse, error) {
	b.mu.Lock()
	settings := b.settings
	b.runCount++
	b.activeRuns++
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.activeRuns--; b.mu.Unlock() }()
	if settings.CrashOnRun {
		os.Exit(8)
	}
	if settings.BackgroundDelayMS > 0 {
		if settings.IgnoreCancel {
			time.Sleep(time.Duration(settings.BackgroundDelayMS) * time.Millisecond)
		} else {
			select {
			case <-time.After(time.Duration(settings.BackgroundDelayMS) * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	host := b.hostClient()
	if host == nil {
		return nil, errors.New("host not ready")
	}
	// The only state is plugin KV. Every event overwrites its own stable key;
	// revisiting an event cannot increment a counter or duplicate a record.
	var cursor struct{ After, Through int64 }
	state, err := host.KVGet(ctx, &pluginv1.KVGetRequest{AccessToken: req.AccessToken, Namespace: "events", Key: "cursor"})
	if err != nil {
		return nil, err
	}
	if state.Found {
		_ = json.Unmarshal(state.Value, &cursor)
	}
	page, err := host.ListUserCreations(ctx, &pluginv1.ListUserCreationsRequest{AccessToken: req.AccessToken, AfterUserId: cursor.After, ThroughUserId: cursor.Through, Limit: 25})
	if err != nil {
		return nil, err
	}
	for _, item := range page.Items {
		raw, err := json.Marshal(map[string]any{"event_id": item.EventId, "user_id": item.UserId, "created_at": item.CreatedAt, "completed_at": item.CompletedAt, "signup_source": item.SignupSource, "email": item.Email})
		if err != nil {
			return nil, err
		}
		if _, err := host.KVSet(ctx, &pluginv1.KVSetRequest{AccessToken: req.AccessToken, Namespace: "events", Key: "event." + strconv.FormatInt(item.UserId, 10), Value: raw}); err != nil {
			return nil, err
		}
	}
	cursor.After, cursor.Through = page.NextUserId, page.ThroughUserId
	if page.Done {
		cursor.After, cursor.Through = 0, 0
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return nil, err
	}
	if _, err := host.KVSet(ctx, &pluginv1.KVSetRequest{AccessToken: req.AccessToken, Namespace: "events", Key: "cursor", Value: raw}); err != nil {
		return nil, err
	}
	return &pluginv1.RunOnceResponse{}, nil
}
