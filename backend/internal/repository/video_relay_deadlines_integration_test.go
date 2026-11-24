//go:build integration

package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type videoRelayCommitConnector struct {
	driver.Connector
	commit func(context.Context, driver.Tx) error
}

type videoRelayCommitConnection interface {
	driver.Conn
	driver.ConnBeginTx
	driver.QueryerContext
	driver.ExecerContext
}

func (c videoRelayCommitConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return videoRelayBeginCommitHook{conn.(videoRelayCommitConnection), c.commit}, nil
}

type videoRelayBeginCommitHook struct {
	videoRelayCommitConnection
	commit func(context.Context, driver.Tx) error
}

func (c videoRelayBeginCommitHook) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.videoRelayCommitConnection.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return videoRelayCommitHook{Tx: tx, commit: func() error { return c.commit(ctx, tx) }}, nil
}

type videoRelayCommitHook struct {
	driver.Tx
	commit func() error
}

func (t videoRelayCommitHook) Commit() error { return t.commit() }

type videoRelaySettlementRepository struct {
	service.VideoRelayRepository
	settlement service.VideoRelayRepository
}

func (r videoRelaySettlementRepository) Settle(ctx context.Context, task *service.VideoRelayTask, change service.VideoRelaySettlement) (*service.VideoRelayTask, *service.UsageBillingApplyResult, error) {
	return r.settlement.Settle(ctx, task, change)
}

