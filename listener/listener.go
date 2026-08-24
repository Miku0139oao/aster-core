package listener

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/http"
	"github.com/Miku0139oao/aster-core/listener/mixed"
	"github.com/Miku0139oao/aster-core/listener/redir"
	embedSS "github.com/Miku0139oao/aster-core/listener/shadowsocks"
	"github.com/Miku0139oao/aster-core/listener/sing_shadowsocks"
	"github.com/Miku0139oao/aster-core/listener/sing_tun"
	"github.com/Miku0139oao/aster-core/listener/sing_vmess"
	"github.com/Miku0139oao/aster-core/listener/socks"
	"github.com/Miku0139oao/aster-core/listener/tproxy"
	"github.com/Miku0139oao/aster-core/listener/tuic"
	LT "github.com/Miku0139oao/aster-core/listener/tunnel"
	"github.com/Miku0139oao/aster-core/log"

	"github.com/samber/lo"
)

var (
	allowLan    = false
	bindAddress = "*"

	socksListener      *socks.Listener
	socksUDPListener   *socks.UDPListener
	httpListener       *http.Listener
	redirListener      *redir.Listener
	redirUDPListener   *tproxy.UDPListener
	tproxyListener     *tproxy.Listener
	tproxyUDPListener  *tproxy.UDPListener
	mixedListener      *mixed.Listener
	mixedUDPLister     *socks.UDPListener
	tunnelTCPListeners = map[string]*LT.Listener{}
	tunnelUDPListeners = map[string]*LT.PacketConn{}
	inboundListeners   = map[string]C.InboundListener{}
	// Names registered in inboundListeners that are not serving, because a failed
	// patch could not release them. Guarded by inboundMux.
	unusableInboundListeners = map[string]struct{}{}
	tunLister                tunListener
	newTunListener           = func(config LC.Tun, tunnel C.Tunnel) (tunListener, error) { return sing_tun.New(config, tunnel) }
	shadowSocksListener      C.MultiAddrListener
	vmessListener            *sing_vmess.Listener
	tuicListener             *tuic.Listener

	// lock for recreate function
	socksMux   sync.Mutex
	httpMux    sync.Mutex
	redirMux   sync.Mutex
	tproxyMux  sync.Mutex
	mixedMux   sync.Mutex
	tunnelMux  sync.Mutex
	inboundMux sync.Mutex
	tunMux     sync.Mutex
	ssMux      sync.Mutex
	vmessMux   sync.Mutex
	tuicMux    sync.Mutex

	LastTunConf  LC.Tun
	LastTuicConf LC.TuicServer
)

type tunListener interface {
	OnReload()
	Close() error
	Config() LC.Tun
	Address() string
}

type Ports struct {
	Port              int    `json:"port"`
	SocksPort         int    `json:"socks-port"`
	RedirPort         int    `json:"redir-port"`
	TProxyPort        int    `json:"tproxy-port"`
	MixedPort         int    `json:"mixed-port"`
	ShadowSocksConfig string `json:"ss-config"`
	VmessConfig       string `json:"vmess-config"`
}

func GetTunConf() LC.Tun {
	tunMux.Lock()
	defer tunMux.Unlock()
	if tunLister == nil {
		return LastTunConf.Clone()
	}
	return tunLister.Config().Clone()
}

func GetTuicConf() LC.TuicServer {
	if tuicListener == nil {
		return LC.TuicServer{Enable: false}
	}
	return tuicListener.Config()
}

func AllowLan() bool {
	return allowLan
}

func BindAddress() string {
	return bindAddress
}

func SetAllowLan(al bool) {
	allowLan = al
}

func SetBindAddress(host string) {
	bindAddress = host
}

func ReCreateHTTP(port int, tunnel C.Tunnel) {
	httpMux.Lock()
	defer httpMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start HTTP server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if httpListener != nil {
		if httpListener.RawAddress() == addr {
			return
		}
		httpListener.Close()
		httpListener = nil
	}

	if portIsZero(addr) {
		return
	}

	httpListener, err = http.New(addr, tunnel)
	if err != nil {
		log.Errorln("Start HTTP server error: %s", err.Error())
		return
	}

	log.Infoln("HTTP proxy listening at: %s", httpListener.Address())
}

