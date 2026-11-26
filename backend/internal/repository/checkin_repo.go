package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type checkinRepository struct{ client *dbent.Client }

func NewCheckinRepository(client *dbent.Client) service.CheckinRepository {
	return &checkinRepository{client: client}
}

func (r *checkinRepository) WithTx(ctx context.Context, fn func(context.Context) error) error {
	tx, e := r.client.Tx(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	if e = fn(dbent.NewTxContext(ctx, tx)); e != nil {
		return e
	}
	return tx.Commit()
}

const checkinSettingsSQL = `COALESCE((SELECT json_object_agg(key,value)::text FROM settings WHERE key=ANY($3::text[])), '{}')`
const checkinStatsCTE = `WITH admin_adjustments AS (
    SELECT COALESCE(SUM(value),0)::text AS admin_net, COALESCE(BOOL_OR(value>0),false) AS admin_has_credit
    FROM redeem_codes WHERE used_by=$1 AND type='admin_balance'
), records AS (
    SELECT checkin_date,amount,verification,created_at FROM checkin_records WHERE user_id=$1
), summary AS (
    SELECT COUNT(*) AS total_days, (COALESCE(SUM(amount),0)*100)::bigint AS total_cents,
           (MAX(amount) FILTER(WHERE checkin_date=$2::date)*100)::bigint AS today_cents,
           COALESCE(BOOL_OR(checkin_date>$2::date),false) AS later_exists,
           (COALESCE(SUM(amount) FILTER(WHERE checkin_date>=date_trunc('month',$2::date)::date AND checkin_date<(date_trunc('month',$2::date)+interval '1 month')::date),0)*100)::bigint AS month_cents,
           MAX(checkin_date) FILTER(WHERE checkin_date<=$2::date) AS prev_date,
           MAX(checkin_date) FILTER(WHERE verification='email') AS last_email_date
    FROM records
), dated_groups AS (
    SELECT checkin_date,checkin_date-(ROW_NUMBER() OVER(ORDER BY checkin_date))::int AS grp
    FROM records WHERE checkin_date<=$2::date
)`
const checkinStatsColumns = `a.admin_net,a.admin_has_credit,s.total_days,s.total_cents,s.today_cents,s.later_exists,s.month_cents,
    COALESCE(to_char(s.prev_date,'YYYY-MM-DD'),''),COALESCE(to_char(s.last_email_date,'YYYY-MM-DD'),''),
    (SELECT COUNT(*) FROM records WHERE checkin_date>s.last_email_date),
    (SELECT COUNT(*) FROM dated_groups WHERE grp=(SELECT grp FROM dated_groups ORDER BY checkin_date DESC LIMIT 1))`
const checkinMonthAggregate = `COALESCE((SUM(amount)*100)::bigint,0), COALESCE(jsonb_agg(jsonb_build_object(
    'date',to_char(checkin_date,'YYYY-MM-DD'),'amount_cents',(amount*100)::bigint,'created_at',created_at) ORDER BY checkin_date),'[]'::jsonb)::text`

func scanCheckinMonth(cents int64, raw string) (service.CheckinMonthData, error) {
	var values []struct {
		Date        string    `json:"date"`
		AmountCents int64     `json:"amount_cents"`
		CreatedAt   time.Time `json:"created_at"`
	}
	if e := json.Unmarshal([]byte(raw), &values); e != nil {
		return service.CheckinMonthData{}, e
	}
	data := service.CheckinMonthData{Cents: cents, Records: make([]service.CheckinRecord, 0, len(values))}
	for _, v := range values {
		data.Records = append(data.Records, service.CheckinRecord{Date: v.Date, AmountCents: v.AmountCents, CreatedAt: v.CreatedAt})
	}
	return data, nil
}

func checkinStatDestinations(s *service.CheckinStats, today *sql.NullInt64) []any {
	return []any{&s.AdminNet, &s.AdminHasCredit, &s.TotalDays, &s.TotalCents, today, &s.LaterExists, &s.MonthCents, &s.PrevDate, &s.LastEmailDate, &s.SinceEmail, &s.RunLen}
}
func scanCheckinFirst(rows *sql.Rows, dest ...any) error {
	defer rows.Close()
	if !rows.Next() {
		if e := rows.Err(); e != nil {
			return e
		}
		return sql.ErrNoRows
	}
	if e := rows.Scan(dest...); e != nil {
		return e
	}
	return rows.Err()
}

func (r *checkinRepository) LoadState(ctx context.Context, id int64, keys []string, date, month string) (service.CheckinState, error) {
	// The CTE reads all statistics, one settings object and optional rendered month
	// in one database snapshot and one SQL round trip.
	query := checkinStatsCTE + `, requested_month(cents,month_records) AS (
        SELECT ` + checkinMonthAggregate + ` FROM records
        WHERE $4<>'' AND checkin_date>=(NULLIF($4,'')||'-01')::date AND checkin_date<((NULLIF($4,'')||'-01')::date+interval '1 month')::date
    ) SELECT ` + checkinSettingsSQL + `,u.email,u.status,u.total_recharged::text,u.created_at,` + checkinStatsColumns + `,m.cents,m.month_records
    FROM users u CROSS JOIN admin_adjustments a CROSS JOIN summary s CROSS JOIN requested_month m WHERE u.id=$1 AND u.deleted_at IS NULL`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, id, date, pq.Array(keys), month)
	if e != nil {
		return service.CheckinState{}, e
	}
	var state service.CheckinState
	var settings, rawMonth string
	var today sql.NullInt64
	var cents int64
	dest := []any{&settings, &state.User.Email, &state.User.Status, &state.User.TotalRecharged, &state.User.CreatedAt}
	dest = append(dest, checkinStatDestinations(&state.Stats, &today)...)
	dest = append(dest, &cents, &rawMonth)
	if e = scanCheckinFirst(rows, dest...); e != nil {
		return service.CheckinState{}, e
	}
	if e = json.Unmarshal([]byte(settings), &state.User.Settings); e != nil {
		return service.CheckinState{}, e
	}
	if today.Valid {
		state.Stats.TodayCents = &today.Int64
	}
	state.Month, e = scanCheckinMonth(cents, rawMonth)
	return state, e
}
func (r *checkinRepository) LockUser(ctx context.Context, id int64, keys []string) (service.CheckinUser, error) {
	query := `SELECT COALESCE((SELECT json_object_agg(key,value)::text FROM settings WHERE key=ANY($2::text[])),'{}'),u.email,u.status,u.total_recharged::text,u.created_at FROM users u WHERE u.id=$1 AND u.deleted_at IS NULL FOR UPDATE OF u`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, id, pq.Array(keys))
	if e != nil {
		return service.CheckinUser{}, e
	}
	var user service.CheckinUser
	var raw string
	if e = scanCheckinFirst(rows, &raw, &user.Email, &user.Status, &user.TotalRecharged, &user.CreatedAt); e != nil {
		return service.CheckinUser{}, e
	}
	e = json.Unmarshal([]byte(raw), &user.Settings)
	return user, e
}
func (r *checkinRepository) LoadStats(ctx context.Context, id int64, date string) (service.CheckinStats, error) {
	query := checkinStatsCTE + ` SELECT ` + checkinStatsColumns + ` FROM admin_adjustments a CROSS JOIN summary s`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, id, date)
	if e != nil {
		return service.CheckinStats{}, e
	}
	var stats service.CheckinStats
	var today sql.NullInt64
	if e = scanCheckinFirst(rows, checkinStatDestinations(&stats, &today)...); e != nil {
		return service.CheckinStats{}, e
	}
	if today.Valid {
		stats.TodayCents = &today.Int64
	}
	return stats, nil
}
func (r *checkinRepository) LockIP(ctx context.Context, date, ip string) error {
	_, e := clientFromContext(ctx, r.client).ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('checkin-ip:' || $1::text || ':' || $2::text,0))`, date, ip)
	return e
}
func (r *checkinRepository) Commit(ctx context.Context, record service.CheckinRecord, limit int) (service.CheckinCommitResult, error) {
	var ipLimit any
	if limit > 0 {
		ipLimit = limit
	}
	query := `WITH cnt AS (
        SELECT CASE WHEN $8::int IS NULL THEN 0 ELSE (SELECT COUNT(DISTINCT user_id) FROM checkin_records WHERE checkin_date=$2::date AND ip=$6 AND user_tier<>'veteran' AND user_id<>$1) END AS n
    ), ins AS (
        INSERT INTO checkin_records(user_id,checkin_date,amount,user_tier,verification,ip,created_at)
        SELECT $1,$2::date,$3::decimal,$4,$5,$6,$7 FROM cnt WHERE $8::int IS NULL OR n<$8::int
        ON CONFLICT(user_id,checkin_date) DO NOTHING RETURNING id,created_at
    ), upd AS (
        UPDATE users SET balance=balance+$3::decimal,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND EXISTS(SELECT 1 FROM ins) RETURNING balance
    ) SELECT cnt.n,ins.id,ins.created_at,upd.balance::text FROM cnt LEFT JOIN ins ON true LEFT JOIN upd ON true`
	amount := decimal.New(record.AmountCents, -2).StringFixed(2)
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, record.UserID, record.Date, amount, record.Tier, record.Verification, record.IP, record.CreatedAt, ipLimit)
	if e != nil {
		return service.CheckinCommitResult{}, e
	}
	var result service.CheckinCommitResult
	var id sql.NullInt64
	var at sql.NullTime
	var balance sql.NullString
	if e = scanCheckinFirst(rows, &result.IPCount, &id, &at, &balance); e != nil {
		return service.CheckinCommitResult{}, e
	}
	if id.Valid {
		result.ID = id.Int64
	}
	if at.Valid {
		result.CreatedAt = at.Time
	}
	if balance.Valid {
		n, e := decimal.NewFromString(balance.String)
		if e != nil {
			return service.CheckinCommitResult{}, e
		}
		result.Balance = &n
	}
	return result, nil
}
func (r *checkinRepository) ListMonth(ctx context.Context, id int64, month string) (service.CheckinMonthData, error) {
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT `+checkinMonthAggregate+` FROM checkin_records WHERE user_id=$1 AND checkin_date>=($2||'-01')::date AND checkin_date<(($2||'-01')::date+interval '1 month')::date`, id, month)
	if e != nil {
		return service.CheckinMonthData{}, e
	}
	var cents int64
	var raw string
	if e = scanCheckinFirst(rows, &cents, &raw); e != nil {
		return service.CheckinMonthData{}, e
	}
	return scanCheckinMonth(cents, raw)
}

