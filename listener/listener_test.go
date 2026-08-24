package listener

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	IN "github.com/Miku0139oao/aster-core/listener/inbound"

	"github.com/stretchr/testify/require"
)

type patchTestConfig string

func (c patchTestConfig) Name() string { return string(c) }
func (c patchTestConfig) Equal(other C.InboundConfig) bool {
	value, ok := other.(patchTestConfig)
	return ok && value == c
}

type patchTestListener struct {
	name                   string
	config                 patchTestConfig
	address                string
	active                 map[string]*patchTestListener
	events                 *[]string
	listenErr              error
	startBeforeListenError bool
	closeErr               error
	listening              bool
	listenCall             int
	closeCall              int
}

func (l *patchTestListener) Name() string            { return l.name }
func (l *patchTestListener) Address() string         { return l.address }
func (l *patchTestListener) RawAddress() string      { return l.address }
func (l *patchTestListener) Config() C.InboundConfig { return l.config }
func (l *patchTestListener) Listen(C.Tunnel) error {
	l.listenCall++
	if l.events != nil {
		*l.events = append(*l.events, "listen:"+l.name)
	}
	if l.listenErr != nil && !l.startBeforeListenError {
		return l.listenErr
	}
	if l.address != "" && l.active != nil {
		if owner := l.active[l.address]; owner != nil && owner != l {
			return fmt.Errorf("address %s already in use by %s", l.address, owner.name)
		}
		l.active[l.address] = l
		l.listening = true
	}
	return l.listenErr
}

func (l *patchTestListener) Close() error {
	l.closeCall++
	if l.events != nil {
		*l.events = append(*l.events, "close:"+l.name)
	}
	if l.listening && l.active != nil && l.active[l.address] == l {
		delete(l.active, l.address)
	}
	l.listening = false
	return l.closeErr
}

func activatePatchTestListener(l *patchTestListener) {
	if l.address != "" && l.active != nil {
		l.active[l.address] = l
		l.listening = true
	}
}

func replaceInboundListenersForTest(t *testing.T, listeners map[string]C.InboundListener) {
	t.Helper()
	inboundMux.Lock()
	previous := inboundListeners
	previousUnusable := unusableInboundListeners
	inboundListeners = listeners
	unusableInboundListeners = map[string]struct{}{}
	inboundMux.Unlock()
	t.Cleanup(func() {
		inboundMux.Lock()
		inboundListeners = previous
		unusableInboundListeners = previousUnusable
		inboundMux.Unlock()
	})
}

func TestPatchInboundListenersPreclosesInDeterministicOrderForAddressTransfer(t *testing.T) {
	var events []string
	active := make(map[string]*patchTestListener)
	oldB := &patchTestListener{name: "old-b", config: "old-b", address: "address-b", active: active, events: &events}
	oldZ := &patchTestListener{name: "old-z", config: "old-z", address: "transferred-address", active: active, events: &events}
	activatePatchTestListener(oldB)
	activatePatchTestListener(oldZ)
	replaceInboundListenersForTest(t, map[string]C.InboundListener{"z": oldZ, "b": oldB})

	newA := &patchTestListener{name: "new-a", config: "new-a", address: "transferred-address", active: active, events: &events}
	newB := &patchTestListener{name: "new-b", config: "new-b", address: "address-b", active: active, events: &events}
	err := PatchInboundListeners(map[string]C.InboundListener{"b": newB, "a": newA}, nil, true)

	require.NoError(t, err)
	require.Equal(t, []string{"close:old-b", "close:old-z", "listen:new-a", "listen:new-b"}, events)
	require.Same(t, newA, inboundListeners["a"])
	require.Same(t, newB, inboundListeners["b"])
	require.NotContains(t, inboundListeners, "z")
}

