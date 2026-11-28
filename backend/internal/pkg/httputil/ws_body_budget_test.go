//go:build unit

package httputil

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWSBodyMemorySharedBudgetAndOwner(t *testing.T) {
	const total = int64(1 << 20)
	budget := NewBodyMemoryBudget(total)
	httpCtx, httpRelease := WithRequestBodyMemoryBudget(context.Background(), budget)
	defer httpRelease()
	ctx, release, err := BeginWSRequestBodyMemory(httpCtx, 64<<10)
	require.NoError(t, err)
	defer release()
	count, credit, err := wsReadChunkPlan(64 << 10)
	require.NoError(t, err)
	require.Equal(t, 8, count)
	require.Equal(t, credit, budget.used)
	require.Equal(t, credit, budget.wsUsed)
	data, owner, err := ReadWSBody(ctx, bytes.NewReader([]byte(`{"input":"hello"}`)), 64<<10)
	require.NoError(t, err)
	require.Equal(t, `{"input":"hello"}`, string(data))
	require.Equal(t, int64(cap(data)), budget.used)
	second := owner.Hold()
	owner.Drop()
	require.Equal(t, int64(cap(data)), budget.wsUsed)
	copyBody, independent, err := CloneWSBody(ctx, data)
	require.NoError(t, err)
	require.Equal(t, data, copyBody)
	require.Equal(t, int64(cap(data)+cap(copyBody)), budget.used)
	second.Drop()
	independent.Drop()
	require.Zero(t, budget.used)
	require.Zero(t, budget.wsUsed)
	// Direct accounting policy: each class is bounded and the HTTP share is
	// still available; this does not allocate a request of that size.
	wsScope, wsDone := WithRequestBodyMemoryScope(ctx)
	defer wsDone()
	require.NoError(t, ReserveRequestBodyMemory(wsScope, total/2))
	require.ErrorIs(t, ReserveRequestBodyMemory(wsScope, 1), ErrRequestBodyMemoryBudget)
	require.NoError(t, ReserveRequestBodyMemory(httpCtx, total/2))
	require.Equal(t, total, budget.used)
	wsDone()
	require.Equal(t, total/2, budget.used)
	require.Zero(t, budget.wsUsed)
	ReleaseRequestBodyMemory(httpCtx, total/2)
	require.Zero(t, budget.used)
}

func TestWSBodyReadConfiguredLimit(t *testing.T) {
	budget := NewBodyMemoryBudget(1 << 20)
	httpCtx, release := WithRequestBodyMemoryBudget(context.Background(), budget)
	defer release()
	ctx, done, err := BeginWSRequestBodyMemory(httpCtx, 256)
	require.NoError(t, err)
	defer done()
	exact := bytes.Repeat([]byte("x"), 256)
	got, owner, err := ReadWSBody(ctx, bytes.NewReader(exact), 256)
	require.NoError(t, err)
	require.Equal(t, exact, got)
	owner.Drop()
	require.Zero(t, budget.used)
	_, owner, err = ReadWSBody(ctx, bytes.NewReader(append(exact, 'x')), 256)
	require.ErrorIs(t, err, ErrWSBodySizeLimit)
	require.Nil(t, owner)
	require.Zero(t, budget.used)
}
