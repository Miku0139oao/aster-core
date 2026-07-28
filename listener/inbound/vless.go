package inbound

import (
	"errors"
	"strings"
	"sync"

	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/listener/sing_vless"
	"github.com/Miku0139oao/aster-core/log"
)

type VlessOption struct {
	BaseOption
	Users           []VlessUser   `inbound:"users"`
	Decryption      string        `inbound:"decryption,omitempty"`
	WsPath          string        `inbound:"ws-path,omitempty"`
	XHTTPConfig     XHTTPConfig   `inbound:"xhttp-config,omitempty"`
	GrpcServiceName string        `inbound:"grpc-service-name,omitempty"`
	Certificate     string        `inbound:"certificate,omitempty"`
	PrivateKey      string        `inbound:"private-key,omitempty"`
	ClientAuthType  string        `inbound:"client-auth-type,omitempty"`
	ClientAuthCert  string        `inbound:"client-auth-cert,omitempty"`
	EchKey          string        `inbound:"ech-key,omitempty"`
	AllowInsecure   bool          `inbound:"allow-insecure,omitempty"`
	ShadowTLS       ShadowTLS     `inbound:"shadow-tls,omitempty"`
	ResTLS          ResTLS        `inbound:"res-tls,omitempty"`
	JLSConfig       JLSConfig     `inbound:"jls-config,omitempty"`
	RealityConfig   RealityConfig `inbound:"reality-config,omitempty"`
	MuxOption       MuxOption     `inbound:"mux-option,omitempty"`
}

type VlessUser struct {
	Username string `inbound:"username,omitempty"`
	UUID     string `inbound:"uuid"`
	Flow     string `inbound:"flow,omitempty"`
}

type XHTTPConfig struct {
	Path                 string `inbound:"path,omitempty"`
	Host                 string `inbound:"host,omitempty"`
	Mode                 string `inbound:"mode,omitempty"`
	XPaddingBytes        string `inbound:"x-padding-bytes,omitempty"`
	XPaddingObfsMode     bool   `inbound:"x-padding-obfs-mode,omitempty"`
	XPaddingKey          string `inbound:"x-padding-key,omitempty"`
	XPaddingHeader       string `inbound:"x-padding-header,omitempty"`
	XPaddingPlacement    string `inbound:"x-padding-placement,omitempty"`
	XPaddingMethod       string `inbound:"x-padding-method,omitempty"`
	UplinkHTTPMethod     string `inbound:"uplink-http-method,omitempty"`
	SessionPlacement     string `inbound:"session-placement,omitempty"`
	SessionKey           string `inbound:"session-key,omitempty"`
	SeqPlacement         string `inbound:"seq-placement,omitempty"`
	SeqKey               string `inbound:"seq-key,omitempty"`
	UplinkDataPlacement  string `inbound:"uplink-data-placement,omitempty"`
	UplinkDataKey        string `inbound:"uplink-data-key,omitempty"`
	UplinkChunkSize      string `inbound:"uplink-chunk-size,omitempty"`
	NoSSEHeader          bool   `inbound:"no-sse-header,omitempty"`
	ScStreamUpServerSecs string `inbound:"sc-stream-up-server-secs,omitempty"`
	ScMaxBufferedPosts   string `inbound:"sc-max-buffered-posts,omitempty"`
	ScMaxEachPostBytes   string `inbound:"sc-max-each-post-bytes,omitempty"`
}

func (o XHTTPConfig) Build() LC.XHTTPConfig {
	return LC.XHTTPConfig{
		Path:                 o.Path,
		Host:                 o.Host,
		Mode:                 o.Mode,
		NoSSEHeader:          o.NoSSEHeader,
		XPaddingBytes:        o.XPaddingBytes,
		XPaddingObfsMode:     o.XPaddingObfsMode,
		XPaddingKey:          o.XPaddingKey,
		XPaddingHeader:       o.XPaddingHeader,
		XPaddingPlacement:    o.XPaddingPlacement,
		XPaddingMethod:       o.XPaddingMethod,
		UplinkHTTPMethod:     o.UplinkHTTPMethod,
		SessionPlacement:     o.SessionPlacement,
		SessionKey:           o.SessionKey,
		SeqPlacement:         o.SeqPlacement,
		SeqKey:               o.SeqKey,
		UplinkDataPlacement:  o.UplinkDataPlacement,
		UplinkDataKey:        o.UplinkDataKey,
		UplinkChunkSize:      o.UplinkChunkSize,
		ScStreamUpServerSecs: o.ScStreamUpServerSecs,
		ScMaxBufferedPosts:   o.ScMaxBufferedPosts,
		ScMaxEachPostBytes:   o.ScMaxEachPostBytes,
	}
}

func (o VlessOption) Equal(config C.InboundConfig) bool {
	return optionToString(o) == optionToString(config)
}

type Vless struct {
	*Base
	config             *VlessOption
	l                  *sing_vless.Listener
	vs                 LC.VlessServer
	managedMu          sync.RWMutex
	managedUsers       []C.ManagedUser
	managedUsersStaged bool
}

