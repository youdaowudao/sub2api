// This fixture proves the published v1 Transport contract still works. It is
// deliberately separate from both generic Backend examples.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
)

var pluginVersion = "1.0.0"

type transport struct {
	pluginv1.UnimplementedTransportPluginServer
	mu      sync.Mutex
	headers map[string]string
}

func main() { pluginv1.Serve(&transport{}) }
func (*transport) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{PluginId: "example.legacy", PluginVersion: pluginVersion, ProtocolVersion: 1, TransportApiVersion: 1, Capabilities: []string{"openai.oauth.outbound_transport.v1"}}, nil
}
func (*transport) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	return &pluginv1.HealthResponse{Healthy: true}, nil
}
func (*transport) ValidateConfig(_ context.Context, req *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	var cfg map[string]any
	err := json.Unmarshal(req.ConfigJson, &cfg)
	return &pluginv1.ValidateConfigResponse{Valid: err == nil && cfg != nil, NormalizedConfigJson: req.ConfigJson}, nil
}
func (t *transport) ApplyConfig(_ context.Context, req *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	var cfg struct {
		ExtraHeaders map[string]string `json:"extra_headers"`
	}
	if err := json.Unmarshal(req.ConfigJson, &cfg); err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.headers = cfg.ExtraHeaders
	t.mu.Unlock()
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}
func (*transport) TestConfig(context.Context, *pluginv1.TestConfigRequest) (*pluginv1.TestConfigResponse, error) {
	return &pluginv1.TestConfigResponse{Success: true, Message: "v1 fixture configuration ready"}, nil
}
func (t *transport) Forward(stream pluginv1.TransportPlugin_ForwardServer) error {
	first, err := stream.Recv()
	if err != nil || first.GetStart() == nil {
		return errors.New("missing request start")
	}
	start := first.GetStart()
	var body bytes.Buffer
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if frame.GetBodyEnd() {
			break
		}
		if body.Len()+len(frame.GetBodyChunk()) > 32*1024*1024 {
			return errors.New("example request limit exceeded")
		}
		if _, err := body.Write(frame.GetBodyChunk()); err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(stream.Context(), start.Method, start.Url, &body)
	if err != nil {
		return err
	}
	for key, values := range start.Headers {
		for _, value := range values.Values {
			request.Header.Add(key, value)
		}
	}
	t.mu.Lock()
	for key, value := range t.headers {
		request.Header.Set(key, value)
	}
	t.mu.Unlock()
	started := time.Now()
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "EXAMPLE_UPSTREAM_FAILED", Message: err.Error(), RequestSent: true}}})
	}
	defer func() { _ = response.Body.Close() }()
	headers := map[string]*pluginv1.HeaderValues{}
	for key, values := range response.Header {
		headers[key] = &pluginv1.HeaderValues{Values: values}
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: int32(response.StatusCode), Protocol: response.Proto, ProtocolMajor: int32(response.ProtoMajor), ProtocolMinor: int32(response.ProtoMinor), Headers: headers, ContentLength: response.ContentLength}}}); err != nil {
		return err
	}
	var total int64
	buffer := make([]byte, 32*1024)
	for {
		n, err := response.Body.Read(buffer)
		if n > 0 {
			total += int64(n)
			if sendErr := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: append([]byte(nil), buffer[:n]...)}}); sendErr != nil {
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
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{BytesReceived: total, DurationMs: time.Since(started).Milliseconds()}}})
}