func ReCreateSocks(port int, tunnel C.Tunnel) {
	socksMux.Lock()
	defer socksMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start SOCKS server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	shouldTCPIgnore := false
	shouldUDPIgnore := false

	if socksListener != nil {
		if socksListener.RawAddress() != addr {
			socksListener.Close()
			socksListener = nil
		} else {
			shouldTCPIgnore = true
		}
	}

	if socksUDPListener != nil {
		if socksUDPListener.RawAddress() != addr {
			socksUDPListener.Close()
			socksUDPListener = nil
		} else {
			shouldUDPIgnore = true
		}
	}

	if shouldTCPIgnore && shouldUDPIgnore {
		return
	}

	if portIsZero(addr) {
		return
	}

	tcpListener, err := socks.New(addr, tunnel)
	if err != nil {
		return
	}

	udpListener, err := socks.NewUDP(addr, tunnel)
	if err != nil {
		tcpListener.Close()
		return
	}

	socksListener = tcpListener
	socksUDPListener = udpListener

	log.Infoln("SOCKS proxy listening at: %s", socksListener.Address())
}

func ReCreateRedir(port int, tunnel C.Tunnel) {
	redirMux.Lock()
	defer redirMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start Redir server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if redirListener != nil {
		if redirListener.RawAddress() != addr {
			redirListener.Close()
			redirListener = nil
		}
	}

	if redirUDPListener != nil {
		if redirUDPListener.RawAddress() != addr {
			redirUDPListener.Close()
			redirUDPListener = nil
		}
	}

	if portIsZero(addr) {
		return
	}

	if redirListener == nil {
		redirListener, err = redir.New(addr, tunnel)
		if err != nil {
			return
		}
		log.Infoln("Redirect proxy listening at: %s", redirListener.Address())
	}

	if redirUDPListener == nil {
		var udpErr error
		redirUDPListener, udpErr = tproxy.NewUDP(addr, tunnel)
		if udpErr != nil {
			log.Warnln("Failed to start Redir UDP Listener: %s", udpErr)
		}
	}
}

func ReCreateShadowSocks(shadowSocksConfig string, tunnel C.Tunnel) {
	ssMux.Lock()
	defer ssMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start ShadowSocks server error: %s", err.Error())
		}
	}()

	var ssConfig LC.ShadowsocksServer
	if addr, cipher, password, err := embedSS.ParseSSURL(shadowSocksConfig); err == nil {
		ssConfig = LC.ShadowsocksServer{
			Enable:   len(shadowSocksConfig) > 0,
			Listen:   addr,
			Password: password,
			Cipher:   cipher,
			Udp:      true,
		}
	}

	shouldIgnore := false

	if shadowSocksListener != nil {
		if shadowSocksListener.Config() != ssConfig.String() {
			shadowSocksListener.Close()
			shadowSocksListener = nil
		} else {
			shouldIgnore = true
		}
	}

	if shouldIgnore {
		return
	}

	if !ssConfig.Enable {
		return
	}

	listener, err := sing_shadowsocks.New(ssConfig, inbound.NewListenConfig(), tunnel)
	if err != nil {
		return
	}

	shadowSocksListener = listener

	for _, addr := range shadowSocksListener.AddrList() {
		log.Infoln("ShadowSocks proxy listening at: %s", addr.String())
	}
	return
}

func ReCreateVmess(vmessConfig string, tunnel C.Tunnel) {
	vmessMux.Lock()
	defer vmessMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start Vmess server error: %s", err.Error())
		}
	}()

	var vsConfig LC.VmessServer
	if addr, username, password, err := sing_vmess.ParseVmessURL(vmessConfig); err == nil {
		vsConfig = LC.VmessServer{
			Enable: len(vmessConfig) > 0,
			Listen: addr,
			Users:  []LC.VmessUser{{Username: username, UUID: password, AlterID: 1}},
		}
	}

	shouldIgnore := false

	if vmessListener != nil {
		if vmessListener.Config() != vsConfig.String() {
			vmessListener.Close()
			vmessListener = nil
		} else {
			shouldIgnore = true
		}
	}

	if shouldIgnore {
		return
	}

	if !vsConfig.Enable {
		return
	}

	listener, err := sing_vmess.New(vsConfig, inbound.NewListenConfig(), tunnel)
	if err != nil {
		return
	}

	vmessListener = listener

	for _, addr := range vmessListener.AddrList() {
		log.Infoln("Vmess proxy listening at: %s", addr.String())
	}
	return
}

