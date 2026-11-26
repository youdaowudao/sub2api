package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/shopspring/decimal"
)

const (
	checkinVeteran     = "veteran"
	checkinNewFunded   = "new_funded"
	checkinNewUnfunded = "new_unfunded"
)

var (
	ErrCheckinDisabled          = infraerrors.Forbidden("CHECKIN_DISABLED", "签到功能未开启")
	ErrCheckinUnavailable       = infraerrors.ServiceUnavailable("CHECKIN_UNAVAILABLE", "签到暂不可用，请稍后再试")
	ErrCheckinAlreadyDone       = infraerrors.Conflict("CHECKIN_ALREADY_DONE", "今天已经签到过了")
	ErrCheckinCaptchaRequired   = infraerrors.BadRequest("CHECKIN_CAPTCHA_REQUIRED", "请完成人机验证")
	ErrCheckinCaptchaFailed     = infraerrors.BadRequest("CHECKIN_CAPTCHA_FAILED", "人机验证未通过，请重试")
	ErrCheckinEmailRequired     = infraerrors.BadRequest("CHECKIN_EMAIL_REQUIRED", "为保障账户安全，请完成邮箱验证后继续签到")
	ErrCheckinEmailBindRequired = infraerrors.Forbidden("CHECKIN_EMAIL_BIND_REQUIRED", "为保障账户安全，请先绑定邮箱后继续签到")
	ErrCheckinIPUnknown         = infraerrors.Forbidden("CHECKIN_IP_UNKNOWN", "无法确认当前网络，请刷新页面后再试")
	ErrCheckinIPLimit           = infraerrors.New(429, "CHECKIN_IP_LIMIT", "当前网络今日签到次数已达上限，请明天再试")
	ErrCheckinFailed            = infraerrors.New(500, "CHECKIN_FAILED", "签到失败，余额未变动，请稍后重试")
	ErrCheckinMonthInvalid      = infraerrors.BadRequest("CHECKIN_MONTH_INVALID", "月份无效")
	ErrCheckinEmailNotRequired  = infraerrors.Conflict("CHECKIN_EMAIL_NOT_REQUIRED", "当前无需邮箱验证")
)

var checkinSettingKeys = append([]string{SettingKeyCheckinConfig, SettingKeyTurnstileSiteKey, SettingKeyAliyunCaptchaPrefix}, captchaProviderConfigKeys...)

type CheckinUser struct {
	Settings       map[string]string
	Email, Status  string
	TotalRecharged decimal.Decimal
	CreatedAt      time.Time
}

type CheckinStats struct {
	AdminNet                          decimal.Decimal
	AdminHasCredit                    bool
	TotalDays, TotalCents, MonthCents int64
	TodayCents                        *int64
	LaterExists                       bool
	PrevDate, LastEmailDate           string
	SinceEmail, RunLen                int64
}

type CheckinRecord struct {
	UserID                 int64
	Date                   string
	AmountCents            int64
	Tier, Verification, IP string
	CreatedAt              time.Time
}

type CheckinMonthData struct {
	Cents   int64
	Records []CheckinRecord
}
type CheckinState struct {
	User  CheckinUser
	Stats CheckinStats
	Month CheckinMonthData
}
type CheckinCommitResult struct {
	IPCount   int
	ID        int64
	CreatedAt time.Time
	Balance   *decimal.Decimal
}

type CheckinRepository interface {
	WithTx(context.Context, func(context.Context) error) error
	LoadState(context.Context, int64, []string, string, string) (CheckinState, error)
	LockUser(context.Context, int64, []string) (CheckinUser, error)
	LoadStats(context.Context, int64, string) (CheckinStats, error)
	LockIP(context.Context, string, string) error
	Commit(context.Context, CheckinRecord, int) (CheckinCommitResult, error)
	ListMonth(context.Context, int64, string) (CheckinMonthData, error)
	Overview(context.Context, string, int) (CheckinOverview, error)
	ListAdminUsers(context.Context, CheckinConfig, CheckinUserFilter, pagination.PaginationParams) ([]CheckinAdminUser, int64, error)
	ListAdminRecords(context.Context, CheckinRecordFilter, pagination.PaginationParams) ([]CheckinAdminRecord, int64, error)
	ListAdminIPs(context.Context, CheckinIPFilter, pagination.PaginationParams) ([]CheckinAdminIP, int64, error)
}

