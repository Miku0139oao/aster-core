package route

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Miku0139oao/aster-core/adapter/inbound"
	"github.com/Miku0139oao/aster-core/common/utils"
	"github.com/Miku0139oao/aster-core/component/ca"
	"github.com/Miku0139oao/aster-core/component/ech"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/ntp"
	"github.com/Miku0139oao/aster-core/tunnel/statistic"

	"github.com/metacubex/chi"
	"github.com/metacubex/chi/cors"
	"github.com/metacubex/chi/middleware"
	"github.com/metacubex/chi/render"
	"github.com/metacubex/http"
	"github.com/metacubex/tls"
)

var (
	uiPath = ""

	httpServer *http.Server
	tlsServer  *http.Server
	unixServer *http.Server
	pipeServer *http.Server
	httpListen net.Listener
	tlsListen  net.Listener
	unixListen net.Listener
	pipeListen net.Listener
	serverMu   sync.Mutex
	serverGen  uint64

	embedMode = false
)

func SetEmbedMode(embed bool) {
	serverMu.Lock()
	defer serverMu.Unlock()
	embedMode = embed
}

type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

type Memory struct {
	Inuse   uint64 `json:"inuse"`
	OSLimit uint64 `json:"oslimit"` // maybe we need it in the future
}

type Config struct {
	Addr           string
	TLSAddr        string
	UnixAddr       string
	PipeAddr       string
	RoutingMark    int
	Secret         string
	Certificate    string
	PrivateKey     string
	ClientAuthType string
	ClientAuthCert string
	EchKey         string
	DohServer      string
	IsDebug        bool
	Cors           Cors
}

type Cors struct {
	AllowOrigins        []string
	AllowPrivateNetwork bool
}

func (c Cors) Apply(r chi.Router) {
	r.Use(cors.New(cors.Options{
		AllowedOrigins:      c.AllowOrigins,
		AllowedMethods:      []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:      []string{"Content-Type", "Authorization"},
		AllowPrivateNetwork: c.AllowPrivateNetwork,
		MaxAge:              300,
	}).Handler)
}

func ReCreateServer(cfg *Config) {
	cloned := *cfg
	cloned.Cors.AllowOrigins = append([]string(nil), cfg.Cors.AllowOrigins...)
	serverMu.Lock()
	serverGen++
	generation := serverGen
	serverMu.Unlock()
	go start(&cloned, generation)
	go startTLS(&cloned, generation)
	go startUnix(&cloned, generation)
	if inbound.SupportNamedPipe {
		go startPipe(&cloned, generation)
	}
}

func SetUIPath(path string) {
	serverMu.Lock()
	defer serverMu.Unlock()
	if path == "" {
		uiPath = ""
		return
	}
	uiPath = C.Path.Resolve(path)
}

type asterRoutePolicy struct {
	adminAllowed bool
	secure       bool
}

func router(isDebug bool, secret string, dohServer string, cors Cors, asterPolicy asterRoutePolicy) *chi.Mux {
	r := chi.NewRouter()
	cors.Apply(r)
	addAsterRoutes(r, asterPolicy)
	r.Group(func(r chi.Router) {
		if secret != "" {
			r.Use(authentication(secret))
		}
		if isDebug {
			r.Mount("/debug", func() http.Handler {
				r := chi.NewRouter()
				r.Put("/gc", func(w http.ResponseWriter, r *http.Request) {
					debug.FreeOSMemory()
				})
				handler := middleware.Profiler
				r.Mount("/", handler())
				return r
			}())
		}
		r.Get("/", hello)
		r.Get("/logs", getLogs)
		r.Get("/traffic", traffic)
		r.Get("/memory", memory)
		r.Get("/version", version)
		r.Mount("/configs", configRouter())
		r.Mount("/proxies", proxyRouter())
		r.Mount("/group", groupRouter())
		r.Mount("/rules", ruleRouter())
		r.Mount("/connections", connectionRouter())
		r.Mount("/providers/proxies", proxyProviderRouter())
		r.Mount("/providers/rules", ruleProviderRouter())
		r.Mount("/cache", cacheRouter())
		r.Mount("/dns", dnsRouter())
		r.Mount("/storage", storageRouter())
		if !embedMode { // disallow restart in embed mode
			r.Mount("/restart", restartRouter())
		}
		r.Mount("/upgrade", upgradeRouter())
		addExternalRouters(r)
	})

	if uiPath != "" {
		r.Group(func(r chi.Router) {
			fs := http.StripPrefix("/ui", http.FileServer(http.Dir(uiPath)))
			r.Get("/ui", http.RedirectHandler("/ui/", http.StatusTemporaryRedirect).ServeHTTP)
			r.Get("/ui/*", func(w http.ResponseWriter, r *http.Request) {
				fs.ServeHTTP(w, r)
			})
		})
	}
	if len(dohServer) > 0 && dohServer[0] == '/' {
		r.Mount(dohServer, dohRouter())
	}

	return r
}