func TestPatchInboundListenersFailedListenRestoresOnlyCleanedReplacements(t *testing.T) {
	var events []string
	active := make(map[string]*patchTestListener)
	oldA := &patchTestListener{name: "old-a", config: "old-a", address: "address-a", active: active, events: &events}
	oldB := &patchTestListener{name: "old-b", config: "old-b", address: "address-b", active: active, events: &events}
	activatePatchTestListener(oldA)
	activatePatchTestListener(oldB)
	replaceInboundListenersForTest(t, map[string]C.InboundListener{"b": oldB, "a": oldA})

	newA := &patchTestListener{
		name:    "new-a",
		config:  "new-a",
		address: "address-a",
		active:  active,
		events:  &events,
	}
	newB := &patchTestListener{
		name:                   "new-b",
		config:                 "new-b",
		address:                "address-b",
		active:                 active,
		events:                 &events,
		listenErr:              errors.New("listen b failed"),
		startBeforeListenError: true,
		closeErr:               errors.New("cleanup b failed"),
	}
	err := PatchInboundListeners(map[string]C.InboundListener{"b": newB, "a": newA}, nil, true)

	require.ErrorContains(t, err, "listen b failed")
	require.ErrorContains(t, err, "cleanup b failed")
	require.ErrorContains(t, err, "replacement cleanup failed")
	require.Equal(t, []string{
		"close:old-a", "close:old-b",
		"listen:new-a", "listen:new-b",
		"close:new-b", "close:new-a",
		"listen:old-a",
	}, events)
	require.Equal(t, 1, newB.closeCall)
	require.Equal(t, 1, oldB.closeCall)
	require.Same(t, oldA, inboundListeners["a"])
	require.Same(t, newB, inboundListeners["b"])
}

func TestPatchInboundListenersAggregatesPrecloseErrors(t *testing.T) {
	var events []string
	oldA := &patchTestListener{name: "old-a", config: "old-a", events: &events, closeErr: errors.New("close a failed")}
	oldB := &patchTestListener{name: "old-b", config: "old-b", events: &events, closeErr: errors.New("close b failed")}
	replaceInboundListenersForTest(t, map[string]C.InboundListener{"b": oldB, "a": oldA})
	added := &patchTestListener{name: "added", config: "added", events: &events}

	err := PatchInboundListeners(map[string]C.InboundListener{"added": added}, nil, true)

	require.ErrorContains(t, err, "close a failed")
	require.ErrorContains(t, err, "close b failed")
	require.Equal(t, []string{"close:old-a", "close:old-b"}, events)
	require.Zero(t, added.listenCall)
	require.Same(t, oldA, inboundListeners["a"])
	require.Same(t, oldB, inboundListeners["b"])
}

func TestPatchInboundListenersKeepsReplacementWhenRollbackCloseFails(t *testing.T) {
	old := &patchTestListener{name: "service", config: "old"}
	replaceInboundListenersForTest(t, map[string]C.InboundListener{"service": old})
	replacement := &patchTestListener{
		name:                   "service",
		config:                 "new",
		listenErr:              errors.New("listen failed"),
		startBeforeListenError: true,
		closeErr:               errors.New("close failed"),
	}

	err := PatchInboundListeners(map[string]C.InboundListener{"service": replacement}, nil, true)

	require.ErrorContains(t, err, "listen failed")
	require.ErrorContains(t, err, "close failed")
	require.Same(t, replacement, inboundListeners["service"])
	require.Zero(t, old.listenCall)
}

func TestPatchInboundListenersRebuildsListenerLeftUnusableByRollback(t *testing.T) {
	active := make(map[string]*patchTestListener)
	old := &patchTestListener{name: "service", config: "old", address: "address", active: active}
	activatePatchTestListener(old)
	replaceInboundListenersForTest(t, map[string]C.InboundListener{"service": old})
	stuck := &patchTestListener{
		name:                   "service",
		config:                 "new",
		address:                "address",
		active:                 active,
		listenErr:              errors.New("listen failed"),
		startBeforeListenError: true,
		closeErr:               errors.New("close failed"),
	}

	require.Error(t, PatchInboundListeners(map[string]C.InboundListener{"service": stuck}, nil, true))
	require.Same(t, stuck, inboundListeners["service"])

	// The registered listener is not serving, so an identical config must not be
	// mistaken for a healthy listener that can be reused: the next patch has to
	// retry closing it and bind a fresh one.
	stuck.closeErr = nil
	replacement := &patchTestListener{name: "service", config: "new", address: "address", active: active}
	require.NoError(t, PatchInboundListeners(map[string]C.InboundListener{"service": replacement}, nil, true))
	require.Same(t, replacement, inboundListeners["service"])
	require.Equal(t, 2, stuck.closeCall)
	require.Equal(t, 1, replacement.listenCall)
	require.NotContains(t, unusableInboundListeners, "service")
}

