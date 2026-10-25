package dto

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGroupDisplayOrderProjection(t *testing.T) {
	for _, order := range []int{0, -10, 20} {
		g := &service.Group{ID: 7, SortOrder: order, ProfitControlEnabled: true, ProfitMinMargin: .2, ModelRoutingEnabled: true}
		for _, dto := range []any{GroupFromService(g), GroupFromServiceShallow(g), APIKeyFromService(&service.APIKey{Group: g}).Group, AccountFromService(&service.Account{Groups: []*service.Group{g}}).Groups[0]} {
			data, err := json.Marshal(dto)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(data, &fields))
			require.Equal(t, float64(order), fields["sort_order"])
			for _, private := range []string{"profit_control_enabled", "profit_min_margin", "model_routing", "account_count"} {
				require.NotContains(t, fields, private)
			}
		}
		require.Equal(t, order, GroupFromServiceAdmin(g).SortOrder)
	}
}
