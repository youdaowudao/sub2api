//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// 这些用例锁死"每个入口只声明自己真正要改的列"：
// 任何退回整行回写的改动都会让并发写入被陈旧快照覆盖，并在这里变红。

func TestUpdateProfile_OnlyDeclaresRequestedColumns(t *testing.T) {
	username := "renamed"
	tests := []struct {
		name string
		req  UpdateProfileRequest
		want UserUpdateFields
	}{
		{
			name: "username only",
			req:  UpdateProfileRequest{Username: &username},
			want: UserUpdateFields{Username: true},
		},
		{
			name: "notify settings only",
			req:  UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(true)},
			want: UserUpdateFields{BalanceNotifySettings: true},
		},
		{
			name: "username and notify threshold",
			req:  UpdateProfileRequest{Username: &username, BalanceNotifyThreshold: float64Ptr(1.5)},
			want: UserUpdateFields{Username: true, BalanceNotifySettings: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockUserRepo{getByIDUser: &User{ID: 7, Balance: 0.30, Status: StatusActive}}
			svc := NewUserService(repo, nil, nil, nil)

			_, err := svc.UpdateProfile(context.Background(), 7, tt.req)
			require.NoError(t, err)
			require.Equal(t, []UserUpdateFields{tt.want}, repo.updateFields)
		})
	}
}

// 只改头像时用户行没有任何列要写，不应产生一次整行更新。
func TestUpdateProfile_AvatarOnlySkipsUserRowWrite(t *testing.T) {
	repo := &mockUserRepo{getByIDUser: &User{ID: 7, Balance: 0.30}}
	svc := NewUserService(repo, nil, nil, nil)

	avatar := "https://cdn.example.com/a.png"
	_, err := svc.UpdateProfile(context.Background(), 7, UpdateProfileRequest{AvatarURL: &avatar})
	require.NoError(t, err)
	require.Len(t, repo.upsertAvatarArgs, 1, "avatar must still be stored")
	require.Equal(t, []UserUpdateFields{{}}, repo.updateFields, "no user column should be declared")
}

func TestChangePassword_OnlyDeclaresPasswordHash(t *testing.T) {
	user := &User{ID: 7, Balance: 0.30}
	require.NoError(t, user.SetPassword("old-password"))
	repo := &mockUserRepo{getByIDUser: user}
	svc := NewUserService(repo, nil, nil, nil)

	err := svc.ChangePassword(context.Background(), 7, ChangePasswordRequest{
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	})
	require.NoError(t, err)
	require.Equal(t, []UserUpdateFields{{PasswordHash: true}}, repo.updateFields)
}

func TestUpdateStatus_OnlyDeclaresStatus(t *testing.T) {
	repo := &mockUserRepo{getByIDUser: &User{ID: 7, Balance: 0.30, Status: StatusActive}}
	svc := NewUserService(repo, nil, nil, nil)

	require.NoError(t, svc.UpdateStatus(context.Background(), 7, StatusDisabled))
	require.Equal(t, []UserUpdateFields{{Status: true}}, repo.updateFields)
}

func TestUpdateProfile_NotificationChangeInvalidatesAuthCache(t *testing.T) {
	concurrency := 3
	for _, tc := range []struct {
		name           string
		req            UpdateProfileRequest
		want           bool
		fail           bool
		initialEnabled bool
	}{
		{"enable", UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(true)}, true, false, false},
		{"disable", UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(false)}, true, false, true},
		{"combined", UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(true), BalanceNotifyThreshold: float64Ptr(2), Concurrency: &concurrency}, true, false, false},
		{"threshold", UpdateProfileRequest{BalanceNotifyThreshold: float64Ptr(2)}, true, false, false},
		{"clear threshold", UpdateProfileRequest{BalanceNotifyThreshold: float64Ptr(0)}, true, false, false},
		{"email", UpdateProfileRequest{Email: strPtr("new@example.com")}, true, false, false},
		{"unchanged", UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(false), BalanceNotifyThreshold: float64Ptr(5)}, false, false, false},
		{"unchanged email", UpdateProfileRequest{Email: strPtr("old@example.com")}, false, false, false},
		{"failed", UpdateProfileRequest{BalanceNotifyEnabled: boolPtr(true)}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockUserRepo{getByIDUser: &User{ID: 7, Email: "old@example.com", BalanceNotifyThreshold: float64Ptr(5), BalanceNotifyEnabled: tc.initialEnabled}}
			if tc.fail {
				repo.updateFn = func(context.Context, *User) error { return ErrUserNotFound }
			}
			invalidator := &mockAuthCacheInvalidator{}
			svc := NewUserService(repo, nil, invalidator, nil)
			_, err := svc.UpdateProfile(context.Background(), 7, tc.req)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.want {
				require.Equal(t, []int64{7}, invalidator.invalidatedUserIDs)
			} else {
				require.Empty(t, invalidator.invalidatedUserIDs)
			}
		})
	}
}

func TestNotifyEmailChangesInvalidateAuthCache(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		change func(*UserService) error
		want   bool
	}{
		{"disable", func(s *UserService) error { return s.ToggleNotifyEmail(ctx, 7, "verified@example.com", true) }, true},
		{"unchanged", func(s *UserService) error { return s.ToggleNotifyEmail(ctx, 7, "verified@example.com", false) }, false},
		{"remove", func(s *UserService) error { return s.RemoveNotifyEmail(ctx, 7, "verified@example.com") }, true},
		{"verify", func(s *UserService) error { return s.addOrVerifyNotifyEmail(ctx, 7, "unverified@example.com") }, true},
		{"add", func(s *UserService) error { return s.addOrVerifyNotifyEmail(ctx, 7, "new@example.com") }, true},
		{"already verified", func(s *UserService) error { return s.addOrVerifyNotifyEmail(ctx, 7, "verified@example.com") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockUserRepo{getByIDUser: &User{ID: 7, BalanceNotifyExtraEmails: []NotifyEmailEntry{{Email: "verified@example.com", Verified: true}, {Email: "unverified@example.com"}}}}
			invalidator := &mockAuthCacheInvalidator{}
			svc := NewUserService(repo, nil, invalidator, nil)
			require.NoError(t, tc.change(svc))
			if tc.want {
				require.Equal(t, []int64{7}, invalidator.invalidatedUserIDs)
			} else {
				require.Empty(t, invalidator.invalidatedUserIDs)
			}
		})
	}
}

func TestNotifyEmailFailedWriteDoesNotInvalidateAuthCache(t *testing.T) {
	repo := &mockUserRepo{getByIDUser: &User{ID: 7, BalanceNotifyExtraEmails: []NotifyEmailEntry{{Email: "old@example.com", Verified: true}}}, updateFn: func(context.Context, *User) error { return ErrUserNotFound }}
	invalidator := &mockAuthCacheInvalidator{}
	svc := NewUserService(repo, nil, invalidator, nil)
	require.Error(t, svc.RemoveNotifyEmail(context.Background(), 7, "old@example.com"))
	require.Empty(t, invalidator.invalidatedUserIDs)
}
