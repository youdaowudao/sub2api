//go:build unit || integration

package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

func TestVideoRelayHTTP2RefusedStreamCannotReplayOneShotBody(t *testing.T) {
	for _, tc := range []struct {
		name       string
		replayable bool
		wantPOSTs  int32
	}{
		{name: "control can replay", replayable: true, wantPOSTs: 2},
		{name: "relay cannot replay", replayable: false, wantPOSTs: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				clientConn, serverConn := net.Pipe()
				defer clientConn.Close()
				defer serverConn.Close()
				var posts atomic.Int32
				peerDone := make(chan error, 1)
				go func() { peerDone <- serveVideoRelayRefusingPeer(serverConn, &posts) }()

				cfg := &config.Config{}
				cfg.Gateway.OpenAIHTTP2.Enabled = true
				client := NewHTTPUpstream(cfg).(*httpUpstreamService)
				entry, err := client.getClientEntry("", 12, 1, service.HTTPUpstreamProfileOpenAI, false, false)
				require.NoError(t, err)
				require.Equal(t, upstreamProtocolModeOpenAIH2, entry.protocolMode)
				tr := entry.client.Transport.(*http.Transport)
				// Keep the real pooled client and transport policy. Substitute only
				// an in-memory HTTP/2 connection for network dialing and TLS.
				tr.Protocols.SetHTTP1(false)
				tr.Protocols.SetUnencryptedHTTP2(true)
				tr.DialContext = func(context.Context, string, string) (net.Conn, error) {
					return clientConn, nil
				}
				defer tr.CloseIdleConnections()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ctx = service.WithHTTPUpstreamProfile(ctx, service.HTTPUpstreamProfileOpenAI)
				ctx = service.WithHTTPUpstreamRedirectsDisabled(ctx)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://fixture.test/v1/videos",
					strings.NewReader(`{"model":"fixture","prompt":"Waves","seconds":"5","resolution":"480p"}`))
				require.NoError(t, err)
				if !tc.replayable {
					req.GetBody = nil
				}
				resp, err := client.Do(req, "", 12, 1)
				if tc.replayable {
					require.NoError(t, err)
					require.Equal(t, 2, resp.ProtoMajor)
					require.NoError(t, resp.Body.Close())
				} else {
					require.ErrorContains(t, err, "REFUSED_STREAM")
					require.Nil(t, resp)
				}
				require.Equal(t, tc.wantPOSTs, posts.Load())
				require.NoError(t, clientConn.Close())
				require.NoError(t, <-peerDone)
			})
		})
	}
}

func serveVideoRelayRefusingPeer(conn net.Conn, posts *atomic.Int32) error {
	framer := http2.NewFramer(conn, conn)
	type event struct {
		frame http2.Frame
		err   error
	}
	frames := make(chan event, 8)
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		start := make([]byte, len(http2.ClientPreface))
		_, err := io.ReadFull(conn, start)
		if err == nil && string(start) != http2.ClientPreface {
			err = errors.New("missing HTTP/2 preface")
		}
		for {
			var frame http2.Frame
			if err == nil {
				frame, err = framer.ReadFrame()
			}
			select {
			case frames <- event{frame, err}:
			case <-stopped:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	if err := framer.WriteSettings(); err != nil {
		return err
	}
	for e := range frames {
		if e.err != nil {
			if errors.Is(e.err, io.EOF) || errors.Is(e.err, io.ErrClosedPipe) {
				return nil
			}
			return e.err
		}
		switch frame := e.frame.(type) {
		case *http2.SettingsFrame:
			if !frame.IsAck() {
				if err := framer.WriteSettingsAck(); err != nil {
					return err
				}
			}
		case *http2.DataFrame:
			if !frame.StreamEnded() {
				continue
			}
			if posts.Add(1) == 1 {
				if err := framer.WriteRSTStream(frame.StreamID, http2.ErrCodeRefusedStream); err != nil {
					return err
				}
				continue
			}
			var headers bytes.Buffer
			if err := hpack.NewEncoder(&headers).WriteField(hpack.HeaderField{Name: ":status", Value: "200"}); err != nil {
				return err
			}
			if err := framer.WriteHeaders(http2.HeadersFrameParam{
				StreamID: frame.StreamID, EndHeaders: true, EndStream: true, BlockFragment: headers.Bytes(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