func (r *checkinRepository) Overview(ctx context.Context, date string, days int) (service.CheckinOverview, error) {
	query := `WITH periods(label,first_date,last_date) AS (VALUES
       ('today',$1::date,$1::date),('yesterday',$1::date-1,$1::date-1),('last_7_days',$1::date-6,$1::date),('last_30_days',$1::date-29,$1::date),('all_time',NULL::date,NULL::date)
    ), period_summary AS (
       SELECT p.label,jsonb_build_object('amount',COALESCE(SUM(r.amount),0),'users',COUNT(DISTINCT r.user_id),'tiers',jsonb_build_object(
           'veteran',jsonb_build_object('amount',COALESCE(SUM(r.amount) FILTER(WHERE r.user_tier='veteran'),0),'users',COUNT(DISTINCT r.user_id) FILTER(WHERE r.user_tier='veteran')),
           'new_funded',jsonb_build_object('amount',COALESCE(SUM(r.amount) FILTER(WHERE r.user_tier='new_funded'),0),'users',COUNT(DISTINCT r.user_id) FILTER(WHERE r.user_tier='new_funded')),
           'new_unfunded',jsonb_build_object('amount',COALESCE(SUM(r.amount) FILTER(WHERE r.user_tier='new_unfunded'),0),'users',COUNT(DISTINCT r.user_id) FILTER(WHERE r.user_tier='new_unfunded')))) AS data
       FROM periods p LEFT JOIN checkin_records r ON (p.first_date IS NULL OR r.checkin_date BETWEEN p.first_date AND p.last_date) GROUP BY p.label
    ), trend AS (
       SELECT to_char(d.day,'YYYY-MM-DD') AS date,COALESCE(SUM(r.amount),0) AS amount,COUNT(DISTINCT r.user_id) AS users
       FROM generate_series(($1::date-($2::int-1))::timestamp,$1::date::timestamp,interval '1 day') d(day)
       LEFT JOIN checkin_records r ON r.checkin_date=d.day::date GROUP BY d.day ORDER BY d.day
    ) SELECT ((SELECT jsonb_object_agg(label,data) FROM period_summary) || jsonb_build_object(
       'email_verifications',(SELECT COUNT(*) FROM checkin_records WHERE verification='email'),
       'trend',(SELECT COALESCE(jsonb_agg(to_jsonb(trend) ORDER BY date),'[]'::jsonb) FROM trend)))::text`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, date, days)
	if e != nil {
		return service.CheckinOverview{}, e
	}
	var raw string
	if e = scanCheckinFirst(rows, &raw); e != nil {
		return service.CheckinOverview{}, e
	}
	var result service.CheckinOverview
	e = json.Unmarshal([]byte(raw), &result)
	return result, e
}

