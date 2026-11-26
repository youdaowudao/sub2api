//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const checkinTestConfigJSON = `{"enabled":true,"veteran_threshold":20,"veteran_min":0.05,"veteran_max":0.30,"newcomer_min":0.01,"newcomer_max":0.05,"newcomer_cap":0.05,"newcomer_after_cap":0.01,"newcomer_captcha":false,"min_account_age_days":1,"ip_daily_limit":3,"email_verify_enabled":false,"email_verify_first":3,"email_verify_every":7,"email_verify_skip_reserved":true}`

var checkinRepositoryNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func checkinRepoFixture(t *testing.T) (service.CheckinRepository, *service.CheckinService, service.SettingRepository) {
	t.Helper()
	client := testEntClient(t)
	repo := NewCheckinRepository(client)
	settings := NewSettingRepository(client)
	s := service.NewCheckinService(repo, settings, nil, nil, nil, nil, nil, nil, nil)
	s.Clock = func() time.Time { return checkinRepositoryNow }
	s.Random = func(n int64) int64 { return n - 1 }
	_, err := s.SaveConfig(context.Background(), []byte(checkinTestConfigJSON))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settings.Delete(context.Background(), service.SettingKeyCheckinConfig)) })
	return repo, s, settings
}
func checkinRepoUser(t *testing.T, name, balance, recharged string) int64 {
	t.Helper()
	var id int64
	email := fmt.Sprintf("ck-%s-%d@example.test", name, time.Now().UnixNano())
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,balance,total_recharged,created_at) VALUES($1,'fixture-only',$2::decimal,$3::decimal,$4) RETURNING id`, email, balance, recharged, checkinRepositoryNow.AddDate(0, -2, 0)).Scan(&id))
	t.Cleanup(func() { _, e := integrationDB.Exec(`DELETE FROM users WHERE id=$1`, id); require.NoError(t, e) })
	return id
}
func checkinRepoBalance(t *testing.T, id int64, wantBalance, wantTotal string, wantRecords int) {
	t.Helper()
	var balance, total string
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT balance::text,total_recharged::text,(SELECT count(*) FROM checkin_records WHERE user_id=$1) FROM users WHERE id=$1`, id).Scan(&balance, &total, &count))
	require.Equal(t, wantBalance, balance)
	require.Equal(t, wantTotal, total)
	require.Equal(t, wantRecords, count)
}
func checkinRepoContext(ip string) context.Context {
	return service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: ip})
}
func TestCheckinRepositoryIPConcurrent(t *testing.T) {
	_, s, _ := checkinRepoFixture(t)
	ids := make([]int64, 4)
	for i := range ids {
		ids[i] = checkinRepoUser(t, fmt.Sprint(i), "0", "0")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	start := make(chan struct{})
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id int64) {
			defer wg.Done()
			<-start
			ip := "2001:db8::1"
			if i == 1 {
				ip = "2001:0db8:0:0:0:0:0:1"
			}
			_, e := s.Checkin(checkinRepoContext(ip), id, "", service.CheckinProof{}, "")
			errs <- e
		}(i, id)
	}
	close(start)
	wg.Wait()
	close(errs)
	var success, limited int
	for e := range errs {
		if e == nil {
			success++
		} else {
			require.Equal(t, "CHECKIN_IP_LIMIT", infraerrors.Reason(e))
			limited++
		}
	}
	require.Equal(t, 3, success)
	require.Equal(t, 1, limited)
	var n int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM checkin_records WHERE ip='2001:db8::1' AND user_tier<>'veteran'`).Scan(&n))
	require.Equal(t, 3, n)
	veteran := checkinRepoUser(t, "veteran", "0", "20")
	_, e := s.Checkin(checkinRepoContext("2001:db8::1"), veteran, "", service.CheckinProof{}, "")
	require.NoError(t, e)
	checkinRepoBalance(t, veteran, "0.30000000", "20.00000000", 1)
}
func TestCheckinRepositorySameUserConcurrent(t *testing.T) {
	_, s, _ := checkinRepoFixture(t)
	id := checkinRepoUser(t, "same", "-1", "20")
	var wg sync.WaitGroup
	start := make(chan struct{})
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := s.Checkin(checkinRepoContext(""), id, "", service.CheckinProof{}, "")
			result <- e
		}()
	}
	close(start)
	wg.Wait()
	close(result)
	var success, done int
	for e := range result {
		if e == nil {
			success++
		} else {
			require.Equal(t, "CHECKIN_ALREADY_DONE", infraerrors.Reason(e))
			done++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, done)
	checkinRepoBalance(t, id, "-0.70000000", "20.00000000", 1)
}

// Pause the first call after its real row lock, so the second actually waits on
// the row. This detects statistics being read using the pre-lock statement snapshot.
type checkinWaitingRepo struct {
	service.CheckinRepository
	calls                      atomic.Int32
	firstLocked, secondStarted chan struct{}
}

func (r *checkinWaitingRepo) LockUser(ctx context.Context, id int64, keys []string) (service.CheckinUser, error) {
	n := r.calls.Add(1)
	if n == 2 {
		close(r.secondStarted)
	}
	u, e := r.CheckinRepository.LockUser(ctx, id, keys)
	if e != nil {
		return u, e
	}
	if n == 1 {
		close(r.firstLocked)
		select {
		case <-r.secondStarted:
		case <-ctx.Done():
			return u, ctx.Err()
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			var waiting int
			e = integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query ILIKE '%FOR UPDATE%'`).Scan(&waiting)
			if e != nil {
				return u, e
			}
			if waiting > 0 {
				break
			}
			if time.Now().After(deadline) {
				return u, fmt.Errorf("second transaction did not wait on user lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return u, nil
}
func TestCheckinRepositoryCrossMidnight(t *testing.T) {
	for _, verifyEmail := range []bool{false, true} {
		t.Run(fmt.Sprintf("email=%t", verifyEmail), func(t *testing.T) {
			repo, _, settings := checkinRepoFixture(t)
			id := checkinRepoUser(t, "midnight", "0", "0")
			hooked := &checkinWaitingRepo{CheckinRepository: repo, firstLocked: make(chan struct{}), secondStarted: make(chan struct{})}
			s := service.NewCheckinService(hooked, settings, nil, nil, nil, nil, nil, nil, nil)
			var clockCalls atomic.Int32
			day := timezone.StartOfDay(checkinRepositoryNow)
			first := day.AddDate(0, 0, 1).Add(-time.Second)
			second := first.Add(2 * time.Second)
			s.Clock = func() time.Time {
				if clockCalls.Add(1) == 1 {
					return first
				}
				return second
			}
			s.Random = func(n int64) int64 { return n - 1 }
			if verifyEmail {
				cfg := strings.Replace(checkinTestConfigJSON, `"email_verify_enabled":false`, `"email_verify_enabled":true`, 1)
				cfg = strings.Replace(cfg, `"email_verify_first":3`, `"email_verify_first":1`, 1)
				_, e := s.SaveConfig(context.Background(), []byte(cfg))
				require.NoError(t, e)
			}
			ctx, cancel := context.WithTimeout(checkinRepoContext("192.0.2.1"), 15*time.Second)
			defer cancel()
			a, b := make(chan error, 1), make(chan error, 1)
			go func() { _, e := s.Checkin(ctx, id, "", service.CheckinProof{}, ""); a <- e }()
			select {
			case <-hooked.firstLocked:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go func() { _, e := s.Checkin(ctx, id, "", service.CheckinProof{}, ""); b <- e }()
			require.NoError(t, <-a)
			e := <-b
			if verifyEmail {
				require.Equal(t, "CHECKIN_EMAIL_REQUIRED", infraerrors.Reason(e))
				checkinRepoBalance(t, id, "0.05000000", "0.00000000", 1)
				return
			}
			require.NoError(t, e)
			checkinRepoBalance(t, id, "0.06000000", "0.00000000", 2)
			rows, e := integrationDB.Query(`SELECT checkin_date::text,created_at,amount::text FROM checkin_records WHERE user_id=$1 ORDER BY id`, id)
			require.NoError(t, e)
			defer rows.Close()
			for i, now := range []time.Time{first, second} {
				require.True(t, rows.Next())
				var d, amount string
				var created time.Time
				require.NoError(t, rows.Scan(&d, &created, &amount))
				require.Equal(t, timezone.StartOfDay(now).Format("2006-01-02"), d)
				require.True(t, now.Equal(created))
				require.Equal(t, []string{"0.05000000", "0.01000000"}[i], amount)
			}
			require.False(t, rows.Next())
			require.NoError(t, rows.Err())
		})
	}
}
func TestCheckinRepositoryBalanceFailureRollback(t *testing.T) {
	_, s, _ := checkinRepoFixture(t)
	id := checkinRepoUser(t, "fail", "0", "20")
	constraint := fmt.Sprintf("ck_checkin_fail_%d", id)
	_, e := integrationDB.Exec(fmt.Sprintf(`ALTER TABLE users ADD CONSTRAINT %s CHECK(id<>%d OR balance<=0) NOT VALID`, constraint, id))
	require.NoError(t, e)
	t.Cleanup(func() {
		_, e := integrationDB.Exec(`ALTER TABLE users DROP CONSTRAINT ` + constraint)
		require.NoError(t, e)
	})
	_, e = s.Checkin(checkinRepoContext(""), id, "", service.CheckinProof{}, "")
	require.Equal(t, "CHECKIN_FAILED", infraerrors.Reason(e))
	checkinRepoBalance(t, id, "0.00000000", "20.00000000", 0)
}
func TestCheckinRepositoryStatsAndPrecision(t *testing.T) {
	repo, s, settings := checkinRepoFixture(t)
	id := checkinRepoUser(t, "stats", "5", "19.99999999")
	var e error
	for i, rec := range []struct{ date, amount, verify string }{{"2026-10-31", "0.05", "none"}, {"2026-11-22", "0.02", "none"}, {"2026-11-23", "0.03", "email"}, {"2026-11-24", "0.01", "captcha"}, {"2026-11-25", "0.04", "captcha"}} {
		_, e = integrationDB.Exec(`INSERT INTO checkin_records(user_id,checkin_date,amount,user_tier,verification,ip,created_at) VALUES($1,$2,$3,'new_funded',$4,'192.0.2.5',$2::date::timestamptz + interval '9 hours')`, id, rec.date, rec.amount, rec.verify)
		require.NoError(t, e, "record %d", i)
	}
	cfg := strings.Replace(checkinTestConfigJSON, `"newcomer_captcha":false`, `"newcomer_captcha":true`, 1)
	_, e = s.SaveConfig(context.Background(), []byte(cfg))
	require.NoError(t, e)
	for k, v := range map[string]string{service.SettingKeyTurnstileSiteKey: "fixture-site", service.SettingKeyTurnstileSecretKey: "fixture-only"} {
		require.NoError(t, settings.Set(context.Background(), k, v))
		key := k
		t.Cleanup(func() { require.NoError(t, settings.Delete(context.Background(), key)) })
	}
	keys := []string{service.SettingKeyCheckinConfig}
	ctx := context.Background()
	for i, adjustment := range []string{"0", "0.00000001"} {
		if i == 1 {
			var codeID int64
			require.NoError(t, integrationDB.QueryRow(`INSERT INTO redeem_codes(code,type,value,status,used_by) VALUES($1,'admin_balance',$2,'used',$3) RETURNING id`, fmt.Sprintf("ck-credit-%d", id), adjustment, id).Scan(&codeID))
			t.Cleanup(func() {
				_, e := integrationDB.Exec(`DELETE FROM redeem_codes WHERE id=$1`, codeID)
				require.NoError(t, e)
			})
		}
		state, e := repo.LoadState(ctx, id, keys, "2026-11-26", "2026-11")
		require.NoError(t, e)
		require.Equal(t, adjustment, state.Stats.AdminNet.String())
		require.Equal(t, "19.99999999", state.User.TotalRecharged.String())
		stats, e := repo.LoadStats(ctx, id, "2026-11-26")
		require.NoError(t, e)
		require.Equal(t, adjustment, stats.AdminNet.String())
		require.Equal(t, i == 1, stats.AdminHasCredit)
		require.Equal(t, int64(5), stats.TotalDays)
		require.Equal(t, int64(15), stats.TotalCents)
		require.Equal(t, int64(10), stats.MonthCents)
		require.Equal(t, "2026-11-25", stats.PrevDate)
		require.Equal(t, int64(4), stats.RunLen)
		require.Equal(t, "2026-11-23", stats.LastEmailDate)
		require.Equal(t, int64(2), stats.SinceEmail)
		require.Nil(t, stats.TodayCents)
		require.False(t, stats.LaterExists)
		output, e := s.GetState(ctx, id, "")
		require.NoError(t, e)
		require.Equal(t, "available", output.Status)
		require.Equal(t, i == 0, output.NeedsCaptcha)
		require.Equal(t, 4, len(output.Records))
		require.Equal(t, 0.10, output.MonthAmount)
	}
}
func TestCheckinRepositoryLaterDateAndEmptyPage(t *testing.T) {
	repo, s, _ := checkinRepoFixture(t)
	id := checkinRepoUser(t, "later", "0", "20")
	day := timezone.StartOfDay(checkinRepositoryNow)
	later := day.AddDate(0, 0, 1)
	_, e := integrationDB.Exec(`INSERT INTO checkin_records(user_id,checkin_date,amount,user_tier,verification,ip,created_at) VALUES($1,$2,0.23,'veteran','none','192.0.2.10',$3)`, id, later.Format("2006-01-02"), later)
	require.NoError(t, e)
	state, e := s.GetState(context.Background(), id, "")
	require.NoError(t, e)
	require.Equal(t, "done", state.Status)
	require.Nil(t, state.TodayAmount)
	require.Zero(t, state.SecondsUntilReset)
	filter := service.CheckinRecordFilter{Search: fmt.Sprint(id)}
	items, total, e := repo.ListAdminRecords(context.Background(), filter, pagination.PaginationParams{Page: 1, PageSize: 1})
	require.NoError(t, e)
	require.Len(t, items, 1)
	require.Equal(t, int64(1), total)
	items, total, e = repo.ListAdminRecords(context.Background(), filter, pagination.PaginationParams{Page: 40, PageSize: 1})
	require.NoError(t, e)
	require.Empty(t, items)
	require.Equal(t, int64(1), total)
	ipFilter := service.CheckinIPFilter{MinUsers: 1}
	ipItems, ipTotal, e := repo.ListAdminIPs(context.Background(), ipFilter, pagination.PaginationParams{Page: 40, PageSize: 1})
	require.NoError(t, e)
	require.Empty(t, ipItems)
	require.GreaterOrEqual(t, ipTotal, int64(1))
	cfg, e := s.GetConfig(context.Background())
	require.NoError(t, e)
	userItems, userTotal, e := repo.ListAdminUsers(context.Background(), cfg.Config, service.CheckinUserFilter{Search: fmt.Sprint(id)}, pagination.PaginationParams{Page: 40, PageSize: 1})
	require.NoError(t, e)
	require.Empty(t, userItems)
	require.Equal(t, int64(1), userTotal)
}

func TestCheckinRepositoryAdminQueries(t *testing.T) {
	repo, s, _ := checkinRepoFixture(t)
	a := checkinRepoUser(t, "overview-a", "0", "20")
	b := checkinRepoUser(t, "overview-b", "0", "10")
	c := checkinRepoUser(t, "overview-c", "0", "0")
	rows := []struct {
		id                                  int64
		day, amount, tier, verification, ip string
	}{
		{a, "2026-11-26", "0.23", "veteran", "none", "192.0.2.10"},
		{b, "2026-11-26", "0.05", "new_funded", "captcha", "192.0.2.10"},
		{a, "2026-11-25", "0.30", "veteran", "none", "192.0.2.10"},
		{c, "2026-11-25", "0.01", "new_unfunded", "email", "192.0.2.11"},
		{a, "2026-10-01", "0.10", "veteran", "none", "192.0.2.10"},
	}
	for _, r := range rows {
		_, e := integrationDB.Exec(`INSERT INTO checkin_records(user_id,checkin_date,amount,user_tier,verification,ip,created_at) VALUES($1,$2::date,$3::decimal,$4,$5,$6,$2::date::timestamptz)`, r.id, r.day, r.amount, r.tier, r.verification, r.ip)
		require.NoError(t, e)
	}
	ctx := context.Background()
	overview, e := repo.Overview(ctx, "2026-11-26", 7)
	require.NoError(t, e)
	require.Equal(t, int64(2), overview.Today.Users)
	require.InDelta(t, 0.28, overview.Today.Amount, 1e-9)
	require.Equal(t, int64(1), overview.Today.Tiers["veteran"].Users)
	require.InDelta(t, 0.23, overview.Today.Tiers["veteran"].Amount, 1e-9)
	require.Equal(t, int64(2), overview.Yesterday.Users)
	require.InDelta(t, 0.31, overview.Yesterday.Amount, 1e-9)
	require.InDelta(t, 0.69, overview.AllTime.Amount, 1e-9)
	require.Equal(t, int64(3), overview.AllTime.Users)
	require.Equal(t, int64(1), overview.EmailVerifications)
	require.Len(t, overview.Trend, 7)
	require.Equal(t, "2026-11-26", overview.Trend[6].Date)
	require.InDelta(t, 0.28, overview.Trend[6].Amount, 1e-9)
	p := pagination.PaginationParams{Page: 1, PageSize: 20, SortBy: "total_amount", SortOrder: "desc"}
	cfg, e := s.GetConfig(ctx)
	require.NoError(t, e)
	users, total, e := repo.ListAdminUsers(ctx, cfg.Config, service.CheckinUserFilter{}, p)
	require.NoError(t, e)
	require.Equal(t, int64(3), total)
	require.Len(t, users, 3)
	require.Equal(t, a, users[0].UserID)
	require.Equal(t, "veteran", users[0].Tier)
	require.Equal(t, int64(1), users[0].SharedIPUsers)
	require.Equal(t, int64(3), users[0].TotalDays)
	users, total, e = repo.ListAdminUsers(ctx, cfg.Config, service.CheckinUserFilter{Tier: "new_funded"}, p)
	require.NoError(t, e)
	require.Equal(t, int64(1), total)
	require.Len(t, users, 1)
	require.Equal(t, b, users[0].UserID)
	require.Equal(t, "10.00000000", users[0].R.String())
	records, total, e := repo.ListAdminRecords(ctx, service.CheckinRecordFilter{DateFrom: "2026-11-25", DateTo: "2026-11-25", Verification: "email"}, p)
	require.NoError(t, e)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, c, records[0].UserID)
	ips, total, e := repo.ListAdminIPs(ctx, service.CheckinIPFilter{MinUsers: 2}, p)
	require.NoError(t, e)
	require.Equal(t, int64(1), total)
	require.Len(t, ips, 1)
	require.Equal(t, "192.0.2.10", ips[0].IP)
	require.Equal(t, int64(2), ips[0].Users)
	require.Equal(t, int64(4), ips[0].TotalDays)
	require.Len(t, ips[0].Emails, 2)
}

func TestCheckinRepositoryExactRecordDrilldownAndUnknownIP(t *testing.T) {
	repo, _, _ := checkinRepoFixture(t)
	a := checkinRepoUser(t, "exact-a", "0", "20")
	b := checkinRepoUser(t, "exact-b", "0", "20")
	var email string
	require.NoError(t, integrationDB.QueryRow("SELECT email FROM users WHERE id=$1", a).Scan(&email))
	_, err := integrationDB.Exec("UPDATE users SET email=$1 WHERE id=$2", "other-"+email, b)
	require.NoError(t, err)
	for _, id := range []int64{a, b} {
		for _, record := range []struct{ date, ip string }{{"2026-11-25", "192.0.2.30"}, {"2026-11-26", ""}} {
			_, err = integrationDB.Exec("INSERT INTO checkin_records(user_id,checkin_date,amount,user_tier,verification,ip) VALUES($1,$2,0.01,'veteran','none',$3)", id, record.date, record.ip)
			require.NoError(t, err)
		}
	}
	ctx := context.Background()
	page := pagination.PaginationParams{Page: 1, PageSize: 20}
	records, total, err := repo.ListAdminRecords(ctx, service.CheckinRecordFilter{Search: "#" + fmt.Sprint(a)}, page)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, records, 2)
	for _, item := range records {
		require.Equal(t, a, item.UserID)
	}
	// Ordinary email text remains a fuzzy search.
	records, total, err = repo.ListAdminRecords(ctx, service.CheckinRecordFilter{Search: email}, page)
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	require.Len(t, records, 4)
	ips, total, err := repo.ListAdminIPs(ctx, service.CheckinIPFilter{MinUsers: 2}, page)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, ips, 1)
	require.Equal(t, "192.0.2.30", ips[0].IP)
	require.Equal(t, int64(2), ips[0].Users)
}