type CheckinRequirement struct {
	Status, Tier             string
	NeedsCaptcha, NeedsEmail bool
	AvailableAt              time.Time
}

func checkinTier(c CheckinConfig, u CheckinUser, stats CheckinStats) string {
	r := u.TotalRecharged.Add(stats.AdminNet)
	if r.GreaterThanOrEqual(c.VeteranThreshold) {
		return checkinVeteran
	}
	if u.TotalRecharged.IsPositive() || stats.AdminHasCredit {
		return checkinNewFunded
	}
	return checkinNewUnfunded
}

func resolveRequirement(c CheckinConfig, valid bool, u CheckinUser, stats CheckinStats, now time.Time, captchaAvailable bool) CheckinRequirement {
	if !valid {
		return CheckinRequirement{Status: "unavailable"}
	}
	if !c.Enabled {
		return CheckinRequirement{Status: "disabled"}
	}
	if stats.TodayCents != nil || stats.LaterExists {
		return CheckinRequirement{Status: "done"}
	}
	tier := checkinTier(c, u, stats)
	if tier == checkinVeteran {
		return CheckinRequirement{Status: "available", Tier: tier}
	}
	availableAt := u.CreatedAt.Add(time.Duration(c.MinAccountAgeDays) * 24 * time.Hour)
	if c.MinAccountAgeDays > 0 && now.Before(availableAt) {
		return CheckinRequirement{Status: "too_new", Tier: tier, AvailableAt: availableAt}
	}
	needEmail := tier == checkinNewUnfunded && c.EmailVerifyEnabled && ((stats.LastEmailDate == "" && stats.TotalDays >= int64(c.EmailVerifyFirst)) || (stats.LastEmailDate != "" && c.EmailVerifyEvery > 0 && stats.SinceEmail >= int64(c.EmailVerifyEvery)))
	if needEmail && isReservedEmail(u.Email) {
		if !c.EmailVerifySkipReserved {
			return CheckinRequirement{Status: "bind_email", Tier: tier}
		}
		needEmail = false
	}
	if c.NewcomerCaptcha && !captchaAvailable {
		return CheckinRequirement{Status: "unavailable", Tier: tier}
	}
	return CheckinRequirement{Status: "available", Tier: tier, NeedsCaptcha: c.NewcomerCaptcha, NeedsEmail: needEmail}
}

func checkinRewardCents(c CheckinConfig, tier string, total int64, random func(int64) int64) int64 {
	low, high := c.NewcomerMin, c.NewcomerMax
	if tier == checkinVeteran {
		low, high = c.VeteranMin, c.VeteranMax
	} else if total >= c.NewcomerCap {
		return c.NewcomerAfterCap
	}
	if low == high {
		return low
	}
	return low + random(high-low+1)
}