func ReCreateTuic(config LC.TuicServer, tunnel C.Tunnel) {
	tuicMux.Lock()
	defer func() {
		LastTuicConf = config
		tuicMux.Unlock()
	}()
	shouldIgnore := false

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start Tuic server error: %s", err.Error())
		}
	}()

	if tuicListener != nil {
		if tuicListener.Config().String() != config.String() {
			tuicListener.Close()
			tuicListener = nil
		} else {
			shouldIgnore = true
		}
	}

	if shouldIgnore {
		return
	}

	if !config.Enable {
		return
	}

	listener, err := tuic.New(config, inbound.NewListenConfig(), tunnel)
	if err != nil {
		return
	}

	tuicListener = listener

	for _, addr := range tuicListener.AddrList() {
		log.Infoln("Tuic proxy listening at: %s", addr.String())
	}
	return
}

func ReCreateTProxy(port int, tunnel C.Tunnel) {
	tproxyMux.Lock()
	defer tproxyMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start TProxy server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if tproxyListener != nil {
		if tproxyListener.RawAddress() != addr {
			tproxyListener.Close()
			tproxyListener = nil
		}
	}

	if tproxyUDPListener != nil {
		if tproxyUDPListener.RawAddress() != addr {
			tproxyUDPListener.Close()
			tproxyUDPListener = nil
		}
	}

	if portIsZero(addr) {
		return
	}

	if tproxyListener == nil {
		tproxyListener, err = tproxy.New(addr, tunnel)
		if err != nil {
			return
		}
		log.Infoln("TProxy server listening at: %s", tproxyListener.Address())
	}

	if tproxyUDPListener == nil {
		var udpErr error
		tproxyUDPListener, udpErr = tproxy.NewUDP(addr, tunnel)
		if udpErr != nil {
			log.Warnln("Failed to start TProxy UDP Listener: %s", udpErr)
		}
	}
}

func ReCreateMixed(port int, tunnel C.Tunnel) {
	mixedMux.Lock()
	defer mixedMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start Mixed(http+socks) server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	shouldTCPIgnore := false
	shouldUDPIgnore := false

	if mixedListener != nil {
		if mixedListener.RawAddress() != addr {
			mixedListener.Close()
			mixedListener = nil
		} else {
			shouldTCPIgnore = true
		}
	}
	if mixedUDPLister != nil {
		if mixedUDPLister.RawAddress() != addr {
			mixedUDPLister.Close()
			mixedUDPLister = nil
		} else {
			shouldUDPIgnore = true
		}
	}

	if shouldTCPIgnore && shouldUDPIgnore {
		return
	}

	if portIsZero(addr) {
		return
	}

	mixedListener, err = mixed.New(addr, tunnel)
	if err != nil {
		return
	}

	mixedUDPLister, err = socks.NewUDP(addr, tunnel)
	if err != nil {
		mixedListener.Close()
		return
	}

	log.Infoln("Mixed(http+socks) proxy listening at: %s", mixedListener.Address())
}

func ReCreateTun(tunConf LC.Tun, tunnel C.Tunnel) error {
	tunMux.Lock()
	defer tunMux.Unlock()
	return reCreateTunLocked(tunConf.Clone(), tunnel)
}

// PatchTun serializes snapshot, merge/validation, and activation with full
// configuration reloads. The callback receives a detached copy and may return
// a validation error without changing the live listener.
func PatchTun(build func(LC.Tun) (LC.Tun, error), tunnel C.Tunnel) error {
	tunMux.Lock()
	defer tunMux.Unlock()
	tunConf, err := build(LastTunConf.Clone())
	if err != nil {
		return err
	}
	return reCreateTunLocked(tunConf.Clone(), tunnel)
}