func start(cfg *Config, generation uint64) {
	serverMu.Lock()
	if generation != serverGen {
		serverMu.Unlock()
		return
	}
	if httpServer != nil {
		_ = httpServer.Close()
		httpServer = nil
	}
	if httpListen != nil {
		_ = httpListen.Close()
		httpListen = nil
	}

	if len(cfg.Addr) == 0 {
		serverMu.Unlock()
		return
	}
	lc := inbound.NewListenConfig()
	lc.SetRouteMark(cfg.RoutingMark)
	l, err := lc.Listen(context.Background(), "tcp", cfg.Addr)
	if err != nil {
		serverMu.Unlock()
		log.Errorln("External controller listen error: %s", err)
		return
	}
	server := &http.Server{
		Handler: router(cfg.IsDebug, cfg.Secret, cfg.DohServer, cfg.Cors, asterRoutePolicy{adminAllowed: isLoopbackAddress(l.Addr())}),
	}
	httpServer = server
	httpListen = l
	serverMu.Unlock()
	log.Infoln("RESTful API listening at: %s", l.Addr().String())
	err = server.Serve(l)
	serverMu.Lock()
	if httpServer == server {
		httpServer = nil
		httpListen = nil
	}
	serverMu.Unlock()
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Errorln("External controller serve error: %s", err)
	}
}

func startTLS(cfg *Config, generation uint64) {
	serverMu.Lock()
	if generation != serverGen {
		serverMu.Unlock()
		return
	}
	if tlsServer != nil {
		_ = tlsServer.Close()
		tlsServer = nil
	}
	if tlsListen != nil {
		_ = tlsListen.Close()
		tlsListen = nil
	}

	if len(cfg.TLSAddr) == 0 {
		serverMu.Unlock()
		return
	}
	certLoader, err := ca.NewTLSKeyPairLoader(cfg.Certificate, cfg.PrivateKey)
	if err != nil {
		serverMu.Unlock()
		log.Errorln("External controller tls listen error: %s", err)
		return
	}
	tlsConfig := &tls.Config{Time: ntp.Now, NextProtos: []string{"h2", "http/1.1"}}
	tlsConfig.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return certLoader()
	}
	tlsConfig.ClientAuth = ca.ClientAuthTypeFromString(cfg.ClientAuthType)
	if len(cfg.ClientAuthCert) > 0 && tlsConfig.ClientAuth == tls.NoClientCert {
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	if tlsConfig.ClientAuth == tls.VerifyClientCertIfGiven || tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		pool, err := ca.LoadCertificates(cfg.ClientAuthCert)
		if err != nil {
			serverMu.Unlock()
			log.Errorln("External controller tls listen error: %s", err)
			return
		}
		tlsConfig.ClientCAs = pool
	}
	if cfg.EchKey != "" {
		if err = ech.LoadECHKey(cfg.EchKey, tlsConfig); err != nil {
			serverMu.Unlock()
			log.Errorln("External controller tls serve error: %s", err)
			return
		}
	}
	lc := inbound.NewListenConfig()
	lc.SetRouteMark(cfg.RoutingMark)
	l, err := lc.Listen(context.Background(), "tcp", cfg.TLSAddr)
	if err != nil {
		serverMu.Unlock()
		log.Errorln("External controller tls listen error: %s", err)
		return
	}
	secureListener := tls.NewListener(l, tlsConfig)
	server := &http.Server{
		Handler: router(cfg.IsDebug, cfg.Secret, cfg.DohServer, cfg.Cors, asterRoutePolicy{adminAllowed: true, secure: true}),
	}
	tlsServer = server
	tlsListen = secureListener
	serverMu.Unlock()
	log.Infoln("RESTful API tls listening at: %s", l.Addr().String())
	err = server.Serve(secureListener)
	serverMu.Lock()
	if tlsServer == server {
		tlsServer = nil
		tlsListen = nil
	}
	serverMu.Unlock()
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Errorln("External controller tls serve error: %s", err)
	}
}