type CheckinCaptchaPublic struct {
	SiteKey string `json:"site_key,omitempty"`
	AppID   string `json:"app_id,omitempty"`
	SceneID string `json:"scene_id,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Region  string `json:"region,omitempty"`
}
type checkinCaptchaSelection struct {
	Provider string
	Public   CheckinCaptchaPublic
	Config   CaptchaProviderConfig
}

func selectCheckinCaptcha(values map[string]string) checkinCaptchaSelection {
	c := parseCaptchaProviderConfig(values)
	enabled := 0
	for _, on := range []bool{c.TurnstileEnabled, c.Tencent.Enabled, c.Aliyun.Enabled} {
		if on {
			enabled++
		}
	}
	if enabled > 1 {
		return checkinCaptchaSelection{}
	}
	if c.Tencent.Enabled {
		if _, ok := parseTencentCaptchaCredentials(c.Tencent); !ok {
			return checkinCaptchaSelection{}
		}
		return checkinCaptchaSelection{Provider: "tencent", Config: c, Public: CheckinCaptchaPublic{AppID: strings.TrimSpace(c.Tencent.AppID), Region: c.Tencent.Region}}
	}
	if c.Aliyun.Enabled {
		prefix := strings.TrimSpace(values[SettingKeyAliyunCaptchaPrefix])
		if _, ok := aliyunCaptchaCredentials(c.Aliyun); !ok || prefix == "" {
			return checkinCaptchaSelection{}
		}
		return checkinCaptchaSelection{Provider: "aliyun", Config: c, Public: CheckinCaptchaPublic{SceneID: strings.TrimSpace(c.Aliyun.SceneID), Prefix: prefix, Region: c.Aliyun.Region}}
	}
	key := strings.TrimSpace(values[SettingKeyTurnstileSiteKey])
	if key == "" || strings.TrimSpace(c.TurnstileSecretKey) == "" {
		return checkinCaptchaSelection{}
	}
	return checkinCaptchaSelection{Provider: "turnstile", Config: c, Public: CheckinCaptchaPublic{SiteKey: key}}
}
func (c checkinCaptchaSelection) sameIdentity(other checkinCaptchaSelection) bool {
	return c.Provider == other.Provider && c.Public.SiteKey == other.Public.SiteKey && c.Public.AppID == other.Public.AppID && c.Public.SceneID == other.Public.SceneID && c.Public.Prefix == other.Public.Prefix
}

type CheckinProof struct {
	Token   string `json:"captcha_token"`
	Randstr string `json:"captcha_randstr"`
}
type checkinEmailService interface {
	VerifyCode(context.Context, string, string) error
	SendVerifyCode(context.Context, string, string, ...string) error
}

type CheckinService struct {
	repo           CheckinRepository
	settings       SettingRepository
	settingService *SettingService
	email          checkinEmailService
	authCache      APIKeyAuthCacheInvalidator
	billingCache   *BillingCacheService
	verifyCaptcha  func(context.Context, checkinCaptchaSelection, CheckinProof, string) error
	// Clock and Random are set before using the service; production defaults are supplied by its constructor.
	Clock  func() time.Time
	Random func(int64) int64
}

func NewCheckinService(repo CheckinRepository, settings SettingRepository, settingService *SettingService, turnstile *TurnstileService, tencent *TencentCaptchaService, aliyun *AliyunCaptchaService, email *EmailService, authCache APIKeyAuthCacheInvalidator, billingCache *BillingCacheService) *CheckinService {
	s := &CheckinService{repo: repo, settings: settings, settingService: settingService, authCache: authCache, billingCache: billingCache, Clock: time.Now, Random: rand.Int64N}
	if email != nil {
		s.email = email
	}
	s.verifyCaptcha = func(ctx context.Context, c checkinCaptchaSelection, proof CheckinProof, ip string) error {
		switch c.Provider {
		case "turnstile":
			return turnstile.VerifyTokenWithSecret(ctx, c.Config.TurnstileSecretKey, proof.Token, ip)
		case "tencent":
			return tencent.VerifyTicketWithConfig(ctx, c.Config.Tencent, proof.Token, proof.Randstr, ip)
		case "aliyun":
			return aliyun.VerifyParamWithConfig(ctx, c.Config.Aliyun, proof.Token)
		default:
			return ErrCheckinUnavailable
		}
	}
	return s
}

type CheckinSummary struct {
	TotalDays   int64   `json:"total_days"`
	TotalAmount float64 `json:"total_amount"`
	Streak      int64   `json:"streak"`
}
type CheckinMonthRecord struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
	Time   string  `json:"time"`
}
type CheckinMonthResponse struct {
	Month       string               `json:"month"`
	MonthAmount float64              `json:"month_amount"`
	Records     []CheckinMonthRecord `json:"records"`
}
type CheckinStateResponse struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status,omitempty"`
	*CheckinStateDetails
}
type CheckinStateDetails struct {
	Today             string                `json:"today"`
	SecondsUntilReset int64                 `json:"seconds_until_reset"`
	RegisteredMonth   string                `json:"registered_month"`
	NeedsCaptcha      bool                  `json:"needs_captcha"`
	NeedsEmail        bool                  `json:"needs_email"`
	CaptchaProvider   string                `json:"captcha_provider,omitempty"`
	CaptchaPublic     *CheckinCaptchaPublic `json:"captcha_public,omitempty"`
	MaskedEmail       string                `json:"masked_email,omitempty"`
	AvailableAt       string                `json:"available_at,omitempty"`
	TodayAmount       *float64              `json:"today_amount"`
	Stats             CheckinSummary        `json:"stats"`
	CheckinMonthResponse
}
type CheckinResult struct {
	Amount      float64 `json:"amount"`
	Balance     float64 `json:"balance"`
	CheckinDate string  `json:"checkin_date"`
	Time        string  `json:"time"`
	Month       string  `json:"month"`
	MonthAmount float64 `json:"month_amount"`
	CheckinSummary
}
type CheckinSendCodeResult struct {
	MaskedEmail     string `json:"masked_email"`
	CooldownSeconds int    `json:"cooldown_seconds"`
}
type CheckinConfigResult struct {
	Config           CheckinConfig `json:"config"`
	ConfigValid      bool          `json:"config_valid"`
	CaptchaProvider  string        `json:"captcha_provider"`
	CaptchaAvailable bool          `json:"captcha_available"`
}

