package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMCPSession_CancelOnClose(t *testing.T) {
	defer goleak.VerifyNone(t)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	server := mcp.NewServer(&mcp.Implementation{Name: "test-server"}, nil)
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Close()

	ctx, cancel := context.WithCancel(context.Background())

	client := mcp.NewClient(&mcp.Implementation{Name: "crush-test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	sess := &ClientSession{clientSession, cancel}

	// Verify the context is not cancelled before close.
	require.NoError(t, ctx.Err())

	err = sess.Close()
	require.NoError(t, err)

	// After Close, the context must be cancelled.
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestFilterSensitiveEnv(t *testing.T) {
	t.Parallel()

	env := []string{
		"HOME=/home/user",
		"PATH=/usr/bin",
		"MY_API_KEY=secret123",
		"AWS_SECRET_ACCESS_KEY=abc",
		"GITHUB_TOKEN=ghp_xxx",
		"DB_PASSWORD=hunter2",
		"OAUTH_CREDENTIAL=cred",
		"SSH_PRIVATE_KEY=key",
		"JWT_SIGNING_KEY=sign",
		"DATA_ENCRYPTION_KEY=enc",
		"AWS_ACCESS_KEY_ID=AKIA",
		"GPG_PASSPHRASE=pass",
		"BASIC_AUTH_HEADER=basic",
		"DATABASE_URL=postgres://user:pass@host/db",
		"REDIS_CONNECTION_STRING=redis://host",
		"SENTRY_DSN=https://key@sentry.io/1",
		"EDITOR=vim",
		"LANG=en_US.UTF-8",
	}

	filtered := filterSensitiveEnv(env)

	allowed := map[string]bool{
		"HOME":  true,
		"PATH":  true,
		"EDITOR": true,
		"LANG":  true,
	}

	for _, entry := range filtered {
		key, _, _ := strings.Cut(entry, "=")
		if !allowed[key] {
			t.Errorf("sensitive env var %q was not filtered", key)
		}
	}

	require.Len(t, filtered, len(allowed))
}
