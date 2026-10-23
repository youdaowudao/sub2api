package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginCreationContractMissingStoreDoesNotBreakExistingUser(t *testing.T) {
	ctx := WithUserCreationCapture(context.Background(), nil)
	TrackUserCreation(ctx, 71)
	require.NoError(t, CompleteUserCreation(ctx, 72), "an unrelated existing user must not depend on completion storage")
	require.Error(t, CompleteUserCreation(ctx, 71), "an actual new creation cannot silently bypass missing storage")
}
