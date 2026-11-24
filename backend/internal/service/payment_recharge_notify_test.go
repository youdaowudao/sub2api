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
		"missing defaults to on": nil,
		"on":                     {SettingKeyBalanceRechargeNotifyEnabled: "true"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, userRepo := newRechargeNotifyService(values)
			require.Error(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
			require.Equal(t, 1, userRepo.calls)
		})
	}

	t.Run("off skips recharge email only", func(t *testing.T) {
		svc, userRepo := newRechargeNotifyService(map[string]string{SettingKeyBalanceRechargeNotifyEnabled: "false"})
		require.NoError(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
		require.Zero(t, userRepo.calls)
		// 订阅购买邮件不归该开关管，仍会走到发信步骤。
		require.Error(t, svc.sendSubscriptionPurchaseSuccessNotification(ctx, order))
	})

	t.Run("read failure skips email", func(t *testing.T) {
		svc, userRepo := newRechargeNotifyService(nil)
		svc.notificationEmailService = NewNotificationEmailService(&settingRepoStub{err: errors.New("db down")}, nil)
		require.NoError(t, svc.sendBalanceRechargeSuccessNotification(ctx, order))
		require.Zero(t, userRepo.calls)
	})
}

func TestBalanceRechargeNotifySettingParse(t *testing.T) {
	svc := newAuthService(&userRepoStub{}, map[string]string{}, nil, nil).settingService
	require.True(t, svc.parseSettings(map[string]string{}).BalanceRechargeNotifyEnabled)
	require.False(t, svc.parseSettings(map[string]string{SettingKeyBalanceRechargeNotifyEnabled: "false"}).BalanceRechargeNotifyEnabled)
}