func TestVideoRelayRepoTrackerShutdownAtRealCommitBoundary(t *testing.T) {
	for _, test := range []struct {
		name, status, billing string
		committed, lostAck    bool
		balance               float64
		usage                 int
	}{
		{"charge interrupted before COMMIT", "completed", service.VideoRelayCharged, false, false, 5, 1},
		{"charge interrupted after COMMIT", "completed", service.VideoRelayCharged, true, false, 5, 1},
		{"charge COMMIT acknowledgement lost", "completed", service.VideoRelayCharged, true, true, 5, 1},
		{"refund interrupted before COMMIT", "failed", service.VideoRelayRefunded, false, false, 10, 0},
		{"refund interrupted after COMMIT", "failed", service.VideoRelayRefunded, true, false, 10, 0},
		{"refund COMMIT acknowledgement lost", "failed", service.VideoRelayRefunded, true, true, 10, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var statusReads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.Path != "/v1/videos/tracking-fixture-video" {
					http.NotFound(w, req)
					return
				}
				statusReads.Add(1)
				_, _ = io.WriteString(w, `{"id":"tracking-fixture-video","status":"`+test.status+`","delivery_status":"ready"}`)
			}))
			t.Cleanup(srv.Close)
			ctx := context.Background()
			raw, err := pq.NewConnector(integrationDSN)
			require.NoError(t, err)
			atCommit := make(chan context.Context, 1)
			resume := make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			commitDB := sql.OpenDB(videoRelayCommitConnector{Connector: raw, commit: func(work context.Context, tx driver.Tx) error {
				if test.committed {
					if err := tx.Commit(); err != nil {
						return err
					}
				}
				atCommit <- work
				<-resume
				if test.lostAck {
					return driver.ErrBadConn
				}
				if test.committed {
					return nil
				}
				// The worker has reached its commit with all real SQL executed.
				// End its connection's transaction without sending COMMIT.
				return errors.Join(work.Err(), tx.Rollback())
			}})
			t.Cleanup(func() { require.NoError(t, commitDB.Close()) })
			redisCache := NewBillingCache(testRedis(t))
			cfg := &config.Config{RunMode: config.RunModeStandard}
			oldCache := service.NewBillingCacheService(redisCache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(oldCache.Stop)
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 6*time.Hour, trackingVideoRelayFixtureOptions{
				billingCache: oldCache,
				wrapRepo: func(repo service.VideoRelayRepository) service.VideoRelayRepository {
					return videoRelaySettlementRepository{VideoRelayRepository: repo, settlement: NewVideoRelayRepository(commitDB)}
				},
			})
			require.NoError(t, redisCache.SetUserBalance(ctx, task.UserID, 5))
			tracker.Start()
			t.Cleanup(func() {
				release()
				tracker.Stop()
			})
			var work context.Context
			select {
			case work = <-atCommit:
			case <-time.After(5 * time.Second):
				t.Fatal("the settlement transaction never reached COMMIT")
			}
			restartedDB := videoRelayIndependentDB(t)
			if test.committed {
				assertVideoRelayFunds(t, restartedDB, task, test.balance, 0, test.usage)
			} else {
				assertVideoRelayFunds(t, restartedDB, task, 5, 5, 0)
			}
			cached, err := redisCache.GetUserBalance(ctx, task.UserID)
			require.NoError(t, err)
			require.EqualValues(t, 5, cached)

			stopped := make(chan struct{})
			go func() { tracker.Stop(); close(stopped) }()
			select {
			case <-work.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("Stop did not cancel work at the transaction boundary")
			}
			select {
			case <-stopped:
				t.Fatal("Stop returned before the outstanding settlement returned")
			default:
			}
			release()
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("Stop did not finish after the outstanding settlement returned")
			}
			oldCache.Stop()
			require.NoError(t, commitDB.Close())
			require.EqualValues(t, 1, statusReads.Load())
			other := NewVideoRelayRepository(restartedDB)
			freshCache := service.NewBillingCacheService(redisCache, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(freshCache.Stop)
			gateway := newTrackingVideoRelayFixtureGateway(t, NewAccountRepository(testEntClient(t), restartedDB, nil),
				trackingVideoRelayFixtureOptions{billingCache: freshCache})
			worker := service.NewVideoRelayTracker(service.NewVideoRelayService(other, gateway, nil))
			if test.committed {
				cached, err = redisCache.GetUserBalance(ctx, task.UserID)
				if test.lostAck {
					require.NoError(t, err)
					require.EqualValues(t, 5, cached, "an unacknowledged transaction must not claim a confirmed cache update")
				} else {
					require.ErrorIs(t, err, redis.Nil, "the old worker may finish the one committed settlement's cache cleanup")
				}
				require.NoError(t, redisCache.SetUserBalance(ctx, task.UserID, test.balance))
				taskAfterRestart, err := other.ClaimDue(ctx)
				require.NoError(t, err)
				if taskAfterRestart != nil {
					require.NotEqual(t, task.TaskID, taskAfterRestart.TaskID, "a settled order must not be claimed on restart")
				}
				key, err := NewAPIKeyRepository(testEntClient(t), restartedDB).GetByID(ctx, task.APIKeyID)
				require.NoError(t, err)
				reopened := newVideoRelayAuditRouter(t, restartedDB, other)
				path := "/v1/videos/" + task.TaskID
				lookup := videoRelayAuditRequest(reopened, key.Key, http.MethodGet, path, "")
				require.Equal(t, http.StatusOK, lookup.Code, lookup.Body.String())
				var public struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				}
				require.NoError(t, json.Unmarshal(lookup.Body.Bytes(), &public))
				require.Equal(t, task.TaskID, public.ID)
				require.Equal(t, test.status, public.Status, "the original customer query after restarting must preserve the committed outcome")
				if test.billing == service.VideoRelayRefunded {
					download := videoRelayAuditRequest(reopened, key.Key, http.MethodGet, path+"/content", "")
					require.Equal(t, http.StatusConflict, download.Code, download.Body.String())
				}
				cached, err = redisCache.GetUserBalance(ctx, task.UserID)
				require.NoError(t, err)
				require.EqualValues(t, test.balance, cached, "restart must preserve the newly filled balance cache")
			} else {
				assertVideoRelayFunds(t, restartedDB, task, 5, 5, 0)
				cached, err = redisCache.GetUserBalance(ctx, task.UserID)
				require.NoError(t, err)
				require.EqualValues(t, 5, cached, "a rolled-back settlement must not invalidate the wallet")
				makeVideoRelayTaskDue(t, restartedDB, task.TaskID)
				recovered, err := other.ClaimDue(ctx)
				require.NoError(t, err)
				require.NotNil(t, recovered)
				require.Equal(t, task.TaskID, recovered.TaskID)
				worker.ProcessClaim(ctx, recovered)
				_, err = redisCache.GetUserBalance(ctx, task.UserID)
				require.ErrorIs(t, err, redis.Nil, "a successful recovery must invalidate the wallet once")
			}
			wantReads := int32(2)
			if test.committed {
				wantReads = 1
			}
			require.Equal(t, wantReads, statusReads.Load())
			final, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, test.billing, final.BillingState)
			require.Equal(t, test.billing == service.VideoRelayCharged, final.CanDownload())
			require.Nil(t, final.NextCheckAt)
			assertVideoRelayFunds(t, restartedDB, task, test.balance, 0, test.usage)
		})
	}
}

type videoRelayLateDatabase struct {
	service.VideoRelayRepository
	calls    atomic.Int32
	finishes atomic.Int32
	work     chan context.Context
	returned chan struct{}
}

func (r *videoRelayLateDatabase) ClaimDue(ctx context.Context) (*service.VideoRelayTask, error) {
	r.calls.Add(1)
	return r.VideoRelayRepository.ClaimDue(ctx)
}

