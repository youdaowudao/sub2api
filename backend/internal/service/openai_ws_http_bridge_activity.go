package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

type openAIWSBridgeReadEvent struct {
	line      string
	err       error
	eof       bool
	keepalive bool
}

type openAIWSBridgeStreamEnd int

const (
	openAIWSBridgeStreamContinue openAIWSBridgeStreamEnd = iota
	openAIWSBridgeStreamIdle
	openAIWSBridgeStreamFirstOutput
	openAIWSBridgeStreamDrain
	openAIWSBridgeStreamControl
)

// Read ownership is separate from event processing, so a blocked SSE read
// cannot prevent the single consumer from enforcing its deadlines.
type openAIWSBridgeActivity struct {
	ctx              context.Context
	ctxDone          <-chan struct{}
	events           chan openAIWSBridgeReadEvent
	done             chan struct{}
	readerDone       chan struct{}
	closeBody        func()
	stopControlClose func() bool
	lastReadAt       atomic.Int64

	idleInterval, keepaliveInterval, drainInterval time.Duration
	firstDeadline, drainDeadline, lastWrite        time.Time
	idleTimer, firstTimer, drainTimer              *time.Timer
	keepaliveTicker                                *time.Ticker

	disconnected bool
	closed       bool
}

func newOpenAIWSBridgeActivity(ctx context.Context, body io.ReadCloser, cfg *config.Config, maxLineSize int, platform string, turnStart time.Time, firstTimeout time.Duration) *openAIWSBridgeActivity {
	started := time.Now()
	a := &openAIWSBridgeActivity{
		ctx: ctx, ctxDone: ctx.Done(), events: make(chan openAIWSBridgeReadEvent), done: make(chan struct{}), readerDone: make(chan struct{}),
		lastWrite: started, drainInterval: pkghttputil.DefaultUpstreamDrainTimeout,
	}
	intervalSeconds := 0
	if cfg != nil {
		intervalSeconds = cfg.Gateway.StreamDataIntervalTimeout
		if cfg.Gateway.StreamKeepaliveInterval > 0 {
			a.keepaliveInterval = time.Duration(cfg.Gateway.StreamKeepaliveInterval) * time.Second
		}
	}
	if intervalSeconds > 0 {
		a.idleInterval = time.Duration(intervalSeconds) * time.Second
		a.drainInterval = a.idleInterval
	}
	if platform == PlatformGrok {
		a.idleInterval = resolveGrokStreamIdleTimeout(intervalSeconds)
	}
	a.lastReadAt.Store(started.UnixNano())
	if a.idleInterval > 0 {
		a.idleTimer = time.NewTimer(a.idleInterval)
	}
	if firstTimeout > 0 {
		a.firstDeadline = turnStart.Add(firstTimeout)
		a.firstTimer = time.NewTimer(time.Until(a.firstDeadline))
	}
	if a.keepaliveInterval > 0 {
		a.keepaliveTicker = time.NewTicker(a.keepaliveInterval)
	}
	var bodyOnce sync.Once
	a.closeBody = func() { bodyOnce.Do(func() { _ = body.Close() }) }
	a.stopControlClose = context.AfterFunc(ctx, func() {
		if isOpenAIWSSessionPreempted(ctx) || errors.Is(context.Cause(ctx), ErrOpenAIWSIngressLeaseLost) {
			a.closeBody()
		}
	})
	go func() {
		defer close(a.readerDone)
		defer close(a.events)
		scanner := bufio.NewScanner(body)
		scanBuf := getSSEScannerBuf64K()
		defer putSSEScannerBuf64K(scanBuf)
		scanner.Buffer(scanBuf[:0], maxLineSize)
		scanner.Split(newBoundedSSESplit(cfg, maxLineSize, nil))
		for scanner.Scan() {
			a.lastReadAt.Store(time.Now().UnixNano())
			event := openAIWSBridgeReadEvent{line: scanner.Text()}
			select {
			case a.events <- event:
			case <-a.done:
				return
			}
		}
		select {
		case a.events <- openAIWSBridgeReadEvent{eof: true, err: scanner.Err()}:
		case <-a.done:
		}
	}()
	return a
}

