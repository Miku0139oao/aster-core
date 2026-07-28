package aster

import (
	"crypto/ecdh"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/listener"
	LI "github.com/Miku0139oao/aster-core/listener/inbound"
)

func (m *Manager) SubscriptionURL(userID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil {
		return "", ErrDisabled
	}
	if m.config.PublicBaseURL == "" {
		return "", nil
	}
	_, _, user := findUser(m.store, userID)
	if user == nil || !user.Enabled {
		return "", ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[user.Inbound]; !managed {
		return "", ErrNotFound
	}
	var eligible bool
	err := listener.WithManagedInboundListener(user.Inbound, func(managed C.ManagedUserListener) error {
		_, err := buildSubscriptionLink(m.config.PublicBaseURL, managed, user)
		if err == nil {
			eligible = true
		}
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !eligible {
		return "", nil
	}
	token := m.store.Subscriptions[userID]
	if token == "" {
		return "", ErrNotFound
	}
	baseURL, err := url.Parse(m.config.PublicBaseURL)
	if err != nil {
		return "", err
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/sub/aster/" + token
	baseURL.RawPath = ""
	return baseURL.String(), nil
}

func (m *Manager) SubscriptionLink(token string) (string, error) {
	runtime := m.runtime.Load()
	userID, exists := runtime.subscriptions[token]
	if !exists || token == "" {
		return "", ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config == nil || m.config.PublicBaseURL == "" || token == "" {
		return "", ErrNotFound
	}
	if candidate := m.store.Subscriptions[userID]; subtle.ConstantTimeCompare([]byte(candidate), []byte(token)) != 1 {
		return "", ErrNotFound
	}
	_, _, user := findUser(m.store, userID)
	if user == nil || !user.Enabled {
		return "", ErrNotFound
	}
	if _, managed := m.runtime.Load().managed[user.Inbound]; !managed {
		return "", ErrNotFound
	}

	var link string
	err := listener.WithManagedInboundListener(user.Inbound, func(managed C.ManagedUserListener) error {
		var err error
		link, err = buildSubscriptionLink(m.config.PublicBaseURL, managed, user)
		return err
	})
	if err != nil || link == "" {
		return "", errorsOrNotFound(err)
	}
	return link, nil
}

func buildSubscriptionLink(publicBaseURL string, managed C.ManagedUserListener, user *User) (string, error) {
	publicURL, err := url.Parse(publicBaseURL)
	if err != nil || publicURL.Hostname() == "" {
		return "", fmt.Errorf("%w: invalid public base URL", ErrInvalid)
	}
	port := listenerPort(managed.Address())
	label := user.Inbound + " - " + user.Name

	switch config := managed.Config().(type) {
	case *LI.VlessOption:
		if config.ShadowTLS.Enable || config.ResTLS.Enable || config.JLSConfig.Enable {
			return "", ErrNotFound
		}
		if port == "" {
			port = config.Port
		}
		if port == "" || user.UUID == "" {
			return "", ErrNotFound
		}
		query := url.Values{"encryption": {"none"}}
		if err := setVLESSNetwork(query, config); err != nil {
			return "", err
		}
		if user.Flow != "" {
			query.Set("flow", user.Flow)
		}
		if err := setSecurityQuery(query, config.RealityConfig, config.Certificate != "" && config.PrivateKey != "", publicURL.Hostname()); err != nil {
			return "", err
		}
		return (&url.URL{
			Scheme:   "vless",
			User:     url.User(user.UUID),
			Host:     net.JoinHostPort(publicURL.Hostname(), port),
			RawQuery: query.Encode(),
			Fragment: label,
		}).String(), nil
	case *LI.AnyTLSOption:
		if config.ShadowTLS.Enable || config.ResTLS.Enable || config.JLSConfig.Enable {
			return "", ErrNotFound
		}
		if port == "" {
			port = config.Port
		}
		if port == "" || user.Password == "" {
			return "", ErrNotFound
		}
		query := url.Values{"type": {"tcp"}}
		if err := setSecurityQuery(query, config.RealityConfig, config.Certificate != "" && config.PrivateKey != "", publicURL.Hostname()); err != nil {
			return "", err
		}
		if query.Get("security") == "" {
			return "", ErrNotFound
		}
		return (&url.URL{
			Scheme:   "anytls",
			User:     url.User(user.Password),
			Host:     net.JoinHostPort(publicURL.Hostname(), port),
			RawQuery: query.Encode(),
			Fragment: label,
		}).String(), nil
	default:
		return "", ErrNotFound
	}
}

func listenerPort(addresses string) string {
	for _, address := range strings.Split(addresses, ",") {
		_, port, err := net.SplitHostPort(strings.TrimSpace(address))
		if err == nil && port != "0" {
			return port
		}
	}
	return ""
}

func setVLESSNetwork(query url.Values, config *LI.VlessOption) error {
	switch {
	case config.WsPath != "":
		query.Set("type", "ws")
		query.Set("path", config.WsPath)
	case config.GrpcServiceName != "":
		query.Set("type", "grpc")
		query.Set("serviceName", config.GrpcServiceName)
	case config.XHTTPConfig.Path != "" || config.XHTTPConfig.Host != "" || config.XHTTPConfig.Mode != "":
		if hasAdvancedXHTTPConfig(config.XHTTPConfig) {
			return ErrNotFound
		}
		query.Set("type", "xhttp")
		query.Set("path", config.XHTTPConfig.Path)
		if config.XHTTPConfig.Host != "" {
			query.Set("host", config.XHTTPConfig.Host)
		}
		if config.XHTTPConfig.Mode != "" {
			query.Set("mode", config.XHTTPConfig.Mode)
		}
	default:
		query.Set("type", "tcp")
	}
	return nil
}

func hasAdvancedXHTTPConfig(config LI.XHTTPConfig) bool {
	return config.XPaddingBytes != "" || config.XPaddingObfsMode || config.XPaddingKey != "" ||
		config.XPaddingHeader != "" || config.XPaddingPlacement != "" || config.XPaddingMethod != "" ||
		config.UplinkHTTPMethod != "" || config.SessionPlacement != "" || config.SessionKey != "" ||
		config.SeqPlacement != "" || config.SeqKey != "" || config.UplinkDataPlacement != "" ||
		config.UplinkDataKey != "" || config.UplinkChunkSize != "" || config.NoSSEHeader ||
		config.ScStreamUpServerSecs != "" || config.ScMaxBufferedPosts != "" || config.ScMaxEachPostBytes != ""
}

func setSecurityQuery(query url.Values, reality LI.RealityConfig, hasCertificate bool, defaultSNI string) error {
	if reality.PrivateKey != "" {
		privateKeyBytes, err := base64.RawURLEncoding.DecodeString(reality.PrivateKey)
		if err != nil {
			return fmt.Errorf("decode REALITY private key: %w", err)
		}
		privateKey, err := ecdh.X25519().NewPrivateKey(privateKeyBytes)
		if err != nil {
			return fmt.Errorf("parse REALITY private key: %w", err)
		}
		query.Set("security", "reality")
		query.Set("fp", "chrome")
		query.Set("pbk", base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()))
		serverNames := append([]string(nil), reality.ServerNames...)
		sort.Strings(serverNames)
		if len(serverNames) > 0 {
			query.Set("sni", serverNames[0])
		}
		shortIDs := append([]string(nil), reality.ShortID...)
		sort.Strings(shortIDs)
		if len(shortIDs) > 0 {
			query.Set("sid", shortIDs[0])
		}
		return nil
	}
	if hasCertificate {
		query.Set("security", "tls")
		query.Set("sni", defaultSNI)
	}
	return nil
}

func errorsOrNotFound(err error) error {
	if err != nil {
		return err
	}
	return ErrNotFound
}