type patchNetAddr string

func (a patchNetAddr) Network() string { return "tcp" }
func (a patchNetAddr) String() string  { return string(a) }

type patchNetListener struct {
	closed    chan struct{}
	closeOnce sync.Once
	closeCall int
}

func newPatchNetListener() *patchNetListener {
	return &patchNetListener{closed: make(chan struct{})}
}

func (l *patchNetListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *patchNetListener) Close() error {
	l.closeCall++
	closed := false
	l.closeOnce.Do(func() {
		closed = true
		close(l.closed)
	})
	if !closed {
		return net.ErrClosed
	}
	return nil
}
func (l *patchNetListener) Addr() net.Addr { return patchNetAddr("127.0.0.1:0") }

type patchListenConfig struct {
	listenCall int
	failListen map[int]error
	listeners  []*patchNetListener
}

func (c *patchListenConfig) Listen(context.Context, string, string) (net.Listener, error) {
	c.listenCall++
	if err := c.failListen[c.listenCall]; err != nil {
		return nil, err
	}
	listener := newPatchNetListener()
	c.listeners = append(c.listeners, listener)
	return listener, nil
}

func (*patchListenConfig) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, errors.New("unexpected packet listener")
}

func TestSliceBackedInboundCleansPartialListenAndRestarts(t *testing.T) {
	lc := &patchListenConfig{failListen: map[int]error{2: errors.New("second listen failed")}}
	in, err := IN.NewHTTP(&IN.HTTPOption{BaseOption: IN.BaseOption{
		NameStr:            "http",
		Listen:             "127.0.0.1",
		Port:               "10000-10001",
		ListenConfigForAPI: lc,
	}})
	require.NoError(t, err)

	err = in.Listen(nil)
	require.ErrorContains(t, err, "second listen failed")
	require.Len(t, lc.listeners, 1)
	require.Equal(t, 1, lc.listeners[0].closeCall)
	require.NoError(t, in.Close())
	require.Equal(t, 1, lc.listeners[0].closeCall)

	lc.failListen = nil
	require.NoError(t, in.Listen(nil))
	require.NoError(t, in.Close())
	require.NoError(t, in.Close())
	require.NoError(t, in.Listen(nil))
	require.NoError(t, in.Close())
	for _, listener := range lc.listeners {
		require.Equal(t, 1, listener.closeCall)
	}
}

func TestSliceBackedInboundCleansChildConstructorFailure(t *testing.T) {
	lc := &patchListenConfig{}
	in, err := IN.NewHTTP(&IN.HTTPOption{
		BaseOption: IN.BaseOption{
			NameStr:            "http",
			Listen:             "127.0.0.1",
			Port:               "10000",
			ListenConfigForAPI: lc,
		},
		Certificate: "invalid certificate",
		PrivateKey:  "invalid private key",
	})
	require.NoError(t, err)

	require.Error(t, in.Listen(nil))
	require.Len(t, lc.listeners, 1)
	require.Equal(t, 1, lc.listeners[0].closeCall)
	require.NoError(t, in.Close())
	require.Equal(t, 1, lc.listeners[0].closeCall)
}

