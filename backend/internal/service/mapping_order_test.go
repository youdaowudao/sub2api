//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSparkShadowMappingOrderPreserved(t *testing.T) {
	order := []any{"z", "a"}
	credentials := map[string]any{
		"model_mapping":               map[string]any{"z": "z", "a": "a"},
		"compact_model_mapping":       map[string]any{"z": "compact-z", "a": "compact-a"},
		"model_mapping_order":         order,
		"compact_model_mapping_order": order,
		"not_allowed":                 true,
	}
	sanitized := sanitizeSparkShadowCredentials(credentials)
	require.Equal(t, order, sanitized["model_mapping_order"])
	require.Equal(t, order, sanitized["compact_model_mapping_order"])
	require.NotContains(t, sanitized, "not_allowed")
}

func TestSparkShadowMappingOrderDoesNotChangeResolution(t *testing.T) {
	for _, order := range [][]string{nil, {"a*", "ab*", "abc"}, {"abc", "ab*", "a*"}} {
		account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{
			"model_mapping":       map[string]any{"a*": "generic", "ab*": "specific", "abc": "exact"},
			"model_mapping_order": order,
		}}
		require.Equal(t, "exact", account.GetMappedModel("abc"))
		require.Equal(t, "specific", account.GetMappedModel("abcd"))
		mapped, matched := account.ResolveMappedModel("abcd")
		require.True(t, matched)
		require.Equal(t, "specific", mapped)
	}
}

func TestChannelMappingOrderUpdateFollowsMapping(t *testing.T) {
	mapping := map[string]map[string]string{PlatformOpenAI: {"z": "target-z", "a": "target-a"}}
	order := map[string][]string{PlatformOpenAI: {"z", "a"}}
	ch := &Channel{ModelMapping: mapping, ModelMappingOrder: order}
	svc := &ChannelService{}
	require.NoError(t, svc.applyUpdateInput(context.Background(), ch, &UpdateChannelInput{ModelMappingOrder: map[string][]string{PlatformOpenAI: {"a", "z"}}}))
	require.Equal(t, order, ch.ModelMappingOrder)
	require.NoError(t, svc.applyUpdateInput(context.Background(), ch, &UpdateChannelInput{ModelMapping: mapping}))
	require.Nil(t, ch.ModelMappingOrder)
	require.NoError(t, svc.applyUpdateInput(context.Background(), ch, &UpdateChannelInput{ModelMapping: map[string]map[string]string{}, ModelMappingOrder: map[string][]string{}}))
	require.NotNil(t, ch.ModelMappingOrder)
	require.Empty(t, ch.ModelMappingOrder)
	require.Empty(t, ch.ModelMapping)
}

func TestGroupRoutingOrderDoesNotChangeExactRouting(t *testing.T) {
	for _, order := range [][]string{nil, {"z", "a"}, {"a", "z"}} {
		group := Group{ModelRoutingEnabled: true, ModelRouting: map[string][]int64{"a": {1}, "z": {2, 3}}, ModelRoutingOrder: order}
		require.Equal(t, []int64{2, 3}, group.GetRoutingAccountIDs("z"))
	}
}

func TestGroupRoutingOrderUpdateFollowsMapping(t *testing.T) {
	mapping := map[string][]int64{"z": {22}, "a": {11}}
	order := []string{"z", "a"}
	repo := &groupPlatformRepoStub{group: &Group{ID: 7, Name: "fixture", Platform: PlatformAnthropic, ModelRouting: mapping, ModelRoutingOrder: order}}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{ModelRoutingOrder: []string{"a", "z"}})
	require.NoError(t, err)
	require.Equal(t, order, repo.updated.ModelRoutingOrder)
	_, err = svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{ModelRouting: mapping})
	require.NoError(t, err)
	require.Nil(t, repo.updated.ModelRoutingOrder)
	_, err = svc.UpdateGroup(context.Background(), 7, &UpdateGroupInput{ModelRouting: map[string][]int64{}, ModelRoutingOrder: []string{}})
	require.NoError(t, err)
	require.Equal(t, []string{}, repo.updated.ModelRoutingOrder)
	require.Equal(t, map[string][]int64{}, repo.updated.ModelRouting)
}

func TestChannelMappingOrderDoesNotChangeResolution(t *testing.T) {
	for _, order := range []map[string][]string{nil, {PlatformOpenAI: {"z", "a"}}, {PlatformOpenAI: {"a", "z"}}} {
		channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{10}, ModelMapping: map[string]map[string]string{PlatformOpenAI: {"z": "target-z", "a": "target-a"}}, ModelMappingOrder: order}
		svc := &ChannelService{}
		svc.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{10: PlatformOpenAI}))
		result := svc.ResolveChannelMapping(context.Background(), 10, "z")
		require.True(t, result.Mapped)
		require.Equal(t, "target-z", result.MappedModel)
	}
}
