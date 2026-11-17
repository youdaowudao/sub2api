package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const opsRecordsRequestPredicate = "(COALESCE(e.status_code,0)>=400 OR e.error_type='cyber_policy')"
const opsRecordsUpstreamPredicate = "(LOWER(COALESCE(e.error_owner,''))='provider' AND e.error_phase IN ('upstream','account_auth'))"

func opsErrorFinancePredicateSQL() string {
	phrases := []string{"insufficient balance", "insufficient account balance", "账户余额不足", "api key 额度已用完", "api key 5小时限额已用完", "api key 日限额已用完", "api key 7天限额已用完", "daily usage limit exceeded", "weekly usage limit exceeded", "monthly usage limit exceeded", "usage quota exhausted for this platform"}
	checks := make([]string, 0, len(phrases))
	for _, phrase := range phrases {
		checks = append(checks, "COALESCE(e.error_message,'') ILIKE '%"+phrase+"%'")
	}
	return "(LOWER(COALESCE(e.error_owner,''))='client' AND e.error_phase IN ('request','auth') AND e.upstream_status_code IS NULL AND (e.upstream_errors IS NULL OR e.upstream_errors='[]'::jsonb) AND (" + strings.Join(checks, " OR ") + "))"
}

func opsErrorMessageKeySQL() string {
	// Escape literal marker introducers first, so original <id>/<time> text
	// cannot collide with metadata placeholders. Only the internal key is escaped.
	base := `m.message_base`
	lower := `m.folded_message`
	boundary := `(?="?(?:[[:space:],;}\])]|$))`
	prefix := func(label string) string {
		return `(?<=(?:^|[^[:alnum:]_-])"?(?:` + label + `)"?[[:space:]]*[:=][[:space:]]*"?)`
	}
	replace := func(input, pattern, marker string) string {
		return `regexp_replace(` + input + `,'` + pattern + `','` + marker + `','gi')`
	}
	ids := prefix(`request[_-]id|client[_-]request[_-]id|trace[_-]id`) + `[a-zA-Z0-9_-]{4,128}` + boundary
	times := prefix(`timestamp|request_time|created_at`) + `[0-9]{4}-(?:0[1-9]|1[0-2])-(?:0[1-9]|[12][0-9]|3[01])T(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]+)?(?:Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])` + boundary
	general := replace(replace(base, ids, `<id>`), times, `<time>`)
	request := replace(base, prefix(`request[_-]id`)+`[a-zA-Z0-9_-]{4,128}`+boundary, `<id>`)
	trace := replace(base, prefix(`trace[_-]id`)+`[a-zA-Z0-9_-]{4,128}`+boundary, `<id>`)
	// A single bare metadata value at the end can be validated with length
	// and character checks. This avoids PostgreSQL's costly bounded-regex
	// replacement on every row. All other forms use the strict expression above.
	raw := `m.raw_message`
	folded := `m.folded_message`
	fastSuffix := func(label, other, fallback string) string {
		pos := `strpos(` + folded + `,'` + label + `=')`
		size := itoa(len(label))
		value := `substr(` + raw + `,` + pos + `+` + itoa(len(label)+1) + `)`
		guard := pos + `>0 AND length(` + folded + `)-length(replace(` + folded + `,'` + label + `',''))=` + size +
			` AND strpos(` + folded + `,'` + other + `')=0 AND (` + pos + `=1 OR substr(` + raw + `,` + pos + `-1,1)!~'[[:alnum:]_-]')` +
			` AND length(` + value + `) BETWEEN 4 AND 128 AND ` + value + `!~'[^a-zA-Z0-9_-]'`
		// The value ends at EOF, so only the original prefix needs escaping.
		prefix := `replace(ltrim(replace(left(` + raw + `,` + pos + `+` + size + `), E'\r\n', E'\n')),'<','<<')`
		return `CASE WHEN ` + guard + ` THEN ` + prefix + `||'<id>' ELSE ` + fallback + ` END`
	}
	request = fastSuffix("request-id", "request_id", fastSuffix("request_id", "request-id", request))
	trace = fastSuffix("trace-id", "trace_id", fastSuffix("trace_id", "trace-id", trace))

	return `CASE
 WHEN strpos(` + lower + `,'timestamp')>0 OR strpos(` + lower + `,'request_time')>0 OR strpos(` + lower + `,'created_at')>0 OR strpos(` + lower + `,'client_request')>0 OR strpos(` + lower + `,'client-request')>0 THEN ` + general + `
 WHEN strpos(` + lower + `,'request')>0 AND strpos(` + lower + `,'trace')>0 THEN ` + general + `
 WHEN strpos(` + lower + `,'request')>0 THEN ` + request + `
 WHEN strpos(` + lower + `,'trace')>0 THEN ` + trace + `
 ELSE ` + base + ` END`
}