func TestPatchInboundListenersRestoredSliceListenerCanCloseAndRestart(t *testing.T) {
	lc := &patchListenConfig{}
	old, err := IN.NewHTTP(&IN.HTTPOption{BaseOption: IN.BaseOption{
		NameStr:            "service",
		Listen:             "127.0.0.1",
		Port:               "10000",
		ListenConfigForAPI: lc,
	}})
	require.NoError(t, err)
	require.NoError(t, old.Listen(nil))
	replaceInboundListenersForTest(t, map[string]C.InboundListener{"service": old})

	replacement := &patchTestListener{name: "replacement", config: "replacement", listenErr: errors.New("replacement failed")}
	err = PatchInboundListeners(map[string]C.InboundListener{"service": replacement}, nil, true)
	require.ErrorContains(t, err, "replacement failed")
	require.Equal(t, 1, replacement.closeCall)
	require.Same(t, old, inboundListeners["service"])
	require.Len(t, lc.listeners, 2)
	require.Equal(t, 1, lc.listeners[0].closeCall)

	require.NoError(t, old.Close())
	require.NoError(t, old.Close())
	require.NoError(t, old.Listen(nil))
	require.NoError(t, old.Close())
	for _, listener := range lc.listeners {
		require.Equal(t, 1, listener.closeCall)
	}
}

type tunLifecycleTestListener struct {
	config      LC.Tun
	address     string
	reloadCalls int
	closeCalls  int
	closeErr    error
}

func (l *tunLifecycleTestListener) OnReload()       { l.reloadCalls++ }
func (l *tunLifecycleTestListener) Config() LC.Tun  { return l.config }
func (l *tunLifecycleTestListener) Address() string { return l.address }
func (l *tunLifecycleTestListener) Close() error {
	l.closeCalls++
	return l.closeErr
}

func replaceTunLifecycleForTest(t *testing.T, config LC.Tun, current tunListener, factory func(LC.Tun, C.Tunnel) (tunListener, error)) {
	t.Helper()
	tunMux.Lock()
	previousConfig := LastTunConf
	previousListener := tunLister
	previousFactory := newTunListener
	LastTunConf = config
	tunLister = current
	newTunListener = factory
	tunMux.Unlock()
	t.Cleanup(func() {
		tunMux.Lock()
		LastTunConf = previousConfig
		tunLister = previousListener
		newTunListener = previousFactory
		tunMux.Unlock()
	})
}

func TestReCreateTunRetriesEnabledSameConfigWithoutListener(t *testing.T) {
	config := LC.Tun{Enable: true, Device: "same-config"}
	creationErr := errors.New("requested creation failed")
	calls := 0
	replaceTunLifecycleForTest(t, config, nil, func(got LC.Tun, _ C.Tunnel) (tunListener, error) {
		calls++
		require.True(t, got.Equal(config))
		return nil, creationErr
	})

	err := ReCreateTun(config, nil)

	require.ErrorIs(t, err, creationErr)
	require.Equal(t, 1, calls)
	require.Nil(t, tunLister)
	require.True(t, LastTunConf.Equal(config))
}

func TestReCreateTunRestoresPreviousListenerAfterReplacementFailure(t *testing.T) {
	previousConfig := LC.Tun{Enable: true, Device: "previous"}
	requestedConfig := LC.Tun{Enable: true, Device: "requested"}
	previousListener := &tunLifecycleTestListener{config: previousConfig, address: "previous"}
	restoredListener := &tunLifecycleTestListener{config: previousConfig, address: "restored"}
	creationErr := errors.New("requested creation failed")
	var calls []LC.Tun
	replaceTunLifecycleForTest(t, previousConfig, previousListener, func(config LC.Tun, _ C.Tunnel) (tunListener, error) {
		calls = append(calls, config)
		if len(calls) == 1 {
			return nil, creationErr
		}
		return restoredListener, nil
	})

	err := ReCreateTun(requestedConfig, nil)

	require.ErrorIs(t, err, creationErr)
	require.Len(t, calls, 2)
	require.True(t, calls[0].Equal(requestedConfig))
	require.True(t, calls[1].Equal(previousConfig))
	require.Equal(t, 1, previousListener.closeCalls)
	require.Same(t, restoredListener, tunLister)
	require.True(t, LastTunConf.Equal(previousConfig))
}