// A lateral page always returns the total row, including pages beyond the end.
func scanCheckinPage[T any](rows *sql.Rows) ([]T, int64, error) {
	defer rows.Close()
	items := make([]T, 0)
	var total int64
	for rows.Next() {
		var raw sql.NullString
		if e := rows.Scan(&total, &raw); e != nil {
			return nil, 0, e
		}
		if raw.Valid {
			var item T
			if e := json.Unmarshal([]byte(raw.String), &item); e != nil {
				return nil, 0, e
			}
			items = append(items, item)
		}
	}
	return items, total, rows.Err()
}
func (r *checkinRepository) ListAdminRecords(ctx context.Context, f service.CheckinRecordFilter, p pagination.PaginationParams) ([]service.CheckinAdminRecord, int64, error) {
	query := `WITH base AS (
       SELECT r.id,r.user_id,u.email,r.amount,to_char(r.checkin_date,'YYYY-MM-DD') AS date,
              r.created_at,r.user_tier AS tier,r.ip,r.verification
       FROM checkin_records r JOIN users u ON u.id=r.user_id
       WHERE (NULLIF($1,'') IS NULL OR r.checkin_date>=NULLIF($1,'')::date)
         AND (NULLIF($2,'') IS NULL OR r.checkin_date<=NULLIF($2,'')::date)
         AND ($3='' OR CASE WHEN $3 ~ '^#[0-9]+$' THEN r.user_id::text=substring($3 from 2) ELSE u.email ILIKE '%'||$3||'%' OR u.username ILIKE '%'||$3||'%' OR r.user_id::text=$3 END)
         AND ($4='' OR r.ip=$4) AND ($5='' OR r.user_tier=$5) AND ($6='' OR r.verification=$6)
    ), total AS(SELECT COUNT(*) AS n FROM base)
    SELECT total.n,CASE WHEN page.id IS NULL THEN NULL ELSE to_jsonb(page)::text END FROM total LEFT JOIN LATERAL(SELECT * FROM base ORDER BY date DESC,created_at DESC,id DESC LIMIT $7 OFFSET $8) page ON true ORDER BY page.date DESC,page.created_at DESC,page.id DESC`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, f.DateFrom, f.DateTo, f.Search, f.IP, f.Tier, f.Verification, p.Limit(), p.Offset())
	if e != nil {
		return nil, 0, e
	}
	return scanCheckinPage[service.CheckinAdminRecord](rows)
}
func (r *checkinRepository) ListAdminIPs(ctx context.Context, f service.CheckinIPFilter, p pagination.PaginationParams) ([]service.CheckinAdminIP, int64, error) {
	query := `WITH matching AS (
       SELECT r.ip,r.user_id,r.checkin_date,r.amount,u.email FROM checkin_records r JOIN users u ON u.id=r.user_id
       WHERE r.ip<>'' AND (NULLIF($1,'') IS NULL OR r.checkin_date>=NULLIF($1,'')::date) AND (NULLIF($2,'') IS NULL OR r.checkin_date<=NULLIF($2,'')::date)
    ), base AS (
       SELECT ip, COUNT(DISTINCT user_id) AS users,COUNT(*) AS total_days,SUM(amount) AS total_amount,
              to_char(MIN(checkin_date),'YYYY-MM-DD') AS first_date,to_char(MAX(checkin_date),'YYYY-MM-DD') AS last_date,
              (ARRAY_AGG(DISTINCT email ORDER BY email))[1:5] AS emails
       FROM matching GROUP BY ip HAVING COUNT(DISTINCT user_id)>=$3
    ), total AS(SELECT COUNT(*) AS n FROM base)
    SELECT total.n,CASE WHEN page.ip IS NULL THEN NULL ELSE to_jsonb(page)::text END FROM total LEFT JOIN LATERAL(SELECT * FROM base ORDER BY users DESC,total_days DESC,ip LIMIT $4 OFFSET $5) page ON true ORDER BY page.users DESC,page.total_days DESC,page.ip`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, f.DateFrom, f.DateTo, f.MinUsers, p.Limit(), p.Offset())
	if e != nil {
		return nil, 0, e
	}
	return scanCheckinPage[service.CheckinAdminIP](rows)
}
func (r *checkinRepository) ListAdminUsers(ctx context.Context, c service.CheckinConfig, f service.CheckinUserFilter, p pagination.PaginationParams) ([]service.CheckinAdminUser, int64, error) {
	sortBy := p.SortBy
	switch sortBy {
	case "total_amount", "total_days", "last_checkin", "shared_ip_users":
	default:
		sortBy = "total_amount"
	}
	order := pagination.NormalizeSortOrder(p.SortOrder, pagination.SortOrderDesc)
	// Only fixed field names and a normalized sort direction enter the SQL text.
	query := `WITH history AS (
       SELECT user_id,COUNT(*) AS total_days,SUM(amount) AS total_amount,MIN(checkin_date) AS first_date,MAX(checkin_date) AS last_date,
              (ARRAY_AGG(ip ORDER BY checkin_date DESC,id DESC))[1] AS last_ip,
              COUNT(*) FILTER(WHERE verification='email') AS email_verifications,
              MAX(checkin_date) FILTER(WHERE verification='email') AS last_email_date
       FROM checkin_records GROUP BY user_id
    ), admin AS (
       SELECT used_by,COALESCE(SUM(value),0) AS admin_net,COALESCE(BOOL_OR(value>0),false) AS admin_has_credit
       FROM redeem_codes WHERE type='admin_balance' AND used_by IS NOT NULL GROUP BY used_by
    ), ip_counts AS (
       SELECT ip,COUNT(DISTINCT user_id) AS users FROM checkin_records WHERE ip<>'' GROUP BY ip
    ), shared AS (
       SELECT r.user_id,MAX(i.users-1) AS shared_ip_users FROM checkin_records r JOIN ip_counts i ON i.ip=r.ip GROUP BY r.user_id
    ), classified AS (
       SELECT h.user_id,u.email,u.username,u.status,u.total_recharged+COALESCE(a.admin_net,0) AS r,
              CASE WHEN u.total_recharged+COALESCE(a.admin_net,0)>=$1::decimal THEN 'veteran'
                   WHEN u.total_recharged>0 OR COALESCE(a.admin_has_credit,false) THEN 'new_funded' ELSE 'new_unfunded' END AS tier,
              h.total_days,h.total_amount,to_char(h.first_date,'YYYY-MM-DD') AS first_checkin,to_char(h.last_date,'YYYY-MM-DD') AS last_checkin,
              h.last_ip,COALESCE(s.shared_ip_users,0) AS shared_ip_users,h.email_verifications,h.last_email_date
       FROM history h JOIN users u ON u.id=h.user_id LEFT JOIN admin a ON a.used_by=h.user_id LEFT JOIN shared s ON s.user_id=h.user_id
       WHERE ($2='' OR u.email ILIKE '%'||$2||'%' OR u.username ILIKE '%'||$2||'%' OR h.user_id::text=$2)
    ), base AS (
       SELECT user_id,email,username,status,r,tier,total_days,total_amount,first_checkin,last_checkin,last_ip,shared_ip_users,email_verifications,
              (tier='new_unfunded' AND $4::boolean AND ((last_email_date IS NULL AND total_days>=$5) OR (last_email_date IS NOT NULL AND $6::int>0 AND (SELECT COUNT(*) FROM checkin_records cr WHERE cr.user_id=classified.user_id AND cr.checkin_date>classified.last_email_date)>=$6))) AS pending_email
       FROM classified WHERE ($3='' OR tier=$3)
    ), total AS(SELECT COUNT(*) AS n FROM base)
    SELECT total.n,CASE WHEN page.user_id IS NULL THEN NULL ELSE to_jsonb(page)::text END FROM total LEFT JOIN LATERAL(SELECT * FROM base ORDER BY ` + sortBy + ` ` + order + `,user_id ASC LIMIT $7 OFFSET $8) page ON true ORDER BY page.` + sortBy + ` ` + order + `,page.user_id ASC`
	rows, e := clientFromContext(ctx, r.client).QueryContext(ctx, query, c.VeteranThreshold.String(), f.Search, f.Tier, c.EmailVerifyEnabled, c.EmailVerifyFirst, c.EmailVerifyEvery, p.Limit(), p.Offset())
	if e != nil {
		return nil, 0, fmt.Errorf("list checkin users: %w", e)
	}
	return scanCheckinPage[service.CheckinAdminUser](rows)
}