func checkinMonth(now time.Time, month string) (string, error) {
	current := now.In(timezone.Location()).Format("2006-01")
	if month == "" {
		return current, nil
	}
	parsed, e := time.Parse("2006-01", month)
	if e != nil || parsed.Year() < 1 || parsed.Format("2006-01") != month || month > current {
		return "", ErrCheckinMonthInvalid
	}
	return month, nil
}
func checkinMaskedEmail(email string) string {
	i := strings.LastIndex(email, "@")
	if i < 1 {
		return "***"
	}
	_, n := utf8.DecodeRuneInString(email[:i])
	return email[:n] + "***" + email[i:]
}
func checkinMonthResponse(month string, data CheckinMonthData) CheckinMonthResponse {
	records := make([]CheckinMonthRecord, 0, len(data.Records))
	for _, r := range data.Records {
		records = append(records, CheckinMonthRecord{Date: r.Date, Amount: checkinCentsAmount(r.AmountCents), Time: r.CreatedAt.In(timezone.Location()).Format("15:04")})
	}
	return CheckinMonthResponse{Month: month, MonthAmount: checkinCentsAmount(data.Cents), Records: records}
}

func (s *CheckinService) GetState(ctx context.Context, id int64, month string) (CheckinStateResponse, error) {
	now := s.Clock()
	d := timezone.StartOfDay(now)
	today := d.Format("2006-01-02")
	month, e := checkinMonth(now, month)
	if e != nil {
		return CheckinStateResponse{}, e
	}
	state, e := s.repo.LoadState(ctx, id, checkinSettingKeys, today, month)
	if e != nil {
		return CheckinStateResponse{}, ErrCheckinFailed
	}
	config, e := readCheckinConfig(state.User.Settings)
	if e != nil {
		return CheckinStateResponse{Enabled: true, Status: "unavailable"}, nil
	}
	if !config.Enabled {
		return CheckinStateResponse{Enabled: false}, nil
	}
	selected := selectCheckinCaptcha(state.User.Settings)
	r := resolveRequirement(config, true, state.User, state.Stats, now, selected.Provider != "")
	details := &CheckinStateDetails{Today: today, SecondsUntilReset: int64(math.Ceil(d.AddDate(0, 0, 1).Sub(now).Seconds())), RegisteredMonth: state.User.CreatedAt.In(timezone.Location()).Format("2006-01"), NeedsCaptcha: r.NeedsCaptcha, NeedsEmail: r.NeedsEmail, Stats: CheckinSummary{TotalDays: state.Stats.TotalDays, TotalAmount: checkinCentsAmount(state.Stats.TotalCents)}, CheckinMonthResponse: checkinMonthResponse(month, state.Month)}
	if r.NeedsCaptcha {
		details.CaptchaProvider = selected.Provider
		details.CaptchaPublic = &selected.Public
	}
	if r.NeedsEmail {
		details.MaskedEmail = checkinMaskedEmail(state.User.Email)
	}
	if r.Status == "too_new" {
		details.AvailableAt = r.AvailableAt.In(timezone.Location()).Format(time.RFC3339)
	}
	if state.Stats.LaterExists {
		details.SecondsUntilReset = 0
	} else {
		if state.Stats.TodayCents != nil {
			amount := checkinCentsAmount(*state.Stats.TodayCents)
			details.TodayAmount = &amount
		}
		if state.Stats.PrevDate == today || state.Stats.PrevDate == d.AddDate(0, 0, -1).Format("2006-01-02") {
			details.Stats.Streak = state.Stats.RunLen
		}
	}
	return CheckinStateResponse{Enabled: true, Status: r.Status, CheckinStateDetails: details}, nil
}