func TestReCreateTunStopsWhenPreviousListenerCloseFails(t *testing.T) {
	previousConfig := LC.Tun{Enable: true, Device: "previous"}
	requestedConfig := LC.Tun{Enable: true, Device: "requested"}
	closeErr := errors.New("close failed")
	previousListener := &tunLifecycleTestListener{config: previousConfig, closeErr: closeErr}
	factoryCalls := 0
	replaceTunLifecycleForTest(t, previousConfig, previousListener, func(LC.Tun, C.Tunnel) (tunListener, error) {
		factoryCalls++
		return &tunLifecycleTestListener{}, nil
	})

	err := ReCreateTun(requestedConfig, nil)

	require.ErrorIs(t, err, closeErr)
	require.Equal(t, 1, previousListener.closeCalls)
	require.Zero(t, factoryCalls)
	require.Same(t, previousListener, tunLister)
	require.True(t, LastTunConf.Equal(previousConfig))
}

func TestReCreateTunJoinsRequestedAndRestoreFailures(t *testing.T) {
	previousConfig := LC.Tun{Enable: true, Device: "previous"}
	requestedConfig := LC.Tun{Enable: true, Device: "requested"}
	previousListener := &tunLifecycleTestListener{config: previousConfig}
	creationErr := errors.New("requested creation failed")
	restoreErr := errors.New("restore failed")
	calls := 0
	replaceTunLifecycleForTest(t, previousConfig, previousListener, func(LC.Tun, C.Tunnel) (tunListener, error) {
		calls++
		if calls == 1 {
			return nil, creationErr
		}
		return nil, restoreErr
	})

	err := ReCreateTun(requestedConfig, nil)

	require.ErrorIs(t, err, creationErr)
	require.ErrorIs(t, err, restoreErr)
	require.ErrorContains(t, err, "restore previous TUN listener")
	require.Equal(t, 2, calls)
	require.Nil(t, tunLister)
	require.True(t, LastTunConf.Equal(previousConfig))
}

type recreateStubTunnel struct{}

func (recreateStubTunnel) HandleTCPConn(net.Conn, *C.Metadata)      {}
func (recreateStubTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (recreateStubTunnel) NatTable() C.NatTable                     { return nil }

func TestReCreateRedirRetriesUDPWithoutReplacingTCP(t *testing.T) {
	redirMux.Lock()
	oldTCP, oldUDP := redirListener, redirUDPListener
	redirListener, redirUDPListener = nil, nil
	redirMux.Unlock()
	t.Cleanup(func() {
		ReCreateRedir(0, recreateStubTunnel{})
		redirMux.Lock()
		if redirListener != nil {
			_ = redirListener.Close()
		}
		if redirUDPListener != nil {
			_ = redirUDPListener.Close()
		}
		redirListener, redirUDPListener = oldTCP, oldUDP
		redirMux.Unlock()
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	prevAllow, prevBind := allowLan, bindAddress
	SetAllowLan(false)
	SetBindAddress("*")
	t.Cleanup(func() {
		allowLan, bindAddress = prevAllow, prevBind
	})

	ReCreateRedir(port, recreateStubTunnel{})
	require.NotNil(t, redirListener)
	firstTCP := redirListener

	ReCreateRedir(port, recreateStubTunnel{})
	require.Same(t, firstTCP, redirListener)
	require.Equal(t, port, GetPorts().RedirPort)
}

func TestReCreateTProxyRetriesUDPIndependently(t *testing.T) {
	tproxyMux.Lock()
	oldTCP, oldUDP := tproxyListener, tproxyUDPListener
	tproxyListener, tproxyUDPListener = nil, nil
	tproxyMux.Unlock()
	t.Cleanup(func() {
		ReCreateTProxy(0, recreateStubTunnel{})
		tproxyMux.Lock()
		if tproxyListener != nil {
			_ = tproxyListener.Close()
		}
		if tproxyUDPListener != nil {
			_ = tproxyUDPListener.Close()
		}
		tproxyListener, tproxyUDPListener = oldTCP, oldUDP
		tproxyMux.Unlock()
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	prevAllow, prevBind := allowLan, bindAddress
	SetAllowLan(false)
	SetBindAddress("*")
	t.Cleanup(func() {
		allowLan, bindAddress = prevAllow, prevBind
	})

	ReCreateTProxy(port, recreateStubTunnel{})
	firstTCP := tproxyListener
	ReCreateTProxy(port, recreateStubTunnel{})
	if firstTCP != nil {
		require.Same(t, firstTCP, tproxyListener)
	}
}