func openAIWSBridgeTimerChannel(t *time.Timer) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}
func openAIWSBridgeTickerChannel(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (a *openAIWSBridgeActivity) stopFirstOutput() {
	if a.firstTimer != nil {
		a.firstTimer.Stop()
		a.firstTimer = nil
	}
	a.firstDeadline = time.Time{}
}

func (a *openAIWSBridgeActivity) startDrain() {
	if a.disconnected {
		return
	}
	a.disconnected = true
	a.stopFirstOutput()
	if a.keepaliveTicker != nil {
		a.keepaliveTicker.Stop()
		a.keepaliveTicker = nil
	}
	if !a.closed {
		a.drainDeadline = time.Now().Add(a.drainInterval)
		a.drainTimer = time.NewTimer(a.drainInterval)
	}
}

// The first actual cancellation remains authoritative. A later observed
// preemption can still end the read, but does not create another notifier.
func (a *openAIWSBridgeActivity) observeControl() error {
	if isOpenAIWSSessionPreempted(a.ctx) {
		// The original preemption owner sets the flag before sending its close
		// frame. Wait for its bounded close/cancel sequence instead of racing it.
		if a.ctx.Err() == nil {
			<-a.ctx.Done()
		}
		return errOpenAIWSSessionPreempted
	}
	if errors.Is(context.Cause(a.ctx), ErrOpenAIWSIngressLeaseLost) {
		return ErrOpenAIWSIngressLeaseLost
	}
	if a.ctx.Err() != nil {
		a.ctxDone = nil
		a.startDrain()
	}
	return nil
}

func (a *openAIWSBridgeActivity) idleDeadline() time.Time {
	if a.idleInterval <= 0 {
		return time.Time{}
	}
	return time.Unix(0, a.lastReadAt.Load()).Add(a.idleInterval)
}

func (a *openAIWSBridgeActivity) boundary() (openAIWSBridgeStreamEnd, error) {
	if err := a.observeControl(); err != nil {
		return openAIWSBridgeStreamControl, err
	}
	now := time.Now()
	reason := openAIWSBridgeStreamContinue
	var earliest time.Time
	consider := func(deadline time.Time, end openAIWSBridgeStreamEnd) {
		if !deadline.IsZero() && !now.Before(deadline) && (earliest.IsZero() || deadline.Before(earliest)) {
			reason, earliest = end, deadline
		}
	}
	consider(a.idleDeadline(), openAIWSBridgeStreamIdle)
	consider(a.firstDeadline, openAIWSBridgeStreamFirstOutput)
	consider(a.drainDeadline, openAIWSBridgeStreamDrain)
	return reason, nil
}

// next checks absolute deadlines even under a continuous sequence of lines.
func (a *openAIWSBridgeActivity) next() (openAIWSBridgeReadEvent, openAIWSBridgeStreamEnd, error) {
	for {
		if reason, err := a.boundary(); reason != openAIWSBridgeStreamContinue {
			return openAIWSBridgeReadEvent{}, reason, err
		}
		select {
		case <-a.ctxDone:
			continue
		case <-openAIWSBridgeTimerChannel(a.idleTimer):
			if reason, err := a.boundary(); reason != openAIWSBridgeStreamContinue {
				return openAIWSBridgeReadEvent{}, reason, err
			}
			a.idleTimer.Reset(time.Until(a.idleDeadline()))
		case <-openAIWSBridgeTimerChannel(a.firstTimer):
			continue
		case <-openAIWSBridgeTimerChannel(a.drainTimer):
			continue
		case <-openAIWSBridgeTickerChannel(a.keepaliveTicker):
			if reason, err := a.boundary(); reason != openAIWSBridgeStreamContinue {
				return openAIWSBridgeReadEvent{}, reason, err
			}
			if !a.disconnected && time.Since(a.lastWrite) >= a.keepaliveInterval {
				return openAIWSBridgeReadEvent{keepalive: true}, openAIWSBridgeStreamContinue, nil
			}
		case event, ok := <-a.events:
			if reason, err := a.boundary(); reason != openAIWSBridgeStreamContinue {
				return openAIWSBridgeReadEvent{}, reason, err
			}
			if !ok {
				event.eof = true
			}
			return event, openAIWSBridgeStreamContinue, nil
		}
	}
}

func (a *openAIWSBridgeActivity) write(message []byte, writer func([]byte) error) error {
	if err := a.observeControl(); err != nil {
		return err
	}
	if a.disconnected {
		return context.Canceled
	}
	err := writer(message)
	if err == nil {
		a.lastWrite = time.Now()
	} else if isOpenAIWSClientDisconnectError(err) {
		a.startDrain()
	}
	return err
}

// Close the final wrapped body only once. The pooled scanner buffer is
// returned by the read goroutine, and never while its read is still active.
func (a *openAIWSBridgeActivity) close() {
	if a.closed {
		return
	}
	a.closed = true
	if a.idleTimer != nil {
		a.idleTimer.Stop()
	}
	a.stopFirstOutput()
	if a.drainTimer != nil {
		a.drainTimer.Stop()
	}
	if a.keepaliveTicker != nil {
		a.keepaliveTicker.Stop()
	}
	a.stopControlClose()
	close(a.done)
	a.closeBody()
	<-a.readerDone
}
