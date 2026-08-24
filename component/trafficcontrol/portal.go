package trafficcontrol

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type portalService struct {
	server  *http.Server
	listen  net.Listener
	secret  [32]byte
	started atomic.Bool
	mu      sync.RWMutex
	config  PortalConfig
	baseURL string
}

type portalPage struct {
	Policies []PolicyStatus
}

var portalTemplate = template.Must(template.New("quota").Funcs(template.FuncMap{
	"bytes": func(value int64) string { return humanBytes(value) },
	"rate":  func(value int64) string { return humanRate(value) },
}).Parse(`<!doctype html>
<html lang="zh-Hant"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="cache-control" content="no-store"><title>Aster 流量額度</title>
<style>body{font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0;margin:0;padding:2rem}.card{max-width:42rem;margin:auto;background:#1e293b;border-radius:1rem;padding:1.5rem}h1{margin-top:0}.item{border-top:1px solid #334155;padding:1rem 0}.warn{color:#fbbf24}.meter{height:.6rem;background:#334155;border-radius:1rem;overflow:hidden}.meter span{display:block;height:100%;background:#f59e0b}</style></head>
<body><main class="card"><h1>流量額度提示</h1><p class="warn">目前連線已套用超額保底速率，額度窗口恢復後會自動解除。</p>
{{range .Policies}}<section class="item"><strong>{{if .Policy.Name}}{{.Policy.Name}}{{else}}{{.Policy.ID}}{{end}}</strong>
<p>已用：{{bytes .Rolling.UploadBytes}} 上傳／{{bytes .Rolling.DownloadBytes}} 下載</p>
<p>保底：{{rate .Policy.Quota.OverageUploadBPS}} 上傳／{{rate .Policy.Quota.OverageDownloadBPS}} 下載</p></section>{{end}}
</main></body></html>`))

func (m *Manager) preparePortal(config PortalConfig) (*portalService, error) {
	if strings.TrimSpace(config.Listen) == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return nil, err
	}
	service := &portalService{listen: listener, config: config}
	service.setConfig(config)
	if _, err := rand.Read(service.secret[:]); err != nil {
		_ = listener.Close()
		return nil, err
	}
	service.server = &http.Server{Handler: http.HandlerFunc(m.servePortal), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	return service, nil
}

func (m *Manager) activatePortal(service *portalService) {
	m.portal.Store(service)
	if service == nil || !service.started.CompareAndSwap(false, true) {
		return
	}
	go func() { _ = service.server.Serve(service.listen) }()
}

func (s *portalService) setConfig(config PortalConfig) {
	s.mu.Lock()
	s.config = config
	s.baseURL = strings.TrimRight(config.URL, "/")
	if s.baseURL == "" && s.listen != nil {
		s.baseURL = "http://" + s.listen.Addr().String()
	}
	s.mu.Unlock()
}

func (s *portalService) requestedListen() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	listen := s.config.Listen
	s.mu.RUnlock()
	return listen
}

func (s *portalService) url() string {
	s.mu.RLock()
	url := s.baseURL
	s.mu.RUnlock()
	return url
}

func (m *Manager) startPortal(config PortalConfig) error {
	service, err := m.preparePortal(config)
	if err != nil {
		return err
	}
	m.activatePortal(service)
	return nil
}

func closePortalService(service *portalService) error {
	if service == nil {
		return nil
	}
	if !service.started.Load() {
		return service.listen.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := service.server.Shutdown(ctx)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (m *Manager) stopPortal() error {
	return closePortalService(m.portal.Swap(nil))
}

func (s *Session) PortalURL() string {
	if s == nil || s.manager == nil || s.manager.portal.Load() == nil {
		return ""
	}
	binding := s.currentBinding()
	if binding == nil {
		return ""
	}
	ids := make([]string, 0, len(binding.policies))
	now := s.manager.now()
	for _, policy := range binding.policies {
		if policy.state.OverQuota.Load() {
			policy.refreshQuota(now)
		}
		if policy.state.OverQuota.Load() && policy.spec.Quota.Portal {
			ids = append(ids, policy.spec.ID)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return s.manager.signedPortalURL(ids, now.Add(5*time.Minute))
}

func (m *Manager) signedPortalURL(ids []string, expires time.Time) string {
	service := m.portal.Load()
	if service == nil {
		return ""
	}
	joined := strings.Join(ids, ",")
	expiry := strconv.FormatInt(expires.Unix(), 10)
	message := joined + "\x00" + expiry
	mac := hmac.New(sha256.New, service.secret[:])
	_, _ = mac.Write([]byte(message))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	values := url.Values{"p": []string{joined}, "e": []string{expiry}, "s": []string{signature}}
	return service.url() + "/?" + values.Encode()
}

func (m *Manager) servePortal(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store, max-age=0")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	ids, ok := m.validatePortalToken(request.URL.Query())
	if !ok {
		http.Error(writer, "額度連結已失效，請重新連線。", http.StatusForbidden)
		return
	}
	status := m.Status()
	index := make(map[string]PolicyStatus, len(status.Policies))
	for _, policy := range status.Policies {
		index[policy.Policy.ID] = policy
	}
	page := portalPage{Policies: make([]PolicyStatus, 0, len(ids))}
	for _, id := range ids {
		if policy, exists := index[id]; exists && policy.OverQuota {
			page.Policies = append(page.Policies, policy)
		}
	}
	if len(page.Policies) == 0 {
		http.Error(writer, "額度已恢復。", http.StatusGone)
		return
	}
	if err := portalTemplate.Execute(writer, page); err != nil {
		return
	}
}

func (m *Manager) validatePortalToken(values url.Values) ([]string, bool) {
	service := m.portal.Load()
	if service == nil {
		return nil, false
	}
	joined, expiry, signature := values.Get("p"), values.Get("e"), values.Get("s")
	unix, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || m.now().Unix() > unix || unix-m.now().Unix() > 10*60 {
		return nil, false
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, service.secret[:])
	_, _ = mac.Write([]byte(joined + "\x00" + expiry))
	expected := mac.Sum(nil)
	if len(provided) != len(expected) || subtle.ConstantTimeCompare(provided, expected) != 1 {
		return nil, false
	}
	ids := strings.Split(joined, ",")
	if len(ids) == 0 || len(ids) > 8 {
		return nil, false
	}
	return ids, true
}

func humanBytes(value int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	unit := units[0]
	for i := 1; i < len(units) && amount >= 1024; i++ {
		amount /= 1024
		unit = units[i]
	}
	return fmt.Sprintf("%.1f %s", amount, unit)
}

func humanRate(value int64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1f Mbit/s", float64(value)/1_000_000)
	}
	return fmt.Sprintf("%.0f Kbit/s", float64(value)/1000)
}
