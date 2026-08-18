package kerneldirect

import (
	"io"
	"net/netip"
	"sort"
	"sync"
	"time"

	"go4.org/netipx"
)

const (
	DefaultEBPFMark        = uint32(0x40000000)
	DefaultEBPFProxyMark   = uint32(0x20000000)
	DefaultEBPFMaxEntries  = uint32(65536)
	DefaultEBPFFlowEntries = uint32(65536)
)

// FastPathOptions configures the optional TC eBPF DIRECT classifier. The
// nftables address set remains active as a fail-safe and for locally generated
// traffic which does not traverse a LAN TC ingress hook.
type FastPathOptions struct {
	Interfaces []string
	// ProxyRedirectInterface enables a TC-to-TUN redirect for PROXY decisions.
	// An empty value preserves the nftables mark shim used by the compatibility
	// path. The original auto-redirect rules stay installed as fail-open backup.
	ProxyRedirectInterface string
	Mark                   uint32
	ProxyMark              uint32
	InputMark              uint32
	MaxEntries             uint32
	FlowEntries            uint32
	ProxySteering          bool
	DirectPrefixes         []netip.Prefix
	ProxyPrefixes          []netip.Prefix
	TableName              string
}

// DecisionSets contains DNS-learned route decisions. Proxy entries win over
// DIRECT entries at the same prefix, while longest-prefix matching preserves
// explicit, more-specific decisions over a configured default route.
type DecisionSets struct {
	Direct *netipx.IPSet
	Proxy  *netipx.IPSet
}

// FastPathStatus is intentionally small so it can be exposed through logs and
// the controller without leaking map internals.
type FastPathStatus struct {
	Backend                string    `json:"backend"`
	RequestedInterfaces    []string  `json:"requested-interfaces,omitempty"`
	Interfaces             []string  `json:"interfaces"`
	ProxyRedirectInterface string    `json:"proxy-redirect-interface,omitempty"`
	Mark                   uint32    `json:"mark"`
	ProxyMark              uint32    `json:"proxy-mark,omitempty"`
	InputMark              uint32    `json:"input-mark,omitempty"`
	IPv4                   int       `json:"ipv4"`
	IPv6                   int       `json:"ipv6"`
	DirectPrefixes         int       `json:"direct-prefixes,omitempty"`
	ProxyPrefixes          int       `json:"proxy-prefixes,omitempty"`
	BypassPrefixes         int       `json:"bypass-prefixes,omitempty"`
	FlowMaxEntries         uint32    `json:"flow-max-entries,omitempty"`
	Packets                uint64    `json:"packets"`
	Bytes                  uint64    `json:"bytes"`
	DirectPackets          uint64    `json:"direct-packets,omitempty"`
	DirectBytes            uint64    `json:"direct-bytes,omitempty"`
	ProxyPackets           uint64    `json:"proxy-packets,omitempty"`
	ProxyBytes             uint64    `json:"proxy-bytes,omitempty"`
	FlowHits               uint64    `json:"flow-hits,omitempty"`
	UpdatedAt              time.Time `json:"updated-at"`
	LastError              string    `json:"last-error,omitempty"`
}

// FastPath classifies learned DIRECT and PROXY destinations at TC ingress.
// Replace must preserve proxy-wins transitions and invalidate cached flows
// before publishing a new generation.
type FastPath interface {
	io.Closer
	Replace(DecisionSets) error
	Status() FastPathStatus
}

var activeFastPaths = struct {
	sync.RWMutex
	paths map[FastPath]struct{}
}{paths: make(map[FastPath]struct{})}

func registerFastPath(path FastPath) {
	activeFastPaths.Lock()
	activeFastPaths.paths[path] = struct{}{}
	activeFastPaths.Unlock()
}

func unregisterFastPath(path FastPath) {
	activeFastPaths.Lock()
	delete(activeFastPaths.paths, path)
	activeFastPaths.Unlock()
}

// FastPathStatuses returns a stable snapshot for the controller API.
func FastPathStatuses() []FastPathStatus {
	activeFastPaths.RLock()
	paths := make([]FastPath, 0, len(activeFastPaths.paths))
	for path := range activeFastPaths.paths {
		paths = append(paths, path)
	}
	activeFastPaths.RUnlock()
	statuses := make([]FastPathStatus, 0, len(paths))
	for _, path := range paths {
		statuses = append(statuses, path.Status())
	}
	sort.Slice(statuses, func(i, j int) bool {
		return len(statuses[i].Interfaces) > 0 && (len(statuses[j].Interfaces) == 0 || statuses[i].Interfaces[0] < statuses[j].Interfaces[0])
	})
	return statuses
}
