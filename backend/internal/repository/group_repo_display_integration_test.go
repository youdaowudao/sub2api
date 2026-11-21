//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newDisplayTestGroup(name, icon, category string) *service.Group {
	return &service.Group{
		Name: name, Platform: service.PlatformOpenAI,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
		DisplayIcon: icon, DisplayCategory: category,
	}
}

func requireDisplayGroupFields(t *testing.T, group *service.Group, icon, category string) {
	t.Helper()
	require.NotNil(t, group)
	require.Equal(t, icon, group.DisplayIcon)
	require.Equal(t, category, group.DisplayCategory)
}

func TestGroupRepoDisplayReadPathsExcludeAuthProjection(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	keyRepo := newAPIKeyRepositoryWithSQL(tx.Client(), tx)
	group := newDisplayTestGroup("group display read paths", "gemini", "domestic")
	require.NoError(t, repo.Create(ctx, group))

	detail, err := repo.GetByID(ctx, group.ID)
	require.NoError(t, err)
	requireDisplayGroupFields(t, detail, "gemini", "domestic")

	params := pagination.PaginationParams{Page: 1, PageSize: 20}
	adminGroups, _, err := repo.ListWithFilters(ctx, params, "", "", group.Name, nil)
	require.NoError(t, err)
	require.Len(t, adminGroups, 1)
	require.Equal(t, group.ID, adminGroups[0].ID)
	requireDisplayGroupFields(t, &adminGroups[0], "gemini", "domestic")

	activeGroups, err := repo.ListActive(ctx)
	require.NoError(t, err)
	var activeGroup *service.Group
	for i := range activeGroups {
		if activeGroups[i].ID == group.ID {
			activeGroup = &activeGroups[i]
			break
		}
	}
	requireDisplayGroupFields(t, activeGroup, "gemini", "domestic")

	user := mustCreateUser(t, tx.Client(), &service.User{})
	key := mustCreateApiKey(t, tx.Client(), &service.APIKey{UserID: user.ID, GroupID: &group.ID})
	keys, _, err := keyRepo.ListByUserID(ctx, user.ID, params, service.APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, key.ID, keys[0].ID)
	requireDisplayGroupFields(t, keys[0].Group, "gemini", "domestic")

	authKey, err := keyRepo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	requireDisplayGroupFields(t, authKey.Group, "", "")
	require.Equal(t, group.ID, authKey.Group.ID)
}

func TestGroupRepoDisplayWriteAndClear(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	group := newDisplayTestGroup("group display updates", "gemini", "domestic")
	require.NoError(t, repo.Create(ctx, group))

	read := func(icon, category string) *service.Group {
		t.Helper()
		stored, err := repo.GetByID(ctx, group.ID)
		require.NoError(t, err)
		requireDisplayGroupFields(t, stored, icon, category)
		return stored
	}
	stored := read("gemini", "domestic")
	stored.DisplayIcon, stored.DisplayCategory = "deepseek", "gemini"
	require.NoError(t, repo.Update(ctx, stored))

	stored = read("deepseek", "gemini")
	stored.Name = "group display renamed"
	require.NoError(t, repo.Update(ctx, stored))

	stored = read("deepseek", "gemini")
	require.Equal(t, "group display renamed", stored.Name)
	stored.DisplayIcon, stored.DisplayCategory = "", ""
	require.NoError(t, repo.Update(ctx, stored))
	read("", "")
}

func TestGroupRepoDisplayDuplicatePersistsOriginalValues(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	legacyIcon := strings.Repeat("图", 32)
	source := newDisplayTestGroup("group display duplicate source", legacyIcon, "domestic")
	require.NoError(t, repo.Create(ctx, source))

	duplicate := newDisplayTestGroup("group display duplicate copy", source.DisplayIcon, source.DisplayCategory)
	duplicate.Status = "inactive"
	require.NoError(t, repo.CreateFromSource(ctx, duplicate, source.ID))
	require.NotEqual(t, duplicate.ID, source.ID)
	storedCopy, err := repo.GetByID(ctx, duplicate.ID)
	require.NoError(t, err)
	requireDisplayGroupFields(t, storedCopy, legacyIcon, "domestic")
	storedSource, err := repo.GetByID(ctx, source.ID)
	require.NoError(t, err)
	requireDisplayGroupFields(t, storedSource, legacyIcon, "domestic")
}
