//go:build unit

package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	wsMeasureB     = int64(1 << 30)
	wsMeasureL     = int64(64 << 20)
	wsMeasureBytes = 27_000_000
	wsMeasureLimit = int64(1792 << 20)
)

// Each ordinary synthetic item contains a distinct ASCII text. The fake only
// records length, hash, and ordered text runs, never retains a request body.
type wsMeasureText struct {
	letter byte
	size   int
}

type wsMeasureItem struct {
	text      wsMeasureText
	assistant int
}

func wsMeasureAppendItem(dst []byte, item wsMeasureItem) []byte {
	if item.assistant == 0 {
		dst = append(dst, `{"type":"message","role":"user","content":"`...)
	} else {
		dst = fmt.Appendf(dst, `{"id":"msg_measure_%02d","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"`, item.assistant)
	}
	start := len(dst)
	dst = dst[:start+item.text.size]
	for i := start; i < len(dst); i++ {
		dst[i] = item.text.letter
	}
	if item.assistant == 0 {
		return append(dst, `"}`...)
	}
	return append(dst, `","annotations":[]}]}`...)
}

func wsMeasureFrame(items []wsMeasureItem, previous string) []byte {
	capacity := 512 + len(items)*256
	for _, item := range items {
		capacity += item.text.size
	}
	out := make([]byte, 0, capacity)
	out = append(out, `{"type":"response.create","model":"gpt-5.1","stream":true`...)
	if previous != "" {
		out = fmt.Appendf(out, `,"previous_response_id":%q`, previous)
	}
	out = append(out, `,"input":[`...)
	for i, item := range items {
		if i != 0 {
			out = append(out, ',')
		}
		out = wsMeasureAppendItem(out, item)
	}
	return append(out, ']', '}')
}

func wsMeasureOutput(turn int) wsMeasureItem {
	return wsMeasureItem{text: wsMeasureText{letter: byte('Q' + turn - 1), size: 64}, assistant: turn}
}

func wsMeasureID(turn int) string { return fmt.Sprintf("resp_measure_%02d", turn) }

// The complete replay contains the same four user notes and the three actual
// assistant outputs seen in the ordinary incremental conversation.
func wsMeasureConversation() []wsMeasureItem {
	items := []wsMeasureItem{
		{text: wsMeasureText{'A', 64}}, wsMeasureOutput(1),
		{text: wsMeasureText{'B', 0}}, wsMeasureOutput(2),
		{text: wsMeasureText{'C', 0}}, wsMeasureOutput(3),
		{text: wsMeasureText{'D', 0}},
	}
	remaining := wsMeasureBytes - len(wsMeasureFrame(items, ""))
	items[2].text.size = remaining / 3
	items[4].text.size = remaining / 3
	items[6].text.size = remaining - 2*(remaining/3)
	return items
}

// The bridge's ordinary map re-encoding writes sorted keys. Independently
// generate the expected complete wire digest with only a small repeated block,
// including every item, role, ID, output field and envelope byte.
func wsMeasureExpectedBody(items []wsMeasureItem) (int64, [32]byte) {
	h := sha256.New()
	var length int64
	write := func(part string) { n, _ := io.WriteString(h, part); length += int64(n) }
	write(`{"input":[`)
	for i, item := range items {
		if i != 0 {
			write(",")
		}
		if item.assistant == 0 {
			write(`{"content":"`)
		} else {
			write(`{"content":[{"annotations":[],"text":"`)
		}
		block := strings.Repeat(string(item.text.letter), min(item.text.size, 32<<10))
		for left := item.text.size; left > 0; {
			n := min(left, len(block))
			write(block[:n])
			left -= n
		}
		if item.assistant == 0 {
			write(`","role":"user","type":"message"}`)
		} else {
			write(fmt.Sprintf(`","type":"output_text"}],"id":"msg_measure_%02d","role":"assistant","status":"completed","type":"message"}`, item.assistant))
		}
	}
	write(`],"model":"gpt-5.1","stream":true}`)
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return length, sum
}

