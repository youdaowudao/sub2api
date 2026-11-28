package httputil

import (
	"context"
	"errors"
	"io"
	"math"
	"sync"
	"unsafe"
)

// ErrWSBodySizeLimit is the actual uncompressed message size violation, not
// an exhausted shared body budget.
var ErrWSBodySizeLimit = errors.New("websocket message exceeds configured size limit")

// wsReadChunkPlan computes the fixed header count for the existing bounded
// HTTP chunk growth pattern, without allocating the chunks. Large limits use
// arithmetic for the remaining equal-size chunks, never a long loop.
func wsReadChunkPlan(limit int64) (int, int64, error) {
	if limit <= 0 || limit > int64(int(^uint(0)>>1)) {
		return 0, 0, ErrRequestBodyMemoryBudget
	}
	var count int64
	remaining := limit
	for size := int64(requestBodyReadInitCap); remaining > 0 && size < requestBodyReadMaxInitCap; size *= 2 {
		remaining -= min(remaining, size)
		count++
	}
	if remaining > 0 {
		count += 1 + (remaining-1)/requestBodyReadMaxInitCap
	}
	headSize := int64(unsafe.Sizeof([]byte(nil)))
	if count > (math.MaxInt64-1)/headSize || limit > (math.MaxInt64-count*headSize-1)/2 {
		return 0, 0, ErrRequestBodyMemoryBudget
	}
	return int(count), 2*limit + count*headSize + 1, nil
}

type wsReadCreditKey struct{}
type wsReadCredit struct {
	mu          sync.Mutex
	reservation *bodyMemoryReservation
}

func (c *wsReadCredit) take() *bodyMemoryReservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.reservation
	c.reservation = nil
	return r
}

// BeginWSRequestBodyMemory uses the original shared budget, a WS category
// inside it, and first-message reading credit. It must run before accepting
// a socket. No capacity is allocated for the unsent first message.
func BeginWSRequestBodyMemory(ctx context.Context, limit int64) (context.Context, func(), error) {
	parent, _ := ctx.Value(bodyMemoryReservationKey{}).(*bodyMemoryReservation)
	if parent == nil {
		return ctx, func() {}, nil
	}
	_, n, err := wsReadChunkPlan(limit)
	if err != nil {
		return ctx, func() {}, err
	}
	parent.mu.Lock()
	closed := parent.closed
	parent.mu.Unlock()
	if closed {
		return ctx, func() {}, ErrRequestBodyMemoryBudget
	}
	r := &bodyMemoryReservation{budget: parent.budget, ws: true}
	root := context.WithValue(ctx, bodyMemoryReservationKey{}, r)
	creditReservation := &bodyMemoryReservation{budget: r.budget, ws: true}
	creditCtx := context.WithValue(root, bodyMemoryReservationKey{}, creditReservation)
	if err := ReserveRequestBodyMemory(creditCtx, n); err != nil {
		return ctx, func() {}, err
	}
	credit := &wsReadCredit{reservation: creditReservation}
	return context.WithValue(root, wsReadCreditKey{}, credit), func() {
		if pending := credit.take(); pending != nil {
			pending.release()
		}
		r.release()
	}, nil
}

func HasWSRequestBodyMemory(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	r, _ := ctx.Value(bodyMemoryReservationKey{}).(*bodyMemoryReservation)
	return r != nil && r.ws
}

// WSBodyMemoryOwner holds one independent allocation. Hold transfers a shared
// reference, not another charge. Drop releases only on the last owner.
// Every holder must stop using its slices before dropping its reference.
type WSBodyMemoryOwner struct {
	mu   sync.Mutex
	r    *bodyMemoryReservation
	refs int
}

func (o *WSBodyMemoryOwner) Hold() *WSBodyMemoryOwner {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.refs <= 0 {
		panic("hold of released WS body owner")
	}
	o.refs++
	return o
}
func (o *WSBodyMemoryOwner) Drop() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.refs <= 0 {
		panic("duplicate WS body owner drop")
	}
	o.refs--
	if o.refs == 0 {
		o.r.release()
	}
}

func ReserveWSBodyMemoryOwner(ctx context.Context, n int64) (*WSBodyMemoryOwner, error) {
	if !HasWSRequestBodyMemory(ctx) {
		return nil, nil
	}
	if n < 0 {
		return nil, ErrRequestBodyMemoryBudget
	}
	memoryCtx, release := WithRequestBodyMemoryScope(ctx)
	if err := ReserveRequestBodyMemory(memoryCtx, n); err != nil {
		release()
		return nil, err
	}
	r, _ := memoryCtx.Value(bodyMemoryReservationKey{}).(*bodyMemoryReservation)
	return &WSBodyMemoryOwner{r: r, refs: 1}, nil
}