func reCreateTunLocked(tunConf LC.Tun, tunnel C.Tunnel) error {
	tunConf.Sort()
	if tunConf.Equal(LastTunConf) {
		if tunLister != nil { // some default value in dialer maybe changed when config reload, reset at here
			tunLister.OnReload()
			return nil
		}
		if !tunConf.Enable {
			return nil
		}
	}

	if !tunConf.Enable {
		if err := closeTunListenerLocked(); err != nil {
			return fmt.Errorf("close TUN listener: %w", err)
		}
		LastTunConf = tunConf
		return nil
	}

	// sing_tun.New cannot coexist with a live listener: the device name,
	// DefaultInterfaceFinder, and auto-redirect mark are exclusive. Close first
	// so a real reload can start, then restore the previous listener if New fails.
	// LastTunConf is only updated after a successful start so a failed recreate
	// cannot persist the new config or flip Enable to false.
	prevConf := LastTunConf.Clone()
	hadListener := tunLister != nil
	if err := closeTunListenerLocked(); err != nil {
		return fmt.Errorf("close previous TUN listener: %w", err)
	}

	lister, err := newTunListener(tunConf, tunnel)
	if err != nil {
		log.Errorln("Start TUN listening error: %s", err.Error())
		if hadListener {
			restored, restoreErr := newTunListener(prevConf, tunnel)
			if restoreErr != nil {
				contextualRestoreErr := fmt.Errorf("restore previous TUN listener: %w", restoreErr)
				log.Errorln("Restore TUN listening error: %s", restoreErr.Error())
				return errors.Join(err, contextualRestoreErr)
			}
			tunLister = restored
		}
		return err
	}
	tunLister = lister
	LastTunConf = tunConf
	log.Infoln("[TUN] Tun adapter listening at: %s", tunLister.Address())
	return nil
}

func PatchTunnel(tunnels []LC.Tunnel, tunnel C.Tunnel) {
	tunnelMux.Lock()
	defer tunnelMux.Unlock()

	type addrProxy struct {
		network string
		addr    string
		target  string
		proxy   string
	}

	tcpOld := lo.Map(
		lo.Keys(tunnelTCPListeners),
		func(key string, _ int) addrProxy {
			parts := strings.Split(key, "/")
			return addrProxy{
				network: "tcp",
				addr:    parts[0],
				target:  parts[1],
				proxy:   parts[2],
			}
		},
	)
	udpOld := lo.Map(
		lo.Keys(tunnelUDPListeners),
		func(key string, _ int) addrProxy {
			parts := strings.Split(key, "/")
			return addrProxy{
				network: "udp",
				addr:    parts[0],
				target:  parts[1],
				proxy:   parts[2],
			}
		},
	)
	oldElm := lo.Union(tcpOld, udpOld)

	newElm := lo.FlatMap(
		tunnels,
		func(tunnel LC.Tunnel, _ int) []addrProxy {
			return lo.Map(
				tunnel.Network,
				func(network string, _ int) addrProxy {
					return addrProxy{
						network: network,
						addr:    tunnel.Address,
						target:  tunnel.Target,
						proxy:   tunnel.Proxy,
					}
				},
			)
		},
	)

	needClose, needCreate := lo.Difference(oldElm, newElm)

	for _, elm := range needClose {
		key := fmt.Sprintf("%s/%s/%s", elm.addr, elm.target, elm.proxy)
		if elm.network == "tcp" {
			tunnelTCPListeners[key].Close()
			delete(tunnelTCPListeners, key)
		} else {
			tunnelUDPListeners[key].Close()
			delete(tunnelUDPListeners, key)
		}
	}

	lc := inbound.NewListenConfig()
	for _, elm := range needCreate {
		key := fmt.Sprintf("%s/%s/%s", elm.addr, elm.target, elm.proxy)
		if elm.network == "tcp" {
			l, err := LT.New(elm.addr, elm.target, elm.proxy, lc, tunnel)
			if err != nil {
				log.Errorln("Start tunnel %s error: %s", elm.target, err.Error())
				continue
			}
			tunnelTCPListeners[key] = l
			log.Infoln("Tunnel(tcp/%s) proxy %s listening at: %s", elm.target, elm.proxy, tunnelTCPListeners[key].Address())
		} else {
			l, err := LT.NewUDP(elm.addr, elm.target, elm.proxy, lc, tunnel)
			if err != nil {
				log.Errorln("Start tunnel %s error: %s", elm.target, err.Error())
				continue
			}
			tunnelUDPListeners[key] = l
			log.Infoln("Tunnel(udp/%s) proxy %s listening at: %s", elm.target, elm.proxy, tunnelUDPListeners[key].Address())
		}
	}
}

