//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

type rechargeNotifyUserRepoStub struct {
	userRepoStub
	calls int
}

func (s *rechargeNotifyUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	s.calls++
	return &User{ID: 7, Balance: 10}, nil
}

// 邮件服务未配置，因此走到发信步骤必然报错；返回 nil 即表示发信前已被开关拦下。
func newRechargeNotifyService(values map[string]string) (*PaymentService, *rechargeNotifyUserRepoStub) {
	settingRepo := newNotificationEmailMemorySettingRepo()
	for key, value := range values {
		settingRepo.values[key] = value
	}
	userRepo := &rechargeNotifyUserRepoStub{}
	return &PaymentService{
		userRepo:                 userRepo,
		notificationEmailService: NewNotificationEmailService(settingRepo, nil),
	}, userRepo
}

func TestBalanceRechargeNotifySwitch(t *testing.T) {
	ctx := context.Background()
	order := &dbent.PaymentOrder{ID: 1, UserID: 7, UserEmail: "user@test.com", Amount: 5}
	for name, values := range map[string]map[string]string{
		"missing": nil, "off": {SettingKeyBalanceRechargeNotifyEnabled: "false"}, "invalid": {SettingKeyBalanceRechargeNotifyEnabled: "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, repo := newRechargeNotifyService(values)
			require.NoError(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
			require.Zero(t, repo.calls)
		})
	}
	t.Run("on", func(t *testing.T) {
		svc, repo := newRechargeNotifyService(map[string]string{SettingKeyBalanceRechargeNotifyEnabled: "true"})
		require.Error(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
		require.Equal(t, 1, repo.calls)
		require.NoError(t, svc.sendSubscriptionPurchaseSuccessNotification(ctx, order))
	})
	t.Run("read failure", func(t *testing.T) {
		svc, repo := newRechargeNotifyService(nil)
		svc.notificationEmailService = NewNotificationEmailService(&settingRepoStub{err: errors.New("db down")}, nil)
		require.NoError(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
		require.Zero(t, repo.calls)
		require.NoError(t, svc.sendSubscriptionPurchaseSuccessNotification(ctx, order))
	})
}

func TestSubscriptionPurchaseNotifySwitch(t *testing.T) {
	ctx := context.Background()
	order := &dbent.PaymentOrder{ID: 1, UserID: 7, UserEmail: "user@test.com"}
	for _, value := range []string{"", "false", "invalid", "true"} {
		t.Run(value+"_value", func(t *testing.T) {
			values := map[string]string{SettingKeyBalanceRechargeNotifyEnabled: "false"}
			if value != "" {
				values["subscription_purchase_notify_enabled"] = value
			}
			svc, repo := newRechargeNotifyService(values)
			err := svc.sendSubscriptionPurchaseSuccessNotification(ctx, order)
			if value == "true" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
			require.Zero(t, repo.calls)
		})
	}
}

func TestBalanceRechargeNotifySettingParse(t *testing.T) {
	svc := newAuthService(&userRepoStub{}, map[string]string{}, nil, nil).settingService
	require.False(t, svc.parseSettings(map[string]string{}).BalanceRechargeNotifyEnabled)
	require.False(t, svc.parseSettings(map[string]string{SettingKeyBalanceRechargeNotifyEnabled: "false"}).BalanceRechargeNotifyEnabled)
	require.True(t, svc.parseSettings(map[string]string{SettingKeyBalanceRechargeNotifyEnabled: "true"}).BalanceRechargeNotifyEnabled)
	require.False(t, svc.parseSettings(map[string]string{}).SubscriptionExpiryNotifyEnabled)
	require.True(t, svc.parseSettings(map[string]string{SettingKeySubscriptionExpiryNotifyEnabled: "true"}).SubscriptionExpiryNotifyEnabled)
}