func (s *CheckinService) ListMonth(ctx context.Context, id int64, month string) (CheckinMonthResponse, error) {
	now := s.Clock()
	m, e := checkinMonth(now, month)
	if e != nil {
		return CheckinMonthResponse{}, e
	}
	data, e := s.repo.ListMonth(ctx, id, m)
	if e != nil {
		return CheckinMonthResponse{}, ErrCheckinFailed
	}
	return checkinMonthResponse(m, data), nil
}

func (s *CheckinService) SendEmailCode(ctx context.Context, id int64, locale string) (CheckinSendCodeResult, error) {
	now := s.Clock()
	today := timezone.StartOfDay(now).Format("2006-01-02")
	state, e := s.repo.LoadState(ctx, id, checkinSettingKeys, today, "")
	if e != nil {
		return CheckinSendCodeResult{}, ErrCheckinFailed
	}
	c, e := readCheckinConfig(state.User.Settings)
	r := resolveRequirement(c, e == nil, state.User, state.Stats, now, selectCheckinCaptcha(state.User.Settings).Provider != "")
	if r.Status != "available" || !r.NeedsEmail {
		return CheckinSendCodeResult{}, ErrCheckinEmailNotRequired
	}
	if s.email == nil || s.settingService == nil {
		return CheckinSendCodeResult{}, ErrCheckinFailed
	}
	if e = s.email.SendVerifyCode(ctx, state.User.Email, s.settingService.GetSiteName(ctx), locale); e != nil {
		return CheckinSendCodeResult{}, e
	}
	return CheckinSendCodeResult{MaskedEmail: checkinMaskedEmail(state.User.Email), CooldownSeconds: 60}, nil
}