func startUnix(cfg *Config, generation uint64) {
	serverMu.Lock()
	if generation != serverGen {
		serverMu.Unlock()
		return
	}
	if unixServer != nil {
		_ = unixServer.Close()
		unixServer = nil
	}
	if unixListen != nil {
		_ = unixListen.Close()
		unixListen = nil
	}

	if len(cfg.UnixAddr) == 0 {
		serverMu.Unlock()
		return
	}
	addr := C.Path.Resolve(cfg.UnixAddr)
	dir := filepath.Dir(addr)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			serverMu.Unlock()
			log.Errorln("External controller unix listen error: %s", err)
			return
		}
	}

	// A pathname Unix socket must be removed before rebinding on Unix and Windows.
	_ = syscall.Unlink(addr)
	lc := inbound.NewListenConfig()
	lc.SetRouteMark(0)
	l, err := lc.Listen(context.Background(), "unix", addr)
	if err != nil {
		serverMu.Unlock()
		log.Errorln("External controller unix listen error: %s", err)
		return
	}
	if err = os.Chmod(addr, 0o600); err != nil {
		_ = l.Close()
		serverMu.Unlock()
		log.Errorln("External controller unix permission error: %s", err)
		return
	}
	server := &http.Server{
		Handler: router(cfg.IsDebug, cfg.Secret, cfg.DohServer, cfg.Cors, asterRoutePolicy{adminAllowed: true}),
	}
	unixServer = server
	unixListen = l
	serverMu.Unlock()
	log.Infoln("RESTful API unix listening at: %s", l.Addr().String())
	err = server.Serve(l)
	serverMu.Lock()
	if unixServer == server {
		unixServer = nil
		unixListen = nil
	}
	serverMu.Unlock()
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Errorln("External controller unix serve error: %s", err)
	}
}

func startPipe(cfg *Config, generation uint64) {
	serverMu.Lock()
	if generation != serverGen {
		serverMu.Unlock()
		return
	}
	if pipeServer != nil {
		_ = pipeServer.Close()
		pipeServer = nil
	}
	if pipeListen != nil {
		_ = pipeListen.Close()
		pipeListen = nil
	}

	if len(cfg.PipeAddr) == 0 {
		serverMu.Unlock()
		return
	}
	if !strings.HasPrefix(cfg.PipeAddr, "\\\\.\\pipe\\") { // windows namedpipe must start with "\\.\pipe\"
		serverMu.Unlock()
		log.Errorln("External controller pipe listen error: windows namedpipe must start with \"\\\\.\\pipe\\\"")
		return
	}
	l, err := inbound.ListenNamedPipe(cfg.PipeAddr)
	if err != nil {
		serverMu.Unlock()
		log.Errorln("External controller pipe listen error: %s", err)
		return
	}
	server := &http.Server{
		Handler: router(cfg.IsDebug, cfg.Secret, cfg.DohServer, cfg.Cors, asterRoutePolicy{adminAllowed: true}),
	}
	pipeServer = server
	pipeListen = l
	serverMu.Unlock()
	log.Infoln("RESTful API pipe listening at: %s", l.Addr().String())
	err = server.Serve(l)
	serverMu.Lock()
	if pipeServer == server {
		pipeServer = nil
		pipeListen = nil
	}
	serverMu.Unlock()
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
		log.Errorln("External controller pipe serve error: %s", err)
	}
}