// Pause an ordinary HTTP read after half its known body has actually been read
// and reserved. The WS rebuild then runs while this HTTP scope remains alive.
// Resume the HTTP reader while the completed upstream request is still held.
type wsMeasureHTTPGate struct {
	ready, resume chan struct{}
}
type wsMeasureGatedBody struct {
	io.ReadCloser
	ctx       context.Context
	gate      *wsMeasureHTTPGate
	remaining int
	paused    bool
}

func (b *wsMeasureGatedBody) Read(p []byte) (int, error) {
	if !b.paused && b.remaining == 0 {
		b.paused = true
		close(b.gate.ready)
		select {
		case <-b.gate.resume:
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		}
	}
	if !b.paused {
		p = p[:min(len(p), b.remaining)]
	}
	n, err := b.ReadCloser.Read(p)
	if !b.paused {
		b.remaining -= n
	}
	return n, err
}

type wsMeasureUpstreamObservation struct {
	turn    int
	length  int64
	sum     [32]byte
	texts   []wsMeasureText
	proceed chan struct{}
}

type wsMeasureUpstream struct {
	ctx      context.Context
	observed chan wsMeasureUpstreamObservation
	turn     int
}

func (u *wsMeasureUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	defer req.Body.Close()
	// A streaming digest and the ordered unique text runs verify the complete
	// synthetic history without a second full body in the test double.
	h := sha256.New()
	var texts []wsMeasureText
	var run wsMeasureText
	flush := func() {
		if run.size >= 32 {
			texts = append(texts, run)
		}
	}
	buf := make([]byte, 32<<10)
	var total int64
	for {
		n, err := req.Body.Read(buf)
		if n > 0 {
			total += int64(n)
			_, _ = h.Write(buf[:n])
			for _, b := range buf[:n] {
				if b == run.letter {
					run.size++
				} else {
					flush()
					run = wsMeasureText{b, 1}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	flush()
	u.turn++
	observation := wsMeasureUpstreamObservation{turn: u.turn, length: total, texts: texts, proceed: make(chan struct{})}
	copy(observation.sum[:], h.Sum(nil))
	select {
	case u.observed <- observation:
	case <-u.ctx.Done():
		return nil, u.ctx.Err()
	}
	select {
	case <-observation.proceed:
	case <-u.ctx.Done():
		return nil, u.ctx.Err()
	}
	item := wsMeasureAppendItem(make([]byte, 0, 512), wsMeasureOutput(u.turn))
	body := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"model\":\"gpt-5.1\",\"status\":\"completed\",\"output\":[%s],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n", wsMeasureID(u.turn), item)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *wsMeasureUpstream) DoWithTLS(req *http.Request, p string, a int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, a, c)
}

type wsMeasureCache struct {
	service.GatewayCache
	ctx     context.Context
	commits chan chan struct{}
}

func (*wsMeasureCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, service.ErrStickySessionNotFound
}
func (*wsMeasureCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}
func (*wsMeasureCache) DeleteSessionAccountID(context.Context, int64, string) error { return nil }
func (c *wsMeasureCache) SetSessionAccountID(_ context.Context, _ int64, key string, _ int64, _ time.Duration) error {
	if !strings.HasPrefix(key, "openai:response:") {
		return nil
	}
	proceed := make(chan struct{})
	select {
	case c.commits <- proceed:
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
	select {
	case <-proceed:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

func wsMeasureBudgetUsed(budget *pkghttputil.BodyMemoryBudget) int64 {
	// Called only at a checkpoint with no concurrent body allocation. Probing
	// reserves bookkeeping bytes, not physical memory, and immediately refunds.
	ctx, release := pkghttputil.WithRequestBodyMemoryBudget(context.Background(), budget)
	defer release()
	lo, hi := int64(0), wsMeasureB
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if pkghttputil.ReserveRequestBodyMemory(ctx, mid) == nil {
			pkghttputil.ReleaseRequestBodyMemory(ctx, mid)
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return wsMeasureB - lo
}

func wsMeasureRSS() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) != 3 {
				return -1
			}
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return n * 1024
			}
		}
	}
	return -1
}

func wsMeasureCgroup(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("/proc/self/cgroup")
	require.NoError(t, err)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "0::") {
			return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(line, "0::"))
		}
	}
	t.Fatal("measurement requires cgroup v2 hard limits")
	return ""
}
func wsMeasureCgroupValue(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	require.NoError(t, err)
	return strings.TrimSpace(string(data))
}

