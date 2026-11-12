//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type affiliateRateRepoStub struct {
	AffiliateRepository // Any unexpected Ensure/Thaw/List/write call panics.
	rate                *float64
	err                 error
	userID              int64
}

func (r *affiliateRateRepoStub) GetUserRebateRate(_ context.Context, id int64) (*float64, error) {
	r.userID = id
	return r.rate, r.err
}

func TestAffiliateRateReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		rate *float64
		want float64
	}{
		{"global", nil, 10}, {"override", ratePointer(12.5), 12.5}, {"zero", ratePointer(0), 0},
		{"invalid", ratePointer(math.NaN()), 10}, {"clamped", ratePointer(120), 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &affiliateRateRepoStub{rate: tc.rate}
			settings := &SettingService{settingRepo: &paymentFulfillmentSettingRepoStub{values: map[string]string{SettingKeyAffiliateEnabled: "true", SettingKeyAffiliateRebateRate: "10"}}}
			svc := &AffiliateService{repo: repo, settingService: settings}
			got, err := svc.GetUserRebateRate(context.Background(), 42)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.EqualValues(t, 42, repo.userID)
		})
	}
}

func ratePointer(v float64) *float64 { return &v }

func TestAffiliateRateFailsClosed(t *testing.T) {
	repo := &affiliateRateRepoStub{err: errors.New("read failed")}
	svc := &AffiliateService{repo: repo, settingService: &SettingService{settingRepo: &paymentFulfillmentSettingRepoStub{values: map[string]string{SettingKeyAffiliateEnabled: "false"}}}}
	_, err := svc.GetUserRebateRate(context.Background(), 42)
	require.Error(t, err)
	require.Zero(t, repo.userID)
	svc.settingService.settingRepo = &paymentFulfillmentSettingRepoStub{values: map[string]string{SettingKeyAffiliateEnabled: "true"}}
	_, err = svc.GetUserRebateRate(context.Background(), 42)
	require.Error(t, err)
	repo.userID = 0
	_, err = svc.GetUserRebateRate(context.Background(), 0)
	require.Error(t, err)
	require.Zero(t, repo.userID)
}