func (s *CheckinService) Checkin(ctx context.Context, id int64, _ string, proof CheckinProof, emailCode string) (CheckinResult, error) {
	var ip string
	if binding := SessionBindingFromContext(ctx); binding != nil {
		if parsed := net.ParseIP(strings.TrimSpace(binding.IP)); parsed != nil {
			ip = parsed.String()
		}
	}
	var outer checkinCaptchaSelection
	var proofErr error
	if proof.Token != "" {
		values, e := s.settings.GetMultiple(ctx, checkinSettingKeys)
		if e != nil {
			return CheckinResult{}, ErrCheckinFailed
		}
		c, e := readCheckinConfig(values)
		if e != nil {
			return CheckinResult{}, ErrCheckinUnavailable
		}
		if !c.Enabled {
			return CheckinResult{}, ErrCheckinDisabled
		}
		outer = selectCheckinCaptcha(values)
		if outer.Provider != "" {
			proofErr = s.verifyCaptcha(ctx, outer, proof, ip)
		}
	}
	var result CheckinResult
	err := s.repo.WithTx(ctx, func(tx context.Context) error {
		user, e := s.repo.LockUser(tx, id, checkinSettingKeys)
		if e != nil {
			return ErrCheckinFailed
		}
		c, e := readCheckinConfig(user.Settings)
		if e != nil {
			return ErrCheckinUnavailable
		}
		if !c.Enabled {
			return ErrCheckinDisabled
		}
		if user.Status != StatusActive {
			return ErrCheckinFailed
		}
		now := s.Clock()
		day := timezone.StartOfDay(now)
		today := day.Format("2006-01-02")
		stats, e := s.repo.LoadStats(tx, id, today)
		if e != nil {
			return ErrCheckinFailed
		}
		selected := selectCheckinCaptcha(user.Settings)
		r := resolveRequirement(c, true, user, stats, now, selected.Provider != "")
		switch r.Status {
		case "done":
			return ErrCheckinAlreadyDone
		case "too_new":
			return infraerrors.Forbidden("CHECKIN_ACCOUNT_TOO_NEW", r.AvailableAt.In(timezone.Location()).Format("01-02 15:04")+" 起可以签到")
		case "bind_email":
			return ErrCheckinEmailBindRequired
		case "unavailable":
			return ErrCheckinUnavailable
		case "disabled":
			return ErrCheckinDisabled
		}
		verification := "none"
		if r.NeedsCaptcha {
			if proof.Token == "" {
				return ErrCheckinCaptchaRequired
			}
			if outer.Provider == "" {
				return ErrCheckinUnavailable
			}
			if proofErr != nil {
				return ErrCheckinCaptchaFailed
			}
			if !outer.sameIdentity(selected) {
				return ErrCheckinCaptchaRequired
			}
			verification = "captcha"
		}
		if r.NeedsEmail {
			if strings.TrimSpace(emailCode) == "" {
				return ErrCheckinEmailRequired
			}
			if s.email == nil {
				return ErrCheckinFailed
			}
			if e = s.email.VerifyCode(tx, user.Email, emailCode); e != nil {
				return e
			}
			verification = "email"
		}
		limit := 0
		if r.Tier != checkinVeteran && c.IPDailyLimit > 0 {
			if ip == "" {
				return ErrCheckinIPUnknown
			}
			limit = c.IPDailyLimit
			if e = s.repo.LockIP(tx, today, ip); e != nil {
				return ErrCheckinFailed
			}
		}
		cents := checkinRewardCents(c, r.Tier, stats.TotalCents, s.Random)
		committed, e := s.repo.Commit(tx, CheckinRecord{UserID: id, Date: today, AmountCents: cents, Tier: r.Tier, Verification: verification, IP: ip, CreatedAt: now}, limit)
		if e != nil {
			return ErrCheckinFailed
		}
		if limit > 0 && committed.IPCount >= limit {
			return ErrCheckinIPLimit
		}
		if committed.ID == 0 {
			return ErrCheckinAlreadyDone
		}
		if committed.Balance == nil {
			return ErrCheckinFailed
		}
		streak := int64(1)
		if stats.PrevDate == day.AddDate(0, 0, -1).Format("2006-01-02") {
			streak = stats.RunLen + 1
		}
		result = CheckinResult{Amount: checkinCentsAmount(cents), Balance: committed.Balance.InexactFloat64(), CheckinDate: today, Time: committed.CreatedAt.In(timezone.Location()).Format("15:04"), Month: day.Format("2006-01"), MonthAmount: checkinCentsAmount(stats.MonthCents + cents), CheckinSummary: CheckinSummary{TotalDays: stats.TotalDays + 1, TotalAmount: checkinCentsAmount(stats.TotalCents + cents), Streak: streak}}
		return nil
	})
	if err != nil {
		// The result is populated only on the successful transaction callback path.
		// A failed COMMIT acknowledgement cannot prove that the database rolled back.
		if result.CheckinDate != "" {
			return CheckinResult{}, infraerrors.New(500, "CHECKIN_FAILED", "无法确认签到结果，请刷新页面后确认").
				WithMetadata(map[string]string{"result_unknown": "true"})
		}
		var appErr *infraerrors.ApplicationError
		if errors.As(err, &appErr) {
			return CheckinResult{}, appErr
		}
		return CheckinResult{}, ErrCheckinFailed
	}
	if s.authCache != nil {
		s.authCache.InvalidateAuthCacheByUserID(ctx, id)
	}
	if s.billingCache != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if e := s.billingCache.InvalidateUserBalance(cacheCtx, id); e != nil {
				logger.LegacyPrintf("service.checkin", "invalidate user balance cache failed: user_id=%d err=%v", id, e)
			}
		}()
	}
	return result, nil
}

func (s *CheckinService) GetConfig(ctx context.Context) (CheckinConfigResult, error) {
	values, e := s.settings.GetMultiple(ctx, checkinSettingKeys)
	if e != nil {
		return CheckinConfigResult{}, ErrCheckinFailed
	}
	c, e := readCheckinConfig(values)
	valid := e == nil
	if !valid {
		c = defaultCheckinConfig()
	}
	provider := selectCheckinCaptcha(values).Provider
	return CheckinConfigResult{Config: c, ConfigValid: valid, CaptchaProvider: provider, CaptchaAvailable: provider != ""}, nil
}
func (s *CheckinService) SaveConfig(ctx context.Context, raw []byte) (CheckinConfig, error) {
	c, e := parseCheckinConfig(raw)
	if e != nil {
		return CheckinConfig{}, e
	}
	data, e := json.Marshal(c)
	if e != nil {
		return CheckinConfig{}, ErrCheckinFailed
	}
	if e = s.settings.Set(ctx, SettingKeyCheckinConfig, string(data)); e != nil {
		return CheckinConfig{}, ErrCheckinFailed
	}
	return c, nil
}

