package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupMapperDisplayFields(t *testing.T) {
	group := &service.Group{
		Platform: service.PlatformOpenAI, DisplayIcon: " gemini ", DisplayCategory: "未登记分类",
	}
	for name, dto := range map[string]any{
		"user": GroupFromService(group), "admin": GroupFromServiceAdmin(group),
	} {
		t.Run(name, func(t *testing.T) {
			fields := marshalToMap(t, dto)
			require.Equal(t, " gemini ", fields["display_icon"])
			require.Equal(t, "未登记分类", fields["display_category"])
			require.Equal(t, "openai", fields["platform"])
		})
	}
}
