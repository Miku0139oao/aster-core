package inbound

import (
	"errors"
	"sort"
	"strings"
	"sync"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener/anytls"
	LC "github.com/Miku0139oao/aster-core/listener/config"
	"github.com/Miku0139oao/aster-core/log"
)

type AnyTLSOption struct {
	BaseOption
	Users          map[string]string `inbound:"users,omitempty"`
	Certificate    string            `inbound:"certificate,omitempty"`
	PrivateKey     string            `inbound:"private-key,omitempty"`
	ClientAuthType string            `inbound:"client-auth-type,omitempty"`
	ClientAuthCert string            `inbound:"client-auth-cert,omitempty"`
	EchKey         string            `inbound:"ech-key,omitempty"`
	ShadowTLS      ShadowTLS         `inbound:"shadow-tls,omitempty"`
	ResTLS         ResTLS            `inbound:"res-tls,omitempty"`
	JLSConfig      JLSConfig         `inbound:"jls-config,omitempty"`
	RealityConfig  RealityConfig     `inbound:"reality-config,omitempty"`
	AllowInsecure  bool              `inbound:"allow-insecure,omitempty"`
	PaddingScheme  string            `inbound:"padding-scheme,omitempty"`
}

func (o AnyTLSOption) Equal(config C.InboundConfig) bool {
	return optionToString(o) == optionToString(config)
}

type AnyTLS struct {
	*Base
	config             *AnyTLSOption
	l                  *anytls.Listener
	vs                 LC.AnyTLSServer
	managedMu          sync.RWMutex
	managedUsers       []C.ManagedUser
	managedUsersStaged bool
}

func NewAnyTLS(options *AnyTLSOption) (*AnyTLS, error) {
	base, err := NewBase(&options.BaseOption)
	if err != nil {
		return nil, err
	}
	listener := &AnyTLS{
		Base:   base,
		config: options,
		vs: LC.AnyTLSServer{
			Enable:         true,
			Listen:         base.RawAddress(),
			Users:          options.Users,
			Certificate:    options.Certificate,
			PrivateKey:     options.PrivateKey,
			ClientAuthType: options.ClientAuthType,
			ClientAuthCert: options.ClientAuthCert,
			EchKey:         options.EchKey,
			ShadowTLS:      options.ShadowTLS.Build(),
			ResTLS:         options.ResTLS.Build(),
			JLSConfig:      options.JLSConfig.Build(),
			RealityConfig:  options.RealityConfig.Build(),
			AllowInsecure:  options.AllowInsecure,
			PaddingScheme:  options.PaddingScheme,
		},
	}
	listener.managedUsers = listener.ConfiguredUsers()
	return listener, nil
}

// Config implements constant.InboundListener
func (v *AnyTLS) Config() C.InboundConfig {
	return v.config
}

// Address implements constant.InboundListener
func (v *AnyTLS) Address() string {
	var addrList []string
	if v.l != nil {
		for _, addr := range v.l.AddrList() {
			addrList = append(addrList, addr.String())
		}
	}
	return strings.Join(addrList, ",")
}

// Listen implements constant.InboundListener
func (v *AnyTLS) Listen(tunnel C.Tunnel) error {
	v.managedMu.Lock()
	defer v.managedMu.Unlock()

	managedUsers := append([]C.ManagedUser(nil), v.managedUsers...)
	if v.managedUsersStaged {
		managedUsers = nil
	}
	users, err := anyTLSManagedUsers(managedUsers)
	if err != nil {
		return err
	}
	server := v.vs
	server.Users = users
	listener, err := anytls.New(server, v.ListenConfig(), tunnel, v.Additions()...)
	if err != nil {
		return err
	}
	v.l = listener
	v.managedUsers = managedUsers
	v.managedUsersStaged = false
	log.Infoln("AnyTLS[%s] proxy listening at: %s", v.Name(), v.Address())
	return nil
}

// Close implements constant.InboundListener
func (v *AnyTLS) Close() error {
	l := v.l
	v.l = nil
	if l == nil {
		return nil
	}
	return l.Close()
}

func (v *AnyTLS) UpdateUsers(users map[string]string) error {
	if v.l == nil {
		return errors.New("AnyTLS listener is not running")
	}
	return v.l.UpdateUsers(users)
}

func (v *AnyTLS) ManagedUserSchema() C.ManagedUserSchema {
	return C.ManagedUserSchema{Protocol: "anytls", Credential: "password"}
}

func (v *AnyTLS) ConfiguredUsers() []C.ManagedUser {
	users := make([]C.ManagedUser, 0, len(v.config.Users))
	for name, password := range v.config.Users {
		users = append(users, C.ManagedUser{
			PrincipalID: name,
			Name:        name,
			Password:    password,
		})
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Name < users[j].Name
	})
	return users
}

func (v *AnyTLS) UpdateManagedUsers(users []C.ManagedUser) error {
	updated, err := anyTLSManagedUsers(users)
	if err != nil {
		return err
	}
	v.managedMu.Lock()
	defer v.managedMu.Unlock()

	if !sameAnyTLSManagedCredentials(v.managedUsers, users) {
		if err := v.UpdateUsers(updated); err != nil {
			return err
		}
	}
	v.managedUsers = append([]C.ManagedUser(nil), users...)
	v.managedUsersStaged = false
	return nil
}

func sameAnyTLSManagedCredentials(first, second []C.ManagedUser) bool {
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
		if firstID != secondID || first[i].Password != second[i].Password {
			return false
		}
	}
	return true
}

func (v *AnyTLS) StageManagedUsers() {
	v.managedMu.Lock()
	v.managedUsersStaged = true
	v.managedMu.Unlock()
}

func (v *AnyTLS) CurrentManagedUsers() []C.ManagedUser {
	v.managedMu.RLock()
	defer v.managedMu.RUnlock()
	return append([]C.ManagedUser(nil), v.managedUsers...)
}

func anyTLSManagedUsers(users []C.ManagedUser) (map[string]string, error) {
	updated := make(map[string]string, len(users))
	for _, user := range users {
		principalID := user.PrincipalID
		if principalID == "" {
			principalID = user.Name
		}
		if _, exists := updated[principalID]; exists {
			return nil, errors.New("duplicate AnyTLS principal ID: " + principalID)
		}
		updated[principalID] = user.Password
	}
	return updated, nil
}

var (
	_ C.InboundListener     = (*AnyTLS)(nil)
	_ C.ManagedUserListener = (*AnyTLS)(nil)
	_ C.ManagedUserStager   = (*AnyTLS)(nil)
)