func isLoopbackAddress(address net.Addr) bool {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		return tcpAddress.IP.IsLoopback()
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func safeEqual(a, b string) bool {
	aBuf := utils.ImmutableBytesFromString(a)
	bBuf := utils.ImmutableBytesFromString(b)
	return subtle.ConstantTimeCompare(aBuf, bBuf) == 1
}

func authentication(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			// Browser websocket not support custom header
			if r.Header.Get("Upgrade") == "websocket" && r.URL.Query().Get("token") != "" {
				token := r.URL.Query().Get("token")
				if !safeEqual(token, secret) {
					render.Status(r, http.StatusUnauthorized)
					render.JSON(w, r, ErrUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")
			bearer, token, found := strings.Cut(header, " ")

			hasInvalidHeader := bearer != "Bearer"
			hasInvalidSecret := !found || !safeEqual(token, secret)
			if hasInvalidHeader || hasInvalidSecret {
				render.Status(r, http.StatusUnauthorized)
				render.JSON(w, r, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(fn)
	}
}

func hello(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, render.M{"hello": "mihomo"})
}

func traffic(w http.ResponseWriter, r *http.Request) {
	var wsConn net.Conn
	if r.Header.Get("Upgrade") == "websocket" {
		var err error
		wsConn, _, err = wsUpgrade(r, w)
		if err != nil {
			return
		}
	}

	if wsConn == nil {
		w.Header().Set("Content-Type", "application/json")
		render.Status(r, http.StatusOK)
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	t := statistic.DefaultManager
	buf := &bytes.Buffer{}
	var err error
	for range tick.C {
		buf.Reset()
		up, down := t.Now()
		upTotal, downTotal := t.Total()
		if err := json.NewEncoder(buf).Encode(Traffic{
			Up:        up,
			Down:      down,
			UpTotal:   upTotal,
			DownTotal: downTotal,
		}); err != nil {
			break
		}

		if wsConn == nil {
			_, err = w.Write(buf.Bytes())
			w.(http.Flusher).Flush()
		} else {
			err = wsWriteServerText(wsConn, buf.Bytes())
		}

		if err != nil {
			break
		}
	}
}

func memory(w http.ResponseWriter, r *http.Request) {
	var wsConn net.Conn
	if r.Header.Get("Upgrade") == "websocket" {
		var err error
		wsConn, _, err = wsUpgrade(r, w)
		if err != nil {
			return
		}
	}

	if wsConn == nil {
		w.Header().Set("Content-Type", "application/json")
		render.Status(r, http.StatusOK)
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	t := statistic.DefaultManager
	buf := &bytes.Buffer{}
	var err error
	first := true
	for range tick.C {
		buf.Reset()

		inuse := t.Memory()
		// make chat.js begin with zero
		// this is shit var,but we need output 0 for first time
		if first {
			inuse = 0
			first = false
		}
		if err := json.NewEncoder(buf).Encode(Memory{
			Inuse:   inuse,
			OSLimit: 0,
		}); err != nil {
			break
		}
		if wsConn == nil {
			_, err = w.Write(buf.Bytes())
			w.(http.Flusher).Flush()
		} else {
			err = wsWriteServerText(wsConn, buf.Bytes())
		}

		if err != nil {
			break
		}
	}
}

type Log struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}
type LogStructuredField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type LogStructured struct {
	Time    string               `json:"time"`
	Level   string               `json:"level"`
	Message string               `json:"message"`
	Fields  []LogStructuredField `json:"fields"`
}

func getLogs(w http.ResponseWriter, r *http.Request) {
	levelText := r.URL.Query().Get("level")
	if levelText == "" {
		levelText = "info"
	}

	formatText := r.URL.Query().Get("format")
	isStructured := false
	if formatText == "structured" {
		isStructured = true
	}

	level, ok := log.LogLevelMapping[levelText]
	if !ok {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	var wsConn net.Conn
	if r.Header.Get("Upgrade") == "websocket" {
		var err error
		wsConn, _, err = wsUpgrade(r, w)
		if err != nil {
			return
		}
	}

	if wsConn == nil {
		w.Header().Set("Content-Type", "application/json")
		render.Status(r, http.StatusOK)
	}

	ch := make(chan log.Event, 1024)
	sub := log.Subscribe()
	defer log.UnSubscribe(sub)
	buf := &bytes.Buffer{}

	go func() {
		for logM := range sub {
			select {
			case ch <- logM:
			default:
			}
		}
		close(ch)
	}()

	for logM := range ch {
		if logM.LogLevel < level {
			continue
		}
		buf.Reset()

		if !isStructured {
			if err := json.NewEncoder(buf).Encode(Log{
				Type:    logM.Type(),
				Payload: logM.Payload,
			}); err != nil {
				break
			}
		} else {
			newLevel := logM.Type()
			if newLevel == "warning" {
				newLevel = "warn"
			}
			if err := json.NewEncoder(buf).Encode(LogStructured{
				Time:    time.Now().Format(time.TimeOnly),
				Level:   newLevel,
				Message: logM.Payload,
				Fields:  []LogStructuredField{},
			}); err != nil {
				break
			}
		}

		var err error
		if wsConn == nil {
			_, err = w.Write(buf.Bytes())
			w.(http.Flusher).Flush()
		} else {
			err = wsWriteServerText(wsConn, buf.Bytes())
		}

		if err != nil {
			break
		}
	}
}

func version(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, render.M{"meta": C.Meta, "version": C.Version})
}