func PatchInboundListeners(newListenerMap map[string]C.InboundListener, tunnel C.Tunnel, dropOld bool) error {
	inboundMux.Lock()
	defer inboundMux.Unlock()
	type listenerPatch struct {
		name     string
		listener C.InboundListener
	}

	oldNames := make([]string, 0, len(inboundListeners))
	for name := range inboundListeners {
		oldNames = append(oldNames, name)
	}
	sort.Strings(oldNames)

	newNames := make([]string, 0, len(newListenerMap))
	for name := range newListenerMap {
		newNames = append(newNames, name)
	}
	sort.Strings(newNames)

	// A listener left registered by a failed rollback is not actually serving, so
	// an equal config must not be taken as evidence that it can be reused.
	reusable := func(name string, oldListener, newListener C.InboundListener) bool {
		if _, unusable := unusableInboundListeners[name]; unusable {
			return false
		}
		return oldListener.Config().Equal(newListener.Config())
	}

	stopped := make([]listenerPatch, 0, len(oldNames))
	for _, name := range oldNames {
		oldListener := inboundListeners[name]
		newListener, exists := newListenerMap[name]
		if exists && reusable(name, oldListener, newListener) {
			continue
		}
		if !exists && !dropOld {
			continue
		}
		stopped = append(stopped, listenerPatch{name: name, listener: oldListener})
	}

	replacements := make([]listenerPatch, 0, len(newNames))
	for _, name := range newNames {
		newListener := newListenerMap[name]
		if oldListener, exists := inboundListeners[name]; exists && reusable(name, oldListener, newListener) {
			continue
		}
		replacements = append(replacements, listenerPatch{name: name, listener: newListener})
	}

	started := make([]listenerPatch, 0, len(replacements))
	closed := make([]listenerPatch, 0, len(stopped))
	rollback := func(cause error) error {
		var rollbackErr error
		for i := len(started) - 1; i >= 0; i-- {
			patch := started[i]
			if err := patch.listener.Close(); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("close replacement listener %q: %w", patch.name, err))
				// The replacement still holds its address, so keep it registered to
				// retain the only handle able to release it later, and remember that
				// it is unusable so the next patch rebuilds it instead of matching
				// its config and assuming it is healthy.
				inboundListeners[patch.name] = patch.listener
				unusableInboundListeners[patch.name] = struct{}{}
				continue
			}
			delete(inboundListeners, patch.name)
			delete(unusableInboundListeners, patch.name)
		}
		for _, patch := range closed {
			if _, exists := inboundListeners[patch.name]; exists {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore listener %q: replacement cleanup failed", patch.name))
				continue
			}
			if err := patch.listener.Listen(tunnel); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore listener %q: %w", patch.name, err))
				if closeErr := patch.listener.Close(); closeErr != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("clean up unrestored listener %q: %w", patch.name, closeErr))
					inboundListeners[patch.name] = patch.listener
					unusableInboundListeners[patch.name] = struct{}{}
				}
				continue
			}
			inboundListeners[patch.name] = patch.listener
			delete(unusableInboundListeners, patch.name)
		}
		return errors.Join(cause, rollbackErr)
	}

	var closeErr error
	for _, patch := range stopped {
		if err := patch.listener.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close listener %q: %w", patch.name, err))
			continue
		}
		delete(inboundListeners, patch.name)
		delete(unusableInboundListeners, patch.name)
		closed = append(closed, patch)
	}
	if closeErr != nil {
		return rollback(closeErr)
	}

	for _, patch := range replacements {
		started = append(started, patch)
		if err := patch.listener.Listen(tunnel); err != nil {
			log.Errorln("Listener %s listen err: %s", patch.name, err.Error())
			return rollback(fmt.Errorf("listen on listener %q: %w", patch.name, err))
		}
	}
	for _, patch := range started {
		inboundListeners[patch.name] = patch.listener
		delete(unusableInboundListeners, patch.name)
	}
	return nil
}

