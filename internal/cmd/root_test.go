package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestTmuxSocketPathIsPerDirectory(t *testing.T) {
	t.Parallel()

	a := tmuxSocketPath("/tmp/project-a")
	b := tmuxSocketPath("/tmp/project-b")
	c := tmuxSocketPath("/tmp/project-a")

	require.NotEqual(t, a, b)
	require.Equal(t, a, c)
}

func TestRequestedCwd(t *testing.T) {
	t.Parallel()

	t.Run("uses explicit cwd flag", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")
		require.NoError(t, cmd.Flags().Set("cwd", "/tmp/doris"))

		cwd, err := requestedCwd(cmd, func() (string, error) {
			return "/tmp/ignored", nil
		})
		require.NoError(t, err)
		require.Equal(t, "/tmp/doris", cwd)
	})

	t.Run("falls back to getwd", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("cwd", "", "")

		cwd, err := requestedCwd(cmd, func() (string, error) {
			return "/tmp/fallback", nil
		})
		require.NoError(t, err)
		require.Equal(t, "/tmp/fallback", cwd)
	})
}

func TestBuildInnerTmuxArgs(t *testing.T) {
	t.Parallel()

	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().Bool("yolo", false, "")
		cmd.Flags().String("cwd", "", "")
		cmd.Flags().String("data-dir", "", "")
		cmd.Flags().Bool("debug", false, "")
		cmd.Flags().String("session", "", "")
		cmd.Flags().Bool("continue", false, "")
		return cmd
	}

	t.Run("preserves explicit continue", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()
		require.NoError(t, cmd.Flags().Set("continue", "true"))

		require.Equal(t, []string{"--continue"}, buildInnerTmuxArgs(cmd))
	})

	t.Run("does not force continue by default", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()

		require.Empty(t, buildInnerTmuxArgs(cmd))
	})

	t.Run("prefers session over continue", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()
		require.NoError(t, cmd.Flags().Set("continue", "true"))
		require.NoError(t, cmd.Flags().Set("session", "sess-123"))

		require.Equal(t, []string{"--session", "sess-123"}, buildInnerTmuxArgs(cmd))
	})

	t.Run("preserves cwd in tmux args", func(t *testing.T) {
		t.Parallel()

		cmd := newCmd()
		require.NoError(t, cmd.Flags().Set("cwd", "/tmp/doris"))

		require.Equal(t, []string{"--cwd", "/tmp/doris"}, buildInnerTmuxArgs(cmd))
	})
}
