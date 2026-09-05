package sing_tun

import (
	"context"
	"testing"
	"time"

	tun "github.com/metacubex/sing-tun"
	"github.com/stretchr/testify/require"
)

// fakeTunStack is a device-free tun.Stack whose Close observes whether the
// listener context was already cancelled — the same ctx passed to StackOptions.
type fakeTunStack struct {
	ctx              context.Context
	sawCancelOnClose bool
}

func (s *fakeTunStack) Start() error { return nil }

func (s *fakeTunStack) Close() error {
	select {
	case <-s.ctx.Done():
		s.sawCancelOnClose = true
	default:
	}
	return nil
}

var _ tun.Stack = (*fakeTunStack)(nil)

func newTestListener(t *testing.T) *Listener {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	return &Listener{ctx: ctx, cancel: cancel}
}

func TestCloseCancelsListenerContext(t *testing.T) {
	l := newTestListener(t)
	require.NoError(t, l.ctx.Err())
	require.NoError(t, l.Close())
	require.ErrorIs(t, l.ctx.Err(), context.Canceled)
}

func TestCloseNilCancelPartialListener(t *testing.T) {
	l := &Listener{}
	require.NoError(t, l.Close())
	require.True(t, l.closed)
}

func TestCloseRepeatedCancelSafe(t *testing.T) {
	l := newTestListener(t)
	require.NoError(t, l.Close())
	require.NoError(t, l.Close())
	require.ErrorIs(t, l.ctx.Err(), context.Canceled)
}

func TestCloseCancelsBeforeDependentResourceClose(t *testing.T) {
	l := newTestListener(t)
	stack := &fakeTunStack{ctx: l.ctx}
	l.tunStack = stack
	require.NoError(t, l.Close())
	require.True(t, stack.sawCancelOnClose, "cancel must run before tunStack.Close")
	require.ErrorIs(t, l.ctx.Err(), context.Canceled)
}

func TestOnReloadDoesNotCancel(t *testing.T) {
	l := newTestListener(t)
	l.OnReload()
	require.NoError(t, l.ctx.Err())
	require.NoError(t, l.Close())
	require.ErrorIs(t, l.ctx.Err(), context.Canceled)
}

// TestCloseUnblocksDependentContextWaiter starts a waiter on Listener.ctx using
// the same primitive as sing-tun mixed packetLoop (channel.ReadContext selects
// ctx.Done()) and TCPNat.loopCheckTimeout. It is device-free and does not claim
// mixed-stack integration.
func TestCloseUnblocksDependentContextWaiter(t *testing.T) {
	l := newTestListener(t)

	started := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		close(started)
		select {
		case <-l.ctx.Done():
		case <-time.After(2 * time.Second):
			t.Error("dependent ctx waiter still blocked after Close")
		}
	}()
	<-started
	require.NoError(t, l.Close())
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("dependent ctx waiter did not exit after Close")
	}
	require.ErrorIs(t, l.ctx.Err(), context.Canceled)
}
