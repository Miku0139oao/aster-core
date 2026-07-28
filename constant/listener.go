package constant

import (
	"context"
	"net"
)

type Listener interface {
	RawAddress() string
	Address() string
	Close() error
}

type MultiAddrListener interface {
	Close() error
	Config() string
	AddrList() (addrList []net.Addr)
}

type InboundListener interface {
	Name() string
	Listen(tunnel Tunnel) error
	Close() error
	Address() string
	RawAddress() string
	Config() InboundConfig
}

type ManagedUser struct {
	PrincipalID string `json:"principal-id"`
	Name        string `json:"name"`
	UUID        string `json:"uuid,omitempty"`
	Password    string `json:"password,omitempty"`
	Flow        string `json:"flow,omitempty"`
}

type ManagedUserSchema struct {
	Protocol   string `json:"protocol"`
	Credential string `json:"credential"`
	Flow       bool   `json:"flow"`
}

type ManagedUserListener interface {
	InboundListener
	ManagedUserSchema() ManagedUserSchema
	ConfiguredUsers() []ManagedUser
	CurrentManagedUsers() []ManagedUser
	UpdateManagedUsers(users []ManagedUser) error
}

type ManagedUserStager interface {
	StageManagedUsers()
}

type InboundConfig interface {
	Name() string
	Equal(config InboundConfig) bool
}

type InboundListenConfig interface {
	Listen(ctx context.Context, network, address string) (net.Listener, error)
	ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error)
}
