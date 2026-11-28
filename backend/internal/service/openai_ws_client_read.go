package service

import (
	"context"
	"errors"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	coderws "github.com/coder/websocket"
)

type openAIWSClientReadResult struct {
	messageType coderws.MessageType
	payload     []byte
	owner       *pkghttputil.WSBodyMemoryOwner
	err         error
}

// ReadOpenAIWSClientMessage keeps one reader alive while control events send
// their close frame, then closes the transport and joins that reader.
func ReadOpenAIWSClientMessage(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
) (coderws.MessageType, []byte, error) {
	return readOpenAIWSClientMessageWithTimeoutStart(
		controlCtx,
		conn,
		timeout,
		timeoutStatus,
		timeoutReason,
		nil,
		nil,
	)
}

// readOpenAIWSClientMessageWithTimeoutStart supports readers whose timeout
// starts after a state transition, such as a completed passthrough turn. When
// timeoutActive is nil, a positive timeout starts immediately.
func readOpenAIWSClientMessageWithTimeoutStart(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
	timeoutStart <-chan struct{},
	timeoutActive func() bool,
) (coderws.MessageType, []byte, error) {
	kind, payload, _, err := readOpenAIWSClientMessageWithMemory(controlCtx, nil, 0, conn, timeout, timeoutStatus, timeoutReason, timeoutStart, timeoutActive)
	return kind, payload, err
}

// ReadOpenAIWSClientMessageWithBodyMemory passes the completed buffer's owner
// to the caller, keeping budget lifetime independent of the control context.
func ReadOpenAIWSClientMessageWithBodyMemory(controlCtx, memoryCtx context.Context, limit int64, conn *coderws.Conn, timeout time.Duration, timeoutStatus coderws.StatusCode, timeoutReason string) (coderws.MessageType, []byte, *pkghttputil.WSBodyMemoryOwner, error) {
	return readOpenAIWSClientMessageWithMemory(controlCtx, memoryCtx, limit, conn, timeout, timeoutStatus, timeoutReason, nil, nil)
}

func readOpenAIWSClientMessageWithMemory(controlCtx, memoryCtx context.Context, limit int64, conn *coderws.Conn, timeout time.Duration, timeoutStatus coderws.StatusCode, timeoutReason string, timeoutStart <-chan struct{}, timeoutActive func() bool) (coderws.MessageType, []byte, *pkghttputil.WSBodyMemoryOwner, error) {
	if conn == nil {
		return 0, nil, nil, errors.New("openai websocket client connection is nil")
	}
	if controlCtx == nil {
		controlCtx = context.Background()
	}

	readDone := make(chan openAIWSClientReadResult, 1)
	go func() {
		if !pkghttputil.HasWSRequestBodyMemory(memoryCtx) {
			messageType, payload, err := conn.Read(context.Background())
			readDone <- openAIWSClientReadResult{messageType: messageType, payload: payload, err: err}
			return
		}
		messageType, reader, err := conn.Reader(context.Background())
		var payload []byte
		var owner *pkghttputil.WSBodyMemoryOwner
		if err == nil {
			payload, owner, err = pkghttputil.ReadWSBody(memoryCtx, reader, limit)
		}
		if errors.Is(err, pkghttputil.ErrRequestBodyMemoryBudget) {
			err = NewOpenAIWSBodyMemoryCloseError(err)
		}
		if errors.Is(err, pkghttputil.ErrWSBodySizeLimit) || errors.Is(err, coderws.ErrMessageTooBig) {
			err = NewOpenAIWSClientCloseError(coderws.StatusMessageTooBig, "websocket message exceeds configured size limit", err)
		}
		readDone <- openAIWSClientReadResult{messageType: messageType, payload: payload, owner: owner, err: err}
	}()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	startTimeout := func() {
		if timeout <= 0 || (timeoutActive != nil && !timeoutActive()) {
			return
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		}
		timeoutCh = timer.C
	}
	if timeoutActive == nil || timeoutActive() {
		startTimeout()
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	closeAndJoin := func(status coderws.StatusCode, reason string, cause error) (coderws.MessageType, []byte, *pkghttputil.WSBodyMemoryOwner, error) {
		_ = conn.Close(status, reason)
		_ = conn.CloseNow()
		result := <-readDone
		result.owner.Drop()
		return 0, nil, nil, NewOpenAIWSClientCloseError(status, reason, cause)
	}

	for {
		select {
		case result := <-readDone:
			return result.messageType, result.payload, result.owner, result.err
		case <-timeoutStart:
			startTimeout()
		case <-timeoutCh:
			return closeAndJoin(timeoutStatus, timeoutReason, context.DeadlineExceeded)
		case <-controlCtx.Done():
			cause := context.Cause(controlCtx)
			if errors.Is(cause, ErrOpenAIWSIngressLeaseLost) {
				return closeAndJoin(
					coderws.StatusTryAgainLater,
					"websocket ingress capacity lease lost; please reconnect",
					cause,
				)
			}
			return closeAndJoin(coderws.StatusGoingAway, "websocket request canceled", cause)
		}
	}
}

// NewOpenAIWSBodyMemoryCloseError is local capacity pressure, never upstream
// health/failover or a claim that the client's message exceeded the limit.
func NewOpenAIWSBodyMemoryCloseError(cause error) error {
	return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "WebSocket request memory is temporarily unavailable; please reconnect or use HTTP", cause)
}
