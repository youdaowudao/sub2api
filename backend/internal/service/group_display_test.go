//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type groupDisplayRepoStub struct {
	GroupRepository
	stored      Group
	createCalls int
	updateCalls int
	readCalls   int
}

func (r *groupDisplayRepoStub) Create(_ context.Context, group *Group) error {
	r.createCalls++
	group.ID = 1
	r.stored = *group
	return nil
}

func (r *groupDisplayRepoStub) Update(_ context.Context, group *Group) error {
	r.updateCalls++
	r.stored = *group
	return nil
}

func (r *groupDisplayRepoStub) GetByID(_ context.Context, _ int64) (*Group, error) {
	r.readCalls++
	group := r.stored
	return &group, nil
}

func TestGroupDisplayWriteAndPreserve(t *testing.T) {
	for _, mode := range []string{config.RunModeStandard, config.RunModeSimple} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo := &groupDisplayRepoStub{}
			svc := &adminServiceImpl{cfg: &config.Config{RunMode: mode}, groupRepo: repo}
			created, err := svc.CreateGroup(ctx, &CreateGroupInput{
				Name: "display group", Platform: PlatformAnthropic, RateMultiplier: 1,
				DisplayIcon: "  gemini\t", DisplayCategory: " domestic ",
			})
			require.NoError(t, err)
			require.Equal(t, 1, repo.createCalls)
			require.Equal(t, "gemini", repo.stored.DisplayIcon)
			require.Equal(t, "domestic", repo.stored.DisplayCategory)

			_, err = svc.UpdateGroup(ctx, created.ID, &UpdateGroupInput{
				DisplayIcon: stringPtr(" deepseek "), DisplayCategory: stringPtr(" gemini "),
			})
			require.NoError(t, err)
			require.Equal(t, 1, repo.updateCalls)
			require.Equal(t, "deepseek", repo.stored.DisplayIcon)
			require.Equal(t, "gemini", repo.stored.DisplayCategory)

			// Omitted fields preserve what was read even if the stored values are invalid.
			repo.stored.DisplayIcon = " typesafe "
			repo.stored.DisplayCategory = " xyz "
			_, err = svc.UpdateGroup(ctx, created.ID, &UpdateGroupInput{Name: "renamed"})
			require.NoError(t, err)
			require.Equal(t, 2, repo.updateCalls)
			require.Equal(t, "renamed", repo.stored.Name)
			require.Equal(t, " typesafe ", repo.stored.DisplayIcon)
			require.Equal(t, " xyz ", repo.stored.DisplayCategory)

			_, err = svc.UpdateGroup(ctx, created.ID, &UpdateGroupInput{
				DisplayIcon: stringPtr(""), DisplayCategory: stringPtr(""),
			})
			require.NoError(t, err)
			require.Equal(t, 3, repo.updateCalls)
			require.Empty(t, repo.stored.DisplayIcon)
			require.Empty(t, repo.stored.DisplayCategory)
		})
	}
}

func TestGroupDisplayImageCategoryPersists(t *testing.T) {
	for _, mode := range []string{config.RunModeStandard, config.RunModeSimple} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo := &groupDisplayRepoStub{}
			svc := &adminServiceImpl{cfg: &config.Config{RunMode: mode}, groupRepo: repo}
			created, err := svc.CreateGroup(ctx, &CreateGroupInput{
				Name: "display group", Platform: PlatformOpenAI, RateMultiplier: 1,
				DisplayCategory: " image ",
			})
			require.NoError(t, err)
			require.Equal(t, "image", repo.stored.DisplayCategory)
			require.Equal(t, PlatformOpenAI, repo.stored.Platform)
			_, err = svc.UpdateGroup(ctx, created.ID, &UpdateGroupInput{DisplayCategory: stringPtr("")})
			require.NoError(t, err)
			require.Empty(t, repo.stored.DisplayCategory)
			_, err = svc.UpdateGroup(ctx, created.ID, &UpdateGroupInput{DisplayCategory: stringPtr(" image ")})
			require.NoError(t, err)
			require.Equal(t, "image", repo.stored.DisplayCategory)
			require.Equal(t, PlatformOpenAI, repo.stored.Platform)
			require.Equal(t, 1, repo.createCalls)
			require.Equal(t, 2, repo.updateCalls)
		})
	}
}

func TestGroupDisplayRejectsInvalidValuesBeforeRepositoryCalls(t *testing.T) {
	for _, tc := range []struct {
		name, reason   string
		icon, category *string
	}{
		{name: "unknown icon", icon: stringPtr("abc"), reason: "INVALID_GROUP_DISPLAY_ICON"},
		{name: "generic icon", icon: stringPtr("typesafe"), reason: "INVALID_GROUP_DISPLAY_ICON"},
		{name: "fallback category", category: stringPtr("other"), reason: "INVALID_GROUP_DISPLAY_CATEGORY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupDisplayRepoStub{}
			svc := &adminServiceImpl{groupRepo: repo}
			create := &CreateGroupInput{Name: "display group", Platform: PlatformAnthropic, RateMultiplier: 1}
			if tc.icon != nil {
				create.DisplayIcon = *tc.icon
			}
			if tc.category != nil {
				create.DisplayCategory = *tc.category
			}
			_, createErr := svc.CreateGroup(context.Background(), create)
			_, updateErr := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{
				DisplayIcon: tc.icon, DisplayCategory: tc.category,
			})
			for _, err := range []error{createErr, updateErr} {
				require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
				require.Equal(t, tc.reason, infraerrors.Reason(err))
			}
			require.Zero(t, repo.createCalls)
			require.Zero(t, repo.updateCalls)
			require.Zero(t, repo.readCalls)
		})
	}
}

func TestGroupDisplayIconPlatformAllowlist(t *testing.T) {
	want := []string{
		"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu",
		"minimax", "deepseek", "opencode_go", "command_code", "cline",
	}
	require.Equal(t, want, groupDisplayIconPlatforms)
	for _, icon := range want {
		got, err := normalizeGroupDisplayIcon(icon)
		require.NoError(t, err)
		require.Equal(t, icon, got)
	}
}

func TestGroupDisplayDuplicateCopiesStoredValuesUnchanged(t *testing.T) {
	source := &Group{
		ID: 1, Name: "display group", Status: StatusActive, Platform: PlatformOpenAI,
		DisplayIcon: " typesafe ", DisplayCategory: "未登记分类",
	}
	repo := newDuplicateGroupRepoStub(source)
	svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}
	displayCopy, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:1", "display-copy")
	require.NoError(t, err)
	require.Equal(t, []int64{source.ID}, repo.createdFromSources)
	written := repo.groups[displayCopy.ID]
	require.NotNil(t, written)
	require.Equal(t, " typesafe ", written.DisplayIcon)
	require.Equal(t, "未登记分类", written.DisplayCategory)
	require.Equal(t, "inactive", written.Status)
}
