//go:build integration

package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

type videoRelayFixtureTransport func(*http.Request) (*http.Response, error)

func (send videoRelayFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return send(request)
}

func installTrackingDNSFixture(t *testing.T, delay time.Duration, queries *atomic.Int32) {
	t.Helper()
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	originalResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", socket.LocalAddr().String())
		},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var buf [1024]byte
		for {
			size, addr, err := socket.ReadFrom(buf[:])
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buf[:size]) != nil {
				continue
			}
			queries.Add(1)
			reply := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			for _, question := range query.Questions {
				if question.Type == dnsmessage.TypeA {
					reply.Answers = append(reply.Answers, dnsmessage.Resource{
						Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 5},
						Body:   &dnsmessage.AResource{A: [4]byte{203, 0, 113, 12}},
					})
				}
			}
			packet, err := reply.Pack()
			if err != nil {
				continue
			}
			time.AfterFunc(delay, func() { _, _ = socket.WriteTo(packet, addr) })
		}
	}()
	t.Cleanup(func() {
		net.DefaultResolver = originalResolver
		require.NoError(t, socket.Close())
		<-done
	})
}

func TestVideoRelayRepoRefundDeadlineDuringDNS(t *testing.T) {
	for _, test := range []struct {
		name  string
		host  string
		delay time.Duration
		want  int32
	}{
		{"early DNS hands off a single request", "early-video-tracking-test.invalid", 0, 1},
		{"late DNS performs no HTTP handoff", "late-video-tracking-test.invalid", 2 * time.Second, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var resolved, handedOff, received atomic.Int32
			installTrackingDNSFixture(t, test.delay, &resolved)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/log/token" {
					http.NotFound(w, r)
					return
				}
				received.Add(1)
				_, _ = io.WriteString(w, trackingFixtureChargeLog)
			}))
			t.Cleanup(server.Close)
			serverURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			transportConfig := &config.Config{}
			transportConfig.Security.URLAllowlist.Enabled = true
			realTransport := NewHTTPUpstream(transportConfig).(*httpUpstreamService)
			task, repo, tracker := newTrackingVideoRelayFixture(t, "http://"+test.host,
				6*time.Hour, trackingVideoRelayFixtureOptions{upstream: realTransport})
			entry, err := realTransport.getClientEntry("", task.AccountID, 3, service.HTTPUpstreamProfileOpenAI, false, true)
			require.NoError(t, err)
			entry.client.Transport = videoRelayFixtureTransport(func(req *http.Request) (*http.Response, error) {
				handedOff.Add(1)
				request := req.Clone(req.Context())
				request.URL.Scheme, request.URL.Host = serverURL.Scheme, serverURL.Host
				return server.Client().Transport.RoundTrip(request)
			})
			prepareVideoRelayRefundLookup(t, repo, task, 120*time.Minute-1200*time.Millisecond)
			ctx := context.Background()
			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			tracker.ProcessClaim(ctx, claim)
			require.Positive(t, resolved.Load(), "both controls must pass through the production DNS validator")
			require.Equal(t, test.want, handedOff.Load())
			require.Equal(t, test.want, received.Load())
			current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, service.VideoRelayRefunded, current.BillingState)
			if test.want == 0 {
				require.Empty(t, current.UpstreamTaskID)
			} else {
				require.Equal(t, "tracking-fixture-video", current.UpstreamTaskID)
			}
			require.False(t, current.CanDownload())
			assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
		})
	}
}

func TestVideoRelayRepoRefundDeadlineCancelsRealHTTPBody(t *testing.T) {
	var handedOff, received atomic.Int32
	disconnected := make(chan time.Time, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/log/token" {
			http.NotFound(w, req)
			return
		}
		received.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-req.Context().Done()
		disconnected <- time.Now()
	}))
	t.Cleanup(server.Close)
	upstream := NewHTTPUpstream(&config.Config{}).(*httpUpstreamService)
	task, repo, tracker := newTrackingVideoRelayFixture(t, server.URL,
		6*time.Hour, trackingVideoRelayFixtureOptions{upstream: upstream})
	entry, err := upstream.getClientEntry("", task.AccountID, 3, service.HTTPUpstreamProfileOpenAI, false, true)
	require.NoError(t, err)
	transport := entry.client.Transport
	entry.client.Transport = videoRelayFixtureTransport(func(req *http.Request) (*http.Response, error) {
		handedOff.Add(1)
		return transport.RoundTrip(req)
	})
	prepareVideoRelayRefundLookup(t, repo, task, 120*time.Minute-1200*time.Millisecond)
	ctx := context.Background()
	claim, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, task.TaskID, claim.TaskID)
	cutoff := claim.SettledAt.Add(service.VideoRelayRefundTrackingWindow)
	tracker.ProcessClaim(ctx, claim)
	select {
	case at := <-disconnected:
		require.WithinDuration(t, cutoff, at, 2*time.Second,
			"the real HTTP body must be canceled at the persisted two-hour cutoff, before its normal upstream timeout")
		t.Logf("HTTP body disconnected %s relative to the refund cutoff", at.Sub(cutoff))
	case <-time.After(3 * time.Second):
		t.Fatal("the real HTTP connection was not canceled while reading the billing log body")
	}
	require.EqualValues(t, 1, handedOff.Load(), "one application handoff before the cutoff")
	require.EqualValues(t, 1, received.Load(), "the real HTTP listener must receive just that request")
	current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Empty(t, current.UpstreamTaskID)
	require.Equal(t, service.VideoRelayRefunded, current.BillingState)
	require.False(t, current.CanDownload())
	require.Nil(t, current.NextCheckAt)
	assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
}