func opsErrorGroupKeySQL() string {
	message := opsErrorMessageKeySQL()
	upstream := opsRecordsUpstreamPredicate
	return `ROW(e.platform,e.group_id,e.account_id,
 COALESCE(NULLIF(btrim(e.requested_model),''),NULLIF(btrim(e.model),''),''),
 COALESCE(NULLIF(btrim(e.upstream_model),''),NULLIF(btrim(e.model),''),''),
 COALESCE(NULLIF(e.inbound_endpoint,''),e.request_path,''),COALESCE(e.upstream_endpoint,''),
 e.request_type,e.stream,e.error_phase,e.error_type,e.error_owner,e.error_source,e.severity,
 COALESCE(e.upstream_status_code,e.status_code,0),` + opsRecordsRequestPredicate + `,` + upstream + `,` + message + `,
 CASE WHEN NOT ` + upstream + ` THEN e.user_id END,
 CASE WHEN NOT ` + upstream + ` THEN e.api_key_id END,
 CASE WHEN (` + upstream + ` AND e.account_id IS NULL) OR (NOT ` + upstream + ` AND e.user_id IS NULL) OR btrim(COALESCE(e.error_message,''))='' THEN e.id END)::text`
}

func buildOpsErrorRecordsWhere(filter *service.OpsErrorRecordsFilter) (string, []any) {
	f := filter.OpsErrorLogFilter
	if f.View == "" {
		f.View = "all"
	}
	f.Query = escapeLikePattern(strings.TrimSpace(f.Query))
	predicate := "(" + opsRecordsRequestPredicate + " OR " + opsRecordsUpstreamPredicate + ")"
	switch filter.Scope {
	case "request":
		predicate = opsRecordsRequestPredicate
	case "upstream":
		predicate = opsRecordsUpstreamPredicate
	}
	clauses, args := appendOpsErrorCommonWhere([]string{predicate}, nil, &f)
	clauses = appendOpsErrorViewWhere(clauses, f.View)
	switch filter.Finance {
	case "include":
	case "only":
		clauses = append(clauses, opsErrorFinancePredicateSQL())
	default:
		clauses = append(clauses, "NOT "+opsErrorFinancePredicateSQL())
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (r *opsRepository) ListErrorRecords(ctx context.Context, filter *service.OpsErrorRecordsFilter) (*service.OpsErrorRecordsList, error) {
	return r.listErrorRecords(ctx, filter, 0)
}
func (r *opsRepository) ListErrorRecordMembers(ctx context.Context, filter *service.OpsErrorRecordsFilter, id int64) (*service.OpsErrorRecordsList, error) {
	return r.listErrorRecords(ctx, filter, id)
}

func (r *opsRepository) listErrorRecords(ctx context.Context, filter *service.OpsErrorRecordsFilter, memberID int64) (*service.OpsErrorRecordsList, error) {
	if r == nil || r.db == nil || filter == nil {
		return nil, fmt.Errorf("error records repository unavailable")
	}
	query, args, page, size := buildOpsErrorRecordsQuery(filter, memberID)
	result := &service.OpsErrorRecordsList{Page: page, PageSize: size, Items: []*service.OpsErrorRecord{}}
	var payload []byte
	var anchorExists bool
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&result.Total, &result.TotalRecords, &payload, &anchorExists); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if !anchorExists {
		return nil, sql.ErrNoRows
	}
	if err := json.Unmarshal(payload, &result.Items); err != nil {
		return nil, err
	}
	return result, nil
}

func buildOpsErrorRecordsQuery(filter *service.OpsErrorRecordsFilter, memberID int64) (string, []any, int, int) {
	page, size := filter.Page, filter.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	where, args := buildOpsErrorRecordsWhere(filter)
	grouped := filter.Display != "raw" && memberID == 0
	groupKey := "NULL::text"
	messageInput := ""
	if grouped || memberID > 0 {
		groupKey = opsErrorGroupKeySQL()
		// OFFSET 0 keeps these shared projections from being expanded at each
		// reference inside the normalization expression. Each row is still
		// filtered before key calculation; no stored state or extra query.
		messageInput = `CROSS JOIN LATERAL (SELECT COALESCE(e.error_message,'') AS raw_message,
         LOWER(COALESCE(e.error_message,'') COLLATE "C") AS folded_message,
         replace(btrim(replace(COALESCE(e.error_message,''),E'\r\n',E'\n')),'<','<<') AS message_base OFFSET 0) m `
	}
	// Projection deliberately omits large bodies and attempt arrays from the CTE.
	query := `WITH filtered AS MATERIALIZED (
 SELECT e.id,e.created_at,e.user_id,e.api_key_id,host(e.client_ip) AS client_ip,NULLIF(e.user_agent,'') AS user_agent,
 COALESCE(NULLIF(btrim(e.requested_model),''),NULLIF(btrim(e.upstream_model),''),btrim(e.model),'') AS model_sort,
 COALESCE(e.upstream_status_code,e.status_code,0) AS status_sort,
 ` + opsRecordsRequestPredicate + ` AS is_request_error,` + opsRecordsUpstreamPredicate + ` AS is_upstream_error,
 ` + groupKey + ` AS grouping_key
 FROM ops_error_logs e ` + messageInput + where + `), matched AS NOT MATERIALIZED (`
	exists := "true"
	if memberID > 0 {
		args = append(args, memberID)
		anchor := "$" + itoa(len(args))
		query += `SELECT * FROM filtered WHERE grouping_key=(SELECT grouping_key FROM filtered WHERE id=` + anchor + `)`
		exists = `EXISTS(SELECT 1 FROM filtered WHERE id=` + anchor + `)`
	} else {
		query += `SELECT * FROM filtered`
	}
	query += `), summaries AS (`
	if grouped {
		query += `SELECT grouping_key,(array_agg(id ORDER BY created_at DESC,id DESC))[1] AS representative_id,
 COUNT(*) AS count,MIN(created_at) AS first_at,MAX(created_at) AS last_at,
 (array_agg(model_sort ORDER BY created_at DESC,id DESC))[1] AS model_sort,MAX(status_sort) AS status_sort,
 bool_or(is_request_error) AS is_request_error,bool_or(is_upstream_error) AS is_upstream_error
 FROM matched GROUP BY grouping_key`
	} else {
		query += `SELECT id AS representative_id,1::bigint AS count,created_at AS first_at,created_at AS last_at,model_sort,status_sort,is_request_error,is_upstream_error,
 (user_id IS NOT NULL)::int AS user_count,user_id AS single_user_id,(user_id IS NULL) AS has_unknown_user,
 (api_key_id IS NOT NULL)::int AS api_key_count,api_key_id AS single_api_key_id,(api_key_id IS NULL) AS has_unknown_api_key,
 (client_ip IS NOT NULL)::int AS client_ip_count,client_ip AS single_client_ip,(client_ip IS NULL) AS has_unknown_client_ip,
 (user_agent IS NOT NULL)::int AS user_agent_count,user_agent AS single_user_agent,(user_agent IS NULL) AS has_unknown_user_agent FROM matched`
	}
	column := "last_at"
	switch filter.SortBy {
	case "model":
		column = "model_sort"
	case "status_code":
		column = "status_sort"
	}
	direction := "DESC"
	if filter.SortOrder == "asc" {
		direction = "ASC"
	}
	order := column + " " + direction + ", representative_id " + direction
	args = append(args, size, int64(page-1)*int64(size))
	query += `), totals AS (SELECT COUNT(*) AS total,COALESCE(SUM(count),0) AS total_records FROM summaries),
 page_summaries AS (SELECT * FROM summaries ORDER BY ` + order + ` LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args)) + `)`
	if grouped {
		// Identity cardinalities are needed only for visible groups. Joining those
		// keys back to the full filtered CTE preserves exact counts without sorting
		// every matched group for several DISTINCT aggregates before pagination.
		query += `, identities AS (SELECT p.representative_id,
 COUNT(DISTINCT m.user_id) AS user_count,MIN(m.user_id) AS single_user_id,bool_or(m.user_id IS NULL) AS has_unknown_user,
 COUNT(DISTINCT m.api_key_id) AS api_key_count,MIN(m.api_key_id) AS single_api_key_id,bool_or(m.api_key_id IS NULL) AS has_unknown_api_key,
 COUNT(DISTINCT m.client_ip) AS client_ip_count,MIN(m.client_ip) AS single_client_ip,bool_or(m.client_ip IS NULL) AS has_unknown_client_ip,
 COUNT(DISTINCT m.user_agent) AS user_agent_count,MIN(m.user_agent) AS single_user_agent,bool_or(m.user_agent IS NULL) AS has_unknown_user_agent

 FROM matched m JOIN page_summaries p ON m.grouping_key=p.grouping_key GROUP BY p.representative_id),
 page_rows AS (SELECT p.*,i.user_count,i.single_user_id,i.has_unknown_user,i.api_key_count,i.single_api_key_id,i.has_unknown_api_key,
 i.client_ip_count,i.single_client_ip,i.has_unknown_client_ip,i.user_agent_count,i.single_user_agent,i.has_unknown_user_agent
 FROM page_summaries p JOIN identities i USING(representative_id))`
	} else {
		query += `, page_rows AS (SELECT * FROM page_summaries)`
	}
	query += `, items AS (SELECT p.*, `
	projection := opsErrorRecordJSON()
	if grouped {
		query += `jsonb_build_object('representative',` + projection + `)`
	} else {
		query += projection + ` || jsonb_build_object('client_status_code',e.status_code)`
	}
	query += ` || jsonb_build_object('representative_id',p.representative_id,'count',p.count,'first_at',p.first_at,'last_at',p.last_at,
 'is_request_error',p.is_request_error,'is_upstream_error',p.is_upstream_error,
 'user_count',p.user_count,'api_key_count',p.api_key_count,'has_unknown_user',p.has_unknown_user,'has_unknown_api_key',p.has_unknown_api_key,
 'single_user',CASE WHEN p.user_count=1 THEN jsonb_build_object('id',p.single_user_id,'email',COALESCE(su.email,'')) END,
 'single_api_key',CASE WHEN p.api_key_count=1 THEN jsonb_build_object('id',p.single_api_key_id,'name',COALESCE(sak.name,''),'deleted',sak.deleted_at IS NOT NULL) END,
 'single_client_ip',CASE WHEN p.client_ip_count=1 THEN p.single_client_ip END,'client_ip_count',p.client_ip_count,'has_unknown_client_ip',p.has_unknown_client_ip,
 'single_user_agent',CASE WHEN p.user_agent_count=1 THEN p.single_user_agent END,'user_agent_count',p.user_agent_count,'has_unknown_user_agent',p.has_unknown_user_agent
 ) AS item FROM page_rows p JOIN ops_error_logs e ON e.id=p.representative_id
 LEFT JOIN users u ON u.id=e.user_id LEFT JOIN users u2 ON u2.id=e.resolved_by_user_id
 LEFT JOIN api_keys ak ON ak.id=e.api_key_id LEFT JOIN accounts a ON a.id=e.account_id LEFT JOIN groups g ON g.id=e.group_id
 LEFT JOIN users su ON su.id=p.single_user_id LEFT JOIN api_keys sak ON sak.id=p.single_api_key_id
 ) SELECT total,total_records,COALESCE((SELECT jsonb_agg(item ORDER BY ` + order + `) FROM items),'[]'::jsonb),` + exists + ` FROM totals`
	return query, args, page, size
}

func opsErrorRecordJSON() string {
	return `jsonb_build_object(
 'id',e.id,'created_at',e.created_at,'phase',e.error_phase,'type',e.error_type,'error_owner',COALESCE(e.error_owner,''),'error_source',COALESCE(e.error_source,''),
 'severity',e.severity,'status_code',COALESCE(e.upstream_status_code,e.status_code,0),'platform',COALESCE(e.platform,''),'model',COALESCE(e.model,''),
 'resolved',COALESCE(e.resolved,false),'resolved_at',e.resolved_at,'resolved_by_user_id',e.resolved_by_user_id,'resolved_by_user_name',COALESCE(u2.email,''),
 'client_request_id',COALESCE(e.client_request_id,''),'request_id',COALESCE(e.request_id,''),'message',COALESCE(e.error_message,''),
 'user_id',e.user_id,'user_email',COALESCE(u.email,''),'api_key_id',e.api_key_id,'api_key_name',COALESCE(ak.name,''),'api_key_deleted',ak.deleted_at IS NOT NULL,
 'account_id',e.account_id,'account_name',COALESCE(a.name,''),'group_id',e.group_id,'group_name',COALESCE(g.name,''),'client_ip',host(e.client_ip),
 'request_path',COALESCE(e.request_path,''),'stream',e.stream,'inbound_endpoint',COALESCE(e.inbound_endpoint,''),'upstream_endpoint',COALESCE(e.upstream_endpoint,''),
 'requested_model',COALESCE(e.requested_model,''),'upstream_model',COALESCE(e.upstream_model,''),'request_type',e.request_type,'user_agent',COALESCE(e.user_agent,''))`
}