func (r *videoRelayLateDatabase) Settle(ctx context.Context, task *service.VideoRelayTask, change service.VideoRelaySettlement) (*service.VideoRelayTask, *service.UsageBillingApplyResult, error) {
	r.work <- ctx
	// A real PostgreSQL query is deliberately allowed to reply late, simulating
	// an already issued operation whose cancellation acknowledgement is delayed.
	_, err := integrationDB.ExecContext(context.WithoutCancel(ctx), "SELECT pg_sleep(26)")
	close(r.returned)
	if err != nil {
		return nil, nil, err
	}
	return r.VideoRelayRepository.Settle(ctx, task, change)
}

func (r *videoRelayLateDatabase) FinishClaim(ctx context.Context, task *service.VideoRelayTask, finish service.VideoRelayTrackingFinish) (*service.VideoRelayTask, error) {
	r.finishes.Add(1)
	return r.VideoRelayRepository.FinishClaim(ctx, task, finish)
}

type videoRelayLateSlot struct {
	service.ConcurrencyCache
	client   *redis.Client
	cleanup  chan context.Context
	returned chan struct{}
	err      chan error
}

func (s *videoRelayLateSlot) ReleaseAccountSlot(ctx context.Context, id int64, request string) error {
	s.cleanup <- ctx
	err := s.ConcurrencyCache.ReleaseAccountSlot(ctx, id, request)
	if err == nil {
		// Keep an actual isolated Redis command outstanding after cancellation.
		_, err = s.client.BLPop(context.WithoutCancel(ctx), 5*time.Second, "video:test:cleanup:blocked").Result()
		if errors.Is(err, redis.Nil) {
			err = nil
		}
	}
	s.err <- err
	close(s.returned)
	return err
}

func TestVideoRelayRepoTrackerDeadlinesWithLateRealDependencies(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"id":"tracking-fixture-video","status":"completed","delivery_status":"ready"}`)
	}))
	t.Cleanup(srv.Close)
	client := testRedis(t)
	slots := &videoRelayLateSlot{
		ConcurrencyCache: NewConcurrencyCache(client, 1, 10), client: client,
		cleanup: make(chan context.Context, 1), returned: make(chan struct{}), err: make(chan error, 1),
	}
	database := &videoRelayLateDatabase{work: make(chan context.Context, 1), returned: make(chan struct{})}
	task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 6*time.Hour, trackingVideoRelayFixtureOptions{
		concurrency: service.NewConcurrencyService(slots),
		wrapRepo: func(repo service.VideoRelayRepository) service.VideoRelayRepository {
			database.VideoRelayRepository = repo
			return database
		},
	})
	start := time.Now()
	tracker.Start()
	t.Cleanup(tracker.Stop)
	var work context.Context
	select {
	case work = <-database.work:
	case <-time.After(3 * time.Second):
		t.Fatal("video tracker did not get to the real PostgreSQL write path")
	}
	select {
	case <-work.Done():
	case <-time.After(27 * time.Second):
		t.Fatal("the 25 second business deadline did not cancel")
	}
	require.ErrorIs(t, work.Err(), context.DeadlineExceeded)
	require.InDelta(t, 25, time.Since(start).Seconds(), 1)
	require.EqualValues(t, 1, database.calls.Load())
	select {
	case <-database.returned:
		t.Fatal("the query should still be running after application cancellation")
	default:
	}

	var cleanup context.Context
	select {
	case cleanup = <-slots.cleanup:
	case <-time.After(4 * time.Second):
		t.Fatal("the tracker did not reach its slot cleanup after PostgreSQL actually returned")
	}
	stopped := make(chan struct{})
	go func() { tracker.Stop(); close(stopped) }()
	select {
	case <-cleanup.Done():
	case <-time.After(6 * time.Second):
		t.Fatal("the 30 second cleanup deadline did not cancel")
	}
	require.ErrorIs(t, cleanup.Err(), context.DeadlineExceeded)
	require.InDelta(t, 30, time.Since(start).Seconds(), 1)
	select {
	case <-stopped:
		t.Fatal("Stop returned before the Redis operation actually exited")
	default:
	}
	select {
	case <-stopped:
	case <-time.After(6 * time.Second):
		t.Fatal("Stop did not return after the bounded Redis fixture finished")
	}
	<-slots.returned
	require.NoError(t, <-slots.err)
	require.EqualValues(t, 1, database.calls.Load())
	require.Zero(t, database.finishes.Load())
	require.EqualValues(t, 1, calls.Load())
	current, err := repo.Get(context.Background(), task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayHeld, current.BillingState)
	assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)
}