var (
	ErrInboundListenerNotFound   = errors.New("inbound listener not found")
	ErrInboundListenerNotManaged = errors.New("inbound listener does not support managed users")
)

func WithManagedInboundListener(name string, fn func(C.ManagedUserListener) error) error {
	inboundMux.Lock()
	defer inboundMux.Unlock()

	inboundListener, exists := inboundListeners[name]
	if !exists {
		return fmt.Errorf("%w: %q", ErrInboundListenerNotFound, name)
	}
	managedListener, ok := inboundListener.(C.ManagedUserListener)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInboundListenerNotManaged, name)
	}
	return fn(managedListener)
}

func FailClosedManagedInboundListener(name string) error {
	inboundMux.Lock()
	defer inboundMux.Unlock()

	inboundListener, exists := inboundListeners[name]
	if !exists {
		return nil
	}
	managedListener, ok := inboundListener.(C.ManagedUserListener)
	if !ok {
		return nil
	}
	if err := managedListener.UpdateManagedUsers(nil); err == nil {
		return nil
	} else {
		closeErr := managedListener.Close()
		if closeErr == nil {
			delete(inboundListeners, name)
		}
		return errors.Join(err, closeErr)
	}
}

func ClearManagedInboundListeners(names []string) error {
	var clearErr error
	for _, name := range names {
		err := WithManagedInboundListener(name, func(managed C.ManagedUserListener) error {
			return managed.UpdateManagedUsers(nil)
		})
		if err == nil {
			continue
		}
		closeErr := FailClosedManagedInboundListener(name)
		clearErr = errors.Join(clearErr, fmt.Errorf("clear managed listener %q: %w", name, err), closeErr)
	}
	return clearErr
}

// GetPorts return the ports of proxy servers
func GetPorts() *Ports {
	ports := &Ports{}

	if httpListener != nil {
		_, portStr, _ := net.SplitHostPort(httpListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.Port = port
	}

	if socksListener != nil {
		_, portStr, _ := net.SplitHostPort(socksListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.SocksPort = port
	}

	if redirListener != nil {
		_, portStr, _ := net.SplitHostPort(redirListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.RedirPort = port
	}

	if tproxyListener != nil {
		_, portStr, _ := net.SplitHostPort(tproxyListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.TProxyPort = port
	}

	if mixedListener != nil {
		_, portStr, _ := net.SplitHostPort(mixedListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.MixedPort = port
	}

	if shadowSocksListener != nil {
		ports.ShadowSocksConfig = shadowSocksListener.Config()
	}

	if vmessListener != nil {
		ports.VmessConfig = vmessListener.Config()
	}

	return ports
}

func portIsZero(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if port == "0" || port == "" || err != nil {
		return true
	}
	return false
}

func genAddr(host string, port int, allowLan bool) string {
	if allowLan {
		if host == "*" {
			return fmt.Sprintf(":%d", port)
		}
		return fmt.Sprintf("%s:%d", host, port)
	}

	return fmt.Sprintf("127.0.0.1:%d", port)
}

func closeTunListenerLocked() error {
	if tunLister == nil {
		return nil
	}
	if err := tunLister.Close(); err != nil {
		return err
	}
	tunLister = nil
	return nil
}

func Cleanup() {
	tunMux.Lock()
	defer tunMux.Unlock()
	if err := closeTunListenerLocked(); err != nil {
		log.Warnln("Close TUN listener error: %s", err)
	}
}
