//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type passwordResetUserRepoStub struct {
	userRepoStub
	user    *User
	updated bool
}

func (s *passwordResetUserRepoStub) GetByEmail(context.Context, string) (*User, error) {
	return s.user, nil
}

func (s *passwordResetUserRepoStub) Update(_ context.Context, user *User, _ UserUpdateFields) error {
	s.updated = true
	s.user = user
	return nil
}

func newPasswordResetAuthService(settings map[string]string, token string) (*AuthService, *passwordResetUserRepoStub) {
	repo := &passwordResetUserRepoStub{user: &User{ID: 7, Email: "user@test.com", Status: StatusActive, PasswordHash: "old"}}
	cache := &resetTokenCacheStub{stored: &PasswordResetTokenData{Token: hashPasswordResetToken(token)}}
	svc := newAuthService(&repo.userRepoStub, settings, cache, nil)
	svc.userRepo = repo
	return svc, repo
}

// 关闭邮箱验证只影响注册，不影响找回密码：申请重置链接和凭链接改密码都要可用。
func TestPasswordResetWorksWithoutEmailVerification(t *testing.T) {
	ctx := context.Background()
	for _, emailVerify := range []string{"true", "false"} {
		t.Run("email_verify="+emailVerify, func(t *testing.T) {
			settings := map[string]string{
				SettingKeyEmailVerifyEnabled:   emailVerify,
				SettingKeyPasswordResetEnabled: "true",
			}
			svc, repo := newPasswordResetAuthService(settings, "reset-token")
			queue := &EmailQueueService{taskChan: make(chan EmailTask, 1)}
			svc.emailQueueService = queue

			require.NoError(t, svc.RequestPasswordResetAsync(ctx, "user@test.com", "https://example.com"))
			require.Len(t, queue.taskChan, 1)
			task := <-queue.taskChan
			require.Equal(t, TaskTypePasswordReset, task.TaskType)
			require.Equal(t, "https://example.com/reset-password", task.ResetURL)

			require.NoError(t, svc.ResetPassword(ctx, "user@test.com", "reset-token", "new-password"))
			require.True(t, repo.updated)
			require.True(t, svc.CheckPassword("new-password", repo.user.PasswordHash))
		})
	}
}

func TestPasswordResetStaysDisabledWhenSwitchIsOff(t *testing.T) {
	ctx := context.Background()
	for name, settings := range map[string]map[string]string{
		"off":     {SettingKeyEmailVerifyEnabled: "true", SettingKeyPasswordResetEnabled: "false"},
		"missing": {SettingKeyEmailVerifyEnabled: "true"},
	} {
		t.Run(name, func(t *testing.T) {
			svc, repo := newPasswordResetAuthService(settings, "reset-token")

			err := svc.RequestPasswordResetAsync(ctx, "user@test.com", "https://example.com")
			require.Equal(t, "PASSWORD_RESET_DISABLED", infraerrors.Reason(err))
			err = svc.ResetPassword(ctx, "user@test.com", "reset-token", "new-password")
			require.Equal(t, "PASSWORD_RESET_DISABLED", infraerrors.Reason(err))
			require.False(t, repo.updated)
		})
	}
}

func TestPasswordResetSettingIndependentOfEmailVerification(t *testing.T) {
	for _, tc := range []struct {
		emailVerify, passwordReset string
		want                       bool
	}{
		{"false", "true", true},
		{"true", "true", true},
		{"true", "false", false},
		{"false", "", false},
	} {
		values := map[string]string{
			SettingKeyEmailVerifyEnabled:   tc.emailVerify,
			SettingKeyPasswordResetEnabled: tc.passwordReset,
		}
		svc := NewSettingService(&settingPublicRepoStub{values: values}, &config.Config{})

		public, err := svc.GetPublicSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, tc.want, public.PasswordResetEnabled, "public %+v", tc)
		require.Equal(t, tc.emailVerify == "true", public.EmailVerifyEnabled)
		require.Equal(t, tc.want, svc.parseSettings(values).PasswordResetEnabled, "admin %+v", tc)
	}
}
