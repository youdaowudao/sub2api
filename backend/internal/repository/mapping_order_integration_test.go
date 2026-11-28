//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestChannelMappingOrderRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := NewChannelRepository(integrationDB)
	svc := service.NewChannelService(repo, nil, nil, nil, nil)
	mapping := map[string]map[string]string{service.PlatformOpenAI: {"z": "target-z", "a": "target-a"}}
	order := map[string][]string{service.PlatformOpenAI: {"z", "a"}}
	channel, err := svc.Create(ctx, &service.CreateChannelInput{Name: "mapping-order-" + uuid.NewString(), ModelMapping: mapping, ModelMappingOrder: order})
	require.NoError(t, err)
	id := channel.ID
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, id)) })
	require.Equal(t, order, channel.ModelMappingOrder)
	rows, _, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, "", channel.Name)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, order, rows[0].ModelMappingOrder)
	all, err := repo.ListAll(ctx)
	require.NoError(t, err)
	found := false
	for _, item := range all {
		if item.ID == id {
			found = true
			require.Nil(t, item.ModelMappingOrder)
			require.Equal(t, mapping, item.ModelMapping)
		}
	}
	require.True(t, found)
	reverse := map[string][]string{service.PlatformOpenAI: {"a", "z"}}
	channel, err = svc.Update(ctx, id, &service.UpdateChannelInput{ModelMapping: mapping, ModelMappingOrder: reverse})
	require.NoError(t, err)
	require.Equal(t, reverse, channel.ModelMappingOrder)
	channel, err = svc.Update(ctx, id, &service.UpdateChannelInput{ModelMappingOrder: order})
	require.NoError(t, err)
	require.Equal(t, reverse, channel.ModelMappingOrder)
	channel, err = svc.Update(ctx, id, &service.UpdateChannelInput{ModelMapping: mapping})
	require.NoError(t, err)
	require.Nil(t, channel.ModelMappingOrder)
	var isNull bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT model_mapping_order IS NULL FROM channels WHERE id=$1", id).Scan(&isNull))
	require.True(t, isNull)
	channel, err = svc.Update(ctx, id, &service.UpdateChannelInput{ModelMapping: map[string]map[string]string{}, ModelMappingOrder: map[string][]string{}})
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]string{}, channel.ModelMapping)
	require.Equal(t, map[string][]string{}, channel.ModelMappingOrder)
}

func TestGroupRoutingOrderRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newGroupRepositoryWithSQL(tx.Client(), tx)
	group := newDisplayTestGroup("routing-order-"+uuid.NewString(), "", "")
	group.ModelRouting = map[string][]int64{"z": {22}, "a": {11}}
	group.ModelRoutingOrder = []string{"z", "a"}
	require.NoError(t, repo.Create(ctx, group))
	id := group.ID
	stored, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, group.ModelRouting, stored.ModelRouting)
	require.Equal(t, group.ModelRoutingOrder, stored.ModelRoutingOrder)
	stored.ModelRoutingOrder = []string{"a", "z"}
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "z"}, stored.ModelRoutingOrder)
	copyGroup := newDisplayTestGroup("routing-copy-"+uuid.NewString(), "", "")
	copyGroup.ModelRouting, copyGroup.ModelRoutingOrder = stored.ModelRouting, stored.ModelRoutingOrder
	require.NoError(t, repo.CreateFromSource(ctx, copyGroup, id))
	copied, err := repo.GetByID(ctx, copyGroup.ID)
	require.NoError(t, err)
	require.Equal(t, stored.ModelRoutingOrder, copied.ModelRoutingOrder)
	stored.ModelRoutingOrder = nil
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Nil(t, stored.ModelRoutingOrder)
	var isNull bool
	rows, err := tx.QueryContext(ctx, "SELECT model_routing_order IS NULL FROM groups WHERE id=$1", id)
	require.NoError(t, err)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&isNull))
	require.NoError(t, rows.Close())
	require.True(t, isNull)
	stored.ModelRouting, stored.ModelRoutingOrder = map[string][]int64{}, []string{}
	require.NoError(t, repo.Update(ctx, stored))
	stored, err = repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, map[string][]int64{}, stored.ModelRouting)
	require.Equal(t, []string{}, stored.ModelRoutingOrder)
}