func NewVless(options *VlessOption) (*Vless, error) {
	base, err := NewBase(&options.BaseOption)
	if err != nil {
		return nil, err
	}
	users := make([]LC.VlessUser, len(options.Users))
	for i, v := range options.Users {
		users[i] = LC.VlessUser{
			Username: v.Username,
			UUID:     v.UUID,
			Flow:     v.Flow,
		}
	}
	listener := &Vless{
		Base:   base,
		config: options,
		vs: LC.VlessServer{
			Enable:          true,
			Listen:          base.RawAddress(),
			Users:           users,
			Decryption:      options.Decryption,
			WsPath:          options.WsPath,
			XHTTPConfig:     options.XHTTPConfig.Build(),
			GrpcServiceName: options.GrpcServiceName,
			Certificate:     options.Certificate,
			PrivateKey:      options.PrivateKey,
			ClientAuthType:  options.ClientAuthType,
			ClientAuthCert:  options.ClientAuthCert,
			EchKey:          options.EchKey,
			AllowInsecure:   options.AllowInsecure,
			ShadowTLS:       options.ShadowTLS.Build(),
			ResTLS:          options.ResTLS.Build(),
			JLSConfig:       options.JLSConfig.Build(),
			RealityConfig:   options.RealityConfig.Build(),
			MuxOption:       options.MuxOption.Build(),
		},
	}
	listener.managedUsers = listener.ConfiguredUsers()
	return listener, nil
}

// Config implements constant.InboundListener
func (v *Vless) Config() C.InboundConfig {
	return v.config
}

// Address implements constant.InboundListener
func (v *Vless) Address() string {
	var addrList []string
	if v.l != nil {
		for _, addr := range v.l.AddrList() {
			addrList = append(addrList, addr.String())
		}
	}
	return strings.Join(addrList, ",")
}

// Listen implements constant.InboundListener
func (v *Vless) Listen(tunnel C.Tunnel) error {
	v.managedMu.Lock()
	defer v.managedMu.Unlock()

	users := append([]C.ManagedUser(nil), v.managedUsers...)
	if v.managedUsersStaged {
		users = nil
	}
	server := v.vs
	server.Users = vlessManagedUsers(users)
	listener, err := sing_vless.New(server, v.ListenConfig(), tunnel, v.Additions()...)
	if err != nil {
		return err
	}
	v.l = listener
	v.managedUsers = users
	v.managedUsersStaged = false
	log.Infoln("Vless[%s] proxy listening at: %s", v.Name(), v.Address())
	return nil
}

// Close implements constant.InboundListener
func (v *Vless) Close() error {
	l := v.l
	v.l = nil
	if l == nil {
		return nil
	}
	return l.Close()
}

func (v *Vless) UpdateUsers(users []LC.VlessUser) error {
	if v.l == nil {
		return errors.New("VLESS listener is not running")
	}
	return v.l.UpdateUsers(users)
}

func (v *Vless) ManagedUserSchema() C.ManagedUserSchema {
	return C.ManagedUserSchema{Protocol: "vless", Credential: "uuid", Flow: true}
}

func (v *Vless) ConfiguredUsers() []C.ManagedUser {
	users := make([]C.ManagedUser, len(v.config.Users))
	for i, user := range v.config.Users {
		users[i] = C.ManagedUser{
			PrincipalID: user.Username,
			Name:        user.Username,
			UUID:        user.UUID,
			Flow:        user.Flow,
		}
	}
	return users
}

func (v *Vless) UpdateManagedUsers(users []C.ManagedUser) error {
	v.managedMu.Lock()
	defer v.managedMu.Unlock()

	if !sameVlessManagedCredentials(v.managedUsers, users) {
		if err := v.UpdateUsers(vlessManagedUsers(users)); err != nil {
			return err
		}
	}
	v.managedUsers = append([]C.ManagedUser(nil), users...)
	v.managedUsersStaged = false
	return nil
}

func sameVlessManagedCredentials(first, second []C.ManagedUser) bool {
	if len(first) != len(second) {
		return false
	}
	for i := range first {
		firstID, secondID := first[i].PrincipalID, second[i].PrincipalID
		if firstID == "" {
			firstID = first[i].Name
		}
		if secondID == "" {
			secondID = second[i].Name
		}
		if firstID != secondID || first[i].UUID != second[i].UUID || first[i].Flow != second[i].Flow {
			return false
		}
	}
	return true
}

func (v *Vless) StageManagedUsers() {
	v.managedMu.Lock()
	v.managedUsersStaged = true
	v.managedMu.Unlock()
}

func (v *Vless) CurrentManagedUsers() []C.ManagedUser {
	v.managedMu.RLock()
	defer v.managedMu.RUnlock()
	return append([]C.ManagedUser(nil), v.managedUsers...)
}

func vlessManagedUsers(users []C.ManagedUser) []LC.VlessUser {
	updated := make([]LC.VlessUser, len(users))
	for i, user := range users {
		principalID := user.PrincipalID
		if principalID == "" {
			principalID = user.Name
		}
		updated[i] = LC.VlessUser{Username: principalID, UUID: user.UUID, Flow: user.Flow}
	}
	return updated
}

var _ C.InboundListener = (*Vless)(nil)
var _ C.ManagedUserListener = (*Vless)(nil)
var _ C.ManagedUserStager = (*Vless)(nil)
