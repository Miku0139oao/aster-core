package session

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecycleSessionDoesNotReinsertClosedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	session := NewClientSession(discardConn{}, nil, "")
	session.seq = 1
	session.Close()

	client.recycleSession(session)
	require.True(t, client.idleSession.IsEmpty())
}

func TestGetIdleSessionSkipsClosedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := NewClient(ctx, nil, nil, "", 0, 0, 0, false)
	defer client.Close()

	session := NewClientSession(discardConn{}, nil, "")
	session.seq = 1
	session.Close()

	client.idleSessionLock.Lock()
	client.idleSession.Insert(math.MaxUint64-session.seq, session)
	client.idleSessionLock.Unlock()

	require.Nil(t, client.getIdleSession())
	require.True(t, client.idleSession.IsEmpty())
}