// CloneWSBody reserves the independent backing capacity before copying.
func CloneWSBody(ctx context.Context, source []byte) ([]byte, *WSBodyMemoryOwner, error) {
	owner, err := ReserveWSBodyMemoryOwner(ctx, int64(len(source)))
	if err != nil {
		return nil, nil, err
	}
	out := make([]byte, len(source))
	copy(out, source)
	return out, owner, nil
}

// ReadWSBody uses the existing bounded chunk growth pattern for decrypted
// message bytes. The first message consumes already-reserved credit; later
// reads reserve each actual allocation. A complete body gets its own owner.
func ReadWSBody(ctx context.Context, reader io.Reader, limit int64) (data []byte, owner *WSBodyMemoryOwner, err error) {
	count, _, err := wsReadChunkPlan(limit)
	if err != nil {
		return nil, nil, err
	}
	parent, _ := ctx.Value(bodyMemoryReservationKey{}).(*bodyMemoryReservation)
	if parent == nil || !parent.ws {
		return nil, nil, ErrRequestBodyMemoryBudget
	}
	r := &bodyMemoryReservation{budget: parent.budget, ws: true}
	credit, _ := ctx.Value(wsReadCreditKey{}).(*wsReadCredit)
	first := false
	if credit != nil {
		if taken := credit.take(); taken != nil {
			r = taken
			first = true
		}
	}
	readCtx := context.WithValue(ctx, bodyMemoryReservationKey{}, r)
	defer func() {
		if err != nil || owner == nil {
			r.release()
		}
	}()
	reserve := func(n int64) error {
		if first {
			return nil
		}
		return ReserveRequestBodyMemory(readCtx, n)
	}
	if err = reserve(int64(count) * int64(unsafe.Sizeof([]byte(nil)))); err != nil {
		return nil, nil, err
	}
	chunks := make([][]byte, 0, count)
	total := 0
	for size := requestBodyReadInitCap; int64(total) < limit; size = min(size*2, requestBodyReadMaxInitCap) {
		capBytes := int(min(int64(size), limit-int64(total)))
		if err = reserve(int64(capBytes)); err != nil {
			return nil, nil, err
		}
		chunk := make([]byte, capBytes)
		n := 0
		eof := false
		for n < len(chunk) {
			read, readErr := reader.Read(chunk[n:])
			n += read
			if readErr == io.EOF {
				eof = true
				break
			}
			if readErr != nil {
				return nil, nil, readErr
			}
			if read == 0 {
				return nil, nil, io.ErrNoProgress
			}
		}
		if n > 0 {
			chunks = append(chunks, chunk[:n])
			total += n
		}
		if eof {
			break
		}
	}
	if int64(total) == limit {
		if err = reserve(1); err != nil {
			return nil, nil, err
		}
		var probe [1]byte
		n, readErr := io.ReadFull(reader, probe[:])
		if n != 0 {
			return nil, nil, ErrWSBodySizeLimit
		}
		if readErr != io.EOF {
			return nil, nil, readErr
		}
	}
	if len(chunks) == 0 {
		return nil, nil, nil
	}
	if len(chunks) == 1 {
		data = chunks[0]
	} else {
		if err = reserve(int64(total)); err != nil {
			return nil, nil, err
		}
		data = make([]byte, total)
		offset := 0
		for _, part := range chunks {
			offset += copy(data[offset:], part)
		}
	}
	// All temporary chunk/header storage is dead; only the completed backing
	// array lives in the caller's owner. The first credit's unused bytes are
	// refunded at exactly the same point.
	r.mu.Lock()
	reserved := r.bytes
	r.mu.Unlock()
	ReleaseRequestBodyMemory(readCtx, reserved-int64(cap(data)))
	owner = &WSBodyMemoryOwner{r: r, refs: 1}
	return data, owner, nil
}

// The ingress handler owns the first frame; the proxy borrows it and holds a
// reference for any replay views that outlive the immediate call site.
type wsSourceOwnerKey struct{}

func WithWSBodyMemorySource(ctx context.Context, owner *WSBodyMemoryOwner) context.Context {
	if owner == nil {
		return ctx
	}
	return context.WithValue(ctx, wsSourceOwnerKey{}, owner)
}

func WSBodyMemorySource(ctx context.Context) *WSBodyMemoryOwner {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(wsSourceOwnerKey{}).(*WSBodyMemoryOwner)
	return owner
}