type wsMeasureSampler struct {
	mu   sync.Mutex
	heap uint64
	rss  int64
}

func (s *wsMeasureSampler) sample() {
	s.mu.Lock()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	rss := wsMeasureRSS()
	s.heap = max(s.heap, stats.HeapAlloc)
	s.rss = max(s.rss, rss)
	s.mu.Unlock()
}
func (s *wsMeasureSampler) reset() { s.mu.Lock(); s.heap = 0; s.rss = 0; s.mu.Unlock() }
func (s *wsMeasureSampler) peak() (uint64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heap, s.rss
}

func wsMeasureCheckpoint(t *testing.T, name string, budget *pkghttputil.BodyMemoryBudget, gc bool) {
	t.Helper()
	if gc {
		runtime.GC()
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	used := wsMeasureBudgetUsed(budget)
	require.LessOrEqual(t, used, wsMeasureB/2)
	var inUse int64
	if gc {
		n, _ := runtime.MemProfile(nil, false)
		profile := make([]runtime.MemProfileRecord, n+64)
		if count, ok := runtime.MemProfile(profile, false); ok {
			for _, r := range profile[:count] {
				inUse += r.InUseBytes()
			}
		}
	}
	t.Logf("WS_MEMORY checkpoint=%s gc=%t heap_bytes=%d inuse_profile_bytes=%d rss_bytes=%d budget_used_bytes=%d", name, gc, stats.HeapAlloc, inUse, wsMeasureRSS(), used)
}

func TestOpenAIWSBodyMemoryLocalMeasure(t *testing.T) {
	if os.Getenv("SUB2API_WS_BODY_MEMORY_MEASURE") != "1" {
		t.Skip("explicit bounded CI measurement only")
	}
	root := wsMeasureCgroup(t)
	require.Equal(t, strconv.FormatInt(wsMeasureLimit, 10), wsMeasureCgroupValue(t, root, "memory.max"))
	require.Equal(t, "0", wsMeasureCgroupValue(t, root, "memory.swap.max"))
	cpu := strings.Fields(wsMeasureCgroupValue(t, root, "cpu.max"))
	require.Len(t, cpu, 2)
	require.Equal(t, cpu[1], cpu[0])
	require.Equal(t, 1, runtime.GOMAXPROCS(0))
	t.Logf("WS_MEMORY config go=%s memory_max=%d cpu_max=%s B=%d L=%d first_message_seconds=30", runtime.Version(), wsMeasureLimit, strings.Join(cpu, " "), wsMeasureB, wsMeasureL)
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	budget := pkghttputil.NewBodyMemoryBudget(wsMeasureB)
	fixtureCache := &wsMeasureCache{ctx: ctx, commits: make(chan chan struct{})}
	upstream := &wsMeasureUpstream{ctx: ctx, observed: make(chan wsMeasureUpstreamObservation)}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.RequestBodyMemoryBudgetBytes = wsMeasureB
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.ClientReadLimitBytes = wsMeasureL
	cfg.Gateway.OpenAIWS.ClientFirstMessageTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 30
	groupID := int64(4201)
	account := service.Account{ID: 9901, Name: "memory-fixture", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture-only", "base_url": "https://example.invalid"}, Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}}
	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billingCache.Stop()
	cache := &concurrencyCacheMock{acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }, acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil }}
	concurrency := service.NewConcurrencyService(cache)
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
	gateway := service.NewOpenAIGatewayService(accountRepo, usageRepo, nil, nil, nil, nil, fixtureCache, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, billingCache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	defer gateway.CloseOpenAIWSPool()
	handler := &OpenAIGatewayHandler{cfg: cfg, gatewayService: gateway, billingCacheService: billingCache, apiKeyService: &service.APIKeyService{}, concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatNone, time.Second)}
	apiKey := &service.APIKey{ID: 1801, GroupID: &groupID, User: &service.User{ID: 1701, Status: service.StatusActive}}
	done := make(chan struct{}, 1)
	httpDone := make(chan struct{}, 1)
	httpGates := make(chan *wsMeasureHTTPGate, 1)
	router := gin.New()
	router.Use(middleware.RequestBodyLimit(wsMeasureL, budget))
	router.GET("/v1/responses", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		handler.ResponsesWebSocket(c)
	})
	// A separate outer handler signals only after the real middleware released.
	router.POST("/http-body", func(c *gin.Context) {
		if c.GetHeader("X-Measure-Overlap") == "1" {
			select {
			case gate := <-httpGates:
				c.Request.Body = &wsMeasureGatedBody{ReadCloser: c.Request.Body, ctx: ctx, gate: gate, remaining: wsMeasureBytes / 2}
			case <-ctx.Done():
				return
			}
		}
		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.String(http.StatusOK, "%d:%x", len(body), sha256.Sum256(body))
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)
		if r.Method == http.MethodGet {
			done <- struct{}{}
		} else {
			httpDone <- struct{}{}
		}
	}))
	var httpWG sync.WaitGroup
	defer func() { cancel(); httpWG.Wait(); server.Close() }()
	// Keep fixture data in descriptors. The client writes each frame then drops
	// its allocation; the same full-frame content is constructed for reconnect.
	items := wsMeasureConversation()
	require.Equal(t, wsMeasureBytes, len(wsMeasureFrame(items, "")))
	httpProbe := func(overlap bool) error {
		started := time.Now()
		payload := wsMeasureFrame(items, "")
		want := fmt.Sprintf("%d:%x", len(payload), sha256.Sum256(payload))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/http-body", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		if overlap {
			req.Header.Set("X-Measure-Overlap", "1")
		}
		res, err := server.Client().Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		reply, err := io.ReadAll(io.LimitReader(res.Body, 128))
		if err != nil {
			return err
		}
		if res.StatusCode != http.StatusOK || string(reply) != want {
			return fmt.Errorf("HTTP probe: status=%d, body length or digest mismatch", res.StatusCode)
		}
		select {
		case <-httpDone:
		case <-ctx.Done():
			return ctx.Err()
		}
		t.Logf("WS_MEMORY http_probe overlap=%t bytes=%d status=%d elapsed_ms=%d", overlap, len(payload), res.StatusCode, time.Since(started).Milliseconds())
		return nil
	}
	dial := func() *coderws.Conn {
		conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		return conn
	}
	closeConn := func(conn *coderws.Conn) {
		_ = conn.CloseNow()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("measurement handler did not return")
		}
		require.Zero(t, wsMeasureBudgetUsed(budget), "all body reservations returned")
	}
	sampler := &wsMeasureSampler{}
	stopSamples := make(chan struct{})
	var sampleWG sync.WaitGroup
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sampler.sample()
			case <-stopSamples:
				return
			}
		}
	}()
	defer func() { close(stopSamples); sampleWG.Wait() }()
	wsMeasureCheckpoint(t, "empty", budget, true)
	conn := dial()
	defer conn.CloseNow()
	wsMeasureCheckpoint(t, "handshake", budget, true)
	closeConn(conn)
	wsMeasureCheckpoint(t, "handshake_closed", budget, true)
	conn = dial()
	defer conn.CloseNow()
	turn := func(conn *coderws.Conn, frameItems, expected []wsMeasureItem, previous string, index int, parallelHTTP bool) {
		sampler.reset()
		started := time.Now()
		var gate *wsMeasureHTTPGate
		var httpResult chan error
		if parallelHTTP {
			gate = &wsMeasureHTTPGate{ready: make(chan struct{}), resume: make(chan struct{})}
			httpGates <- gate
			httpResult = make(chan error, 1)
			httpWG.Add(1)
			go func() { defer httpWG.Done(); httpResult <- httpProbe(true) }()
			select {
			case <-gate.ready:
				t.Logf("WS_MEMORY turn=%d HTTP half body read before WS rebuild", index)
			case err := <-httpResult:
				t.Fatalf("HTTP probe returned before overlap checkpoint: %v", err)
			case <-ctx.Done():
				t.Fatal("HTTP overlap checkpoint missing")
			}
		}
		// Write in a separate stack so the client buffer is dead before idle GC.
		func() {
			writeCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			defer stop()
			require.NoError(t, conn.Write(writeCtx, coderws.MessageText, wsMeasureFrame(frameItems, previous)))
		}()
		var obs wsMeasureUpstreamObservation
		select {
		case obs = <-upstream.observed:
		case <-ctx.Done():
			t.Fatal("fake upstream did not receive generation")
		}
		require.Equal(t, index, obs.turn)
		want := make([]wsMeasureText, len(expected))
		for i, item := range expected {
			want[i] = item.text
		}
		require.Equal(t, want, obs.texts, "complete text and output history must be in order")
		wantLength, wantHash := wsMeasureExpectedBody(expected)
		require.Equal(t, wantLength, obs.length, "complete upstream request byte count")
		require.Equal(t, wantHash, obs.sum, "complete upstream request structure, order and content")
		t.Logf("WS_MEMORY upstream turn=%d bytes=%d sha256=%x", index, obs.length, obs.sum)
		// The HTTP probe's half-read scope remained live throughout rebuild.
		// Finish it while the upstream attempt still owns its assembled request;
		// probe the WS budget only after HTTP has released its own scope.
		if parallelHTTP {
			close(gate.resume)
			select {
			case err := <-httpResult:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("concurrent HTTP probe did not finish")
			}
		}
		wsMeasureCheckpoint(t, fmt.Sprintf("turn_%d_active", index), budget, false)
		close(obs.proceed)
		readCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		for {
			_, event, err := conn.Read(readCtx)
			require.NoError(t, err)
			if gjson.GetBytes(event, "type").String() == "response.completed" {
				require.Equal(t, wsMeasureID(index), gjson.GetBytes(event, "response.id").String())
				break
			}
		}
		var proceed chan struct{}
		select {
		case proceed = <-fixtureCache.commits:
		case <-ctx.Done():
			t.Fatal("post-history commit checkpoint missing")
		}
		wsMeasureCheckpoint(t, fmt.Sprintf("turn_%d_idle", index), budget, true)
		if parallelHTTP {
			require.NoError(t, httpProbe(false))
		}
		close(proceed)
		heap, rss := sampler.peak()
		t.Logf("WS_MEMORY turn=%d elapsed_ms=%d sampled_heap_peak_bytes=%d sampled_rss_peak_bytes=%d", index, time.Since(started).Milliseconds(), heap, rss)
	}
	for i := 0; i < 4; i++ {
		previous := ""
		if i > 0 {
			previous = wsMeasureID(i)
		}
		turn(conn, items[i*2:i*2+1], items[:i*2+1], previous, i+1, i == 3)
	}
	closeConn(conn)
	wsMeasureCheckpoint(t, "incremental_closed", budget, true)
	conn = dial()
	defer conn.CloseNow()
	turn(conn, items, items, "", 5, true)
	closeConn(conn)
	wsMeasureCheckpoint(t, "reconnect_closed", budget, true)
	require.Equal(t, 5, upstream.turn)
	peak, err := strconv.ParseInt(wsMeasureCgroupValue(t, root, "memory.peak"), 10, 64)
	require.NoError(t, err)
	require.Less(t, peak, wsMeasureLimit)
	events := wsMeasureCgroupValue(t, root, "memory.events")
	require.Contains(t, "\n"+events+"\n", "\noom 0\n")
	require.Contains(t, "\n"+events+"\n", "\noom_kill 0\n")
	t.Logf("WS_MEMORY cgroup_peak_bytes=%d events=%q", peak, events)
}