// Administrative read models use the existing pagination conventions.
type CheckinOverviewTier struct {
	Amount float64 `json:"amount"`
	Users  int64   `json:"users"`
}
type CheckinOverviewPeriod struct {
	Amount float64                        `json:"amount"`
	Users  int64                          `json:"users"`
	Tiers  map[string]CheckinOverviewTier `json:"tiers"`
}
type CheckinTrendPoint struct {
	Date   string  `json:"date"`
	Amount float64 `json:"amount"`
	Users  int64   `json:"users"`
}
type CheckinOverview struct {
	Today              CheckinOverviewPeriod `json:"today"`
	Yesterday          CheckinOverviewPeriod `json:"yesterday"`
	Last7Days          CheckinOverviewPeriod `json:"last_7_days"`
	Last30Days         CheckinOverviewPeriod `json:"last_30_days"`
	AllTime            CheckinOverviewPeriod `json:"all_time"`
	EmailVerifications int64                 `json:"email_verifications"`
	Trend              []CheckinTrendPoint   `json:"trend"`
}
type CheckinUserFilter struct{ Search, Tier string }
type CheckinRecordFilter struct{ DateFrom, DateTo, Search, IP, Tier, Verification string }
type CheckinIPFilter struct {
	DateFrom, DateTo string
	MinUsers         int
}
type CheckinAdminUser struct {
	UserID             int64       `json:"user_id"`
	Email              string      `json:"email"`
	Username           string      `json:"username"`
	Status             string      `json:"status"`
	R                  json.Number `json:"r"`
	Tier               string      `json:"tier"`
	TotalDays          int64       `json:"total_days"`
	TotalAmount        float64     `json:"total_amount"`
	FirstCheckin       string      `json:"first_checkin"`
	LastCheckin        string      `json:"last_checkin"`
	LastIP             string      `json:"last_ip"`
	SharedIPUsers      int64       `json:"shared_ip_users"`
	EmailVerifications int64       `json:"email_verifications"`
	PendingEmail       bool        `json:"pending_email"`
}
type CheckinAdminRecord struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	Email        string    `json:"email"`
	Amount       float64   `json:"amount"`
	Date         string    `json:"date"`
	CreatedAt    time.Time `json:"created_at"`
	Tier         string    `json:"tier"`
	IP           string    `json:"ip"`
	Verification string    `json:"verification"`
}
type CheckinAdminIP struct {
	IP          string   `json:"ip"`
	Users       int64    `json:"users"`
	TotalDays   int64    `json:"total_days"`
	TotalAmount float64  `json:"total_amount"`
	FirstDate   string   `json:"first_date"`
	LastDate    string   `json:"last_date"`
	Emails      []string `json:"emails"`
}

func (s *CheckinService) Overview(ctx context.Context, days int) (CheckinOverview, error) {
	return s.repo.Overview(ctx, timezone.StartOfDay(s.Clock()).Format("2006-01-02"), days)
}
func (s *CheckinService) ListAdminUsers(ctx context.Context, filters CheckinUserFilter, p pagination.PaginationParams) ([]CheckinAdminUser, int64, error) {
	values, e := s.settings.GetMultiple(ctx, checkinSettingKeys)
	if e != nil {
		return nil, 0, ErrCheckinFailed
	}
	c, e := readCheckinConfig(values)
	if e != nil {
		return nil, 0, ErrCheckinUnavailable
	}
	return s.repo.ListAdminUsers(ctx, c, filters, p)
}
func (s *CheckinService) ListAdminRecords(ctx context.Context, f CheckinRecordFilter, p pagination.PaginationParams) ([]CheckinAdminRecord, int64, error) {
	return s.repo.ListAdminRecords(ctx, f, p)
}
func (s *CheckinService) ListAdminIPs(ctx context.Context, f CheckinIPFilter, p pagination.PaginationParams) ([]CheckinAdminIP, int64, error) {
	return s.repo.ListAdminIPs(ctx, f, p)
}
