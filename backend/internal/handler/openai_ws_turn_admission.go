package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var errOpenAIWSLocalTurnAdmission = errors.New("websocket generation admission rejected")

// A standard-mode logical turn owns one immutable authorization snapshot and
// one reservation reference, independently of the upstream attempt numbering.
type openAIWSLogicalTurnRecord struct {
	number                      int
	key                         *service.APIKey
	subscription                *service.UserSubscription
	platform                    string
	model                       string
	pricingCtx                  context.Context
	pricingAt                   time.Time
	reservationCtx              context.Context
	reservationDone             func()
	checked, admitted, finished bool
}

type openAIWSLogicalTurnAdmission struct {
	mu      sync.Mutex
	current openAIWSLogicalTurnRecord
}

func (a *openAIWSLogicalTurnAdmission) snapshot() openAIWSLogicalTurnRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

func (a *openAIWSLogicalTurnAdmission) save(r openAIWSLogicalTurnRecord) {
	a.mu.Lock()
	a.current = r
	a.mu.Unlock()
}

func (a *openAIWSLogicalTurnAdmission) finish(number int) {
	a.mu.Lock()
	if a.current.number != number {
		a.mu.Unlock()
		return
	}
	done := a.current.reservationDone
	a.current.reservationDone = nil
	a.current.reservationCtx = nil
	a.current.finished = true
	a.mu.Unlock()
	if done != nil {
		done()
	}
}

func (a *openAIWSLogicalTurnAdmission) close() {
	a.finish(a.snapshot().number)
}
