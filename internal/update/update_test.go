package update

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckForUpdate_Old(t *testing.T) {
	info, err := Check(t.Context(), "v0.10.0", time.Time{}, testClient{"v0.11.0"})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.True(t, info.Available())
}

func TestCheckForUpdate_Beta(t *testing.T) {
	t.Run("current is stable", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.10.0", time.Time{}, testClient{"v0.11.0-beta.1"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.False(t, info.Available())
	})

	t.Run("current is also beta", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.11.0-beta.1", time.Time{}, testClient{"v0.11.0-beta.2"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.True(t, info.Available())
	})

	t.Run("current is beta, latest isn't", func(t *testing.T) {
		info, err := Check(t.Context(), "v0.11.0-beta.1", time.Time{}, testClient{"v0.11.0"})
		require.NoError(t, err)
		require.NotNil(t, info)
		require.True(t, info.Available())
	})
}

func TestCheckForUpdate_DevBuild(t *testing.T) {
	releaseTime := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)

	t.Run("dev build older than release", func(t *testing.T) {
		buildTime := releaseTime.Add(-1 * time.Hour)
		info, err := Check(t.Context(), "devel", buildTime, testClient{"v0.56.0"})
		require.NoError(t, err)
		require.True(t, info.Available())
	})

	t.Run("dev build newer than release", func(t *testing.T) {
		buildTime := releaseTime.Add(1 * time.Hour)
		info, err := Check(t.Context(), "devel", buildTime, testClient{"v0.56.0"})
		require.NoError(t, err)
		require.False(t, info.Available())
	})

	t.Run("dev build no build time", func(t *testing.T) {
		info, err := Check(t.Context(), "devel", time.Time{}, testClient{"v0.56.0"})
		require.NoError(t, err)
		require.True(t, info.Available())
	})
}

type testClient struct{ tag string }

// Latest implements Client.
func (tc testClient) Latest(_ context.Context) (*Release, error) {
	return &Release{
		TagName:     tc.tag,
		HTMLURL:     "https://example.org",
		PublishedAt: time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC),
	}, nil
}
