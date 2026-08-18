package route

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	asterManager "github.com/Miku0139oao/aster-core/component/aster"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/tunnel/statistic"

	"github.com/metacubex/chi"
	"github.com/metacubex/chi/render"
	"github.com/metacubex/http"
)

var asterStartedAt = time.Now()

type asterUserInput struct {
	Inbound  string  `json:"inbound"`
	Name     *string `json:"name"`
	UUID     *string `json:"uuid"`
	Password *string `json:"password"`
	Flow     *string `json:"flow"`
	Enabled  *bool   `json:"enabled"`
	Revision int64   `json:"revision"`
}

type asterUserView struct {
	ID                string `json:"id"`
	Inbound           string `json:"inbound"`
	Type              string `json:"type"`
	Name              string `json:"name"`
	UUID              string `json:"uuid,omitempty"`
	Password          string `json:"password,omitempty"`
	Flow              string `json:"flow,omitempty"`
	Enabled           bool   `json:"enabled"`
	UploadBytes       int64  `json:"upload_bytes"`
	DownloadBytes     int64  `json:"download_bytes"`
	TrafficGeneration uint64 `json:"traffic_generation"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
	ActiveConnections int    `json:"active_connections"`
	Revision          int64  `json:"revision"`
	AppliedRevision   int64  `json:"applied_revision"`
	SubscriptionURL   string `json:"subscription_url,omitempty"`
}

type asterInboundSummary struct {
	Tag              string `json:"tag"`
	Type             string `json:"type"`
	Managed          bool   `json:"managed"`
	Credential       string `json:"credential"`
	Flow             bool   `json:"flow,omitempty"`
	Traffic          bool   `json:"traffic"`
	UserCount        int    `json:"user_count"`
	EnabledUserCount int    `json:"enabled_user_count"`
	Revision         int64  `json:"revision"`
	AppliedRevision  int64  `json:"applied_revision"`
	Pending          bool   `json:"pending,omitempty"`
}

func addAsterRoutes(router chi.Router, policy asterRoutePolicy) {
	router.Get("/sub/aster/{token}", getAsterSubscription)
	router.Head("/sub/aster/{token}", getAsterSubscription)
	if !policy.adminAllowed {
		return
	}
	router.Group(func(router chi.Router) {
		router.Use(asterAdminMiddleware(policy.secure))
		router.Get("/api/admin/overview", getAsterOverview)
		router.Get("/api/admin/status", getAsterOverview)
		router.Get("/api/admin/protocols", getAsterProtocols)
		router.Get("/api/admin/inbounds", getAsterInbounds)
		router.Get("/api/admin/listeners", getAsterInbounds)
		router.Get("/api/admin/users", listAsterUsers)
		router.Get("/api/admin/users/{id}", getAsterUser)
		router.Post("/api/admin/users", createAsterUser)
		router.Put("/api/admin/users/{id}", updateAsterUser)
		router.Delete("/api/admin/users/{id}", deleteAsterUser)
		router.Post("/api/admin/users/{id}/reset-traffic", resetAsterUserTraffic)
		router.Post("/api/admin/users/{id}/rotate-subscription", rotateAsterSubscription)
	})
}

func asterAdminMiddleware(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Cache-Control", "no-store")
			if !asterManager.Default.Enabled() {
				http.NotFound(writer, request)
				return
			}
			if !sameOriginAsterRequest(request, secure) {
				writeAsterError(writer, request, http.StatusForbidden, "Aster admin API requires a same-origin request")
				return
			}
			bearer, token, found := strings.Cut(request.Header.Get("Authorization"), " ")
			if !found || !strings.EqualFold(bearer, "Bearer") || !asterManager.Default.Authenticate(token) {
				writer.Header().Set("WWW-Authenticate", "Bearer")
				writeAsterError(writer, request, http.StatusUnauthorized, "invalid Aster API token")
				return
			}
			next.ServeHTTP(writer, request)
		})
	}
}

func sameOriginAsterRequest(request *http.Request, secure bool) bool {
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil || parsedOrigin.Scheme == "" || parsedOrigin.Host == "" {
		return false
	}
	expectedScheme := "http"
	if secure || request.TLS != nil {
		expectedScheme = "https"
	} else if requestFromLoopbackAddress(request.RemoteAddr) {
		forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0])
		if forwarded == "http" || forwarded == "https" {
			expectedScheme = forwarded
		}
	}
	return strings.EqualFold(parsedOrigin.Scheme, expectedScheme) && strings.EqualFold(parsedOrigin.Host, request.Host)
}

func requestFromLoopbackAddress(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func getAsterOverview(writer http.ResponseWriter, request *http.Request) {
	summary, err := asterManager.Default.Summary()
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	upload, download := statistic.DefaultManager.Total()
	connectionCount := statistic.DefaultManager.ConnectionCount()
	// runtime.ReadMemStats stops the world, which a polled dashboard endpoint must
	// not do; the manager already samples the process resident set size.
	memoryBytes := statistic.DefaultManager.Memory()
	now := time.Now()
	render.JSON(writer, request, render.M{
		"version":        C.Version,
		"api_version":    1,
		"status":         "running",
		"started_at":     asterStartedAt.UnixMilli(),
		"uptime_seconds": int64(now.Sub(asterStartedAt).Seconds()),
		"platform": render.M{
			"os": runtime.GOOS, "arch": runtime.GOARCH, "cpu_cores": runtime.NumCPU(),
			"memory_bytes": memoryBytes, "goroutines": runtime.NumGoroutine(),
		},
		"traffic": render.M{
			"uplink_total": upload, "downlink_total": download, "active_connections": connectionCount,
		},
		"users": render.M{
			"total": summary.TotalUsers, "enabled": summary.EnabledUsers,
			"disabled": summary.TotalUsers - summary.EnabledUsers,
		},
		"capabilities":           render.M{"quota": false, "expiration": false},
		"authentication_enabled": true,
		"inbounds":               makeAsterSummaryInbounds(summary.Listeners),
	})
}

func getAsterProtocols(writer http.ResponseWriter, request *http.Request) {
	render.JSON(writer, request, render.M{
		"schema_version": 1,
		"protocols": []render.M{
			{"kind": "inbound", "type": "vless", "name": "VLESS", "category": "proxy", "network": "tcp", "tls": true, "credential": "uuid", "update_policy": "live"},
			{"kind": "inbound", "type": "anytls", "name": "AnyTLS", "category": "proxy", "network": "tcp", "tls": true, "credential": "password", "update_policy": "live"},
		},
	})
}

func getAsterInbounds(writer http.ResponseWriter, request *http.Request) {
	inbounds, err := asterInboundSummaries()
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	render.JSON(writer, request, render.M{"inbounds": inbounds, "listeners": inbounds})
}

func listAsterUsers(writer http.ResponseWriter, request *http.Request) {
	snapshot, err := asterManager.Default.ManagementSnapshot(request.URL.Query().Get("inbound"))
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	connections := asterActiveConnections()
	views := make([]asterUserView, 0, len(snapshot.Users))
	for _, record := range snapshot.Users {
		views = append(views, makeAsterUserView(record.User, record.Revision, connections[statistic.Principal{Inbound: record.User.Inbound, UserID: record.User.ID}], false))
		views[len(views)-1].AppliedRevision = record.AppliedRevision
	}
	render.JSON(writer, request, render.M{"users": views, "inbounds": makeAsterInboundSummaries(snapshot.Listeners)})
}

func getAsterUser(writer http.ResponseWriter, request *http.Request) {
	user, revision, err := asterManager.Default.GetUser(chi.URLParam(request, "id"))
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	view := makeAsterUserView(user, revision, asterUserConnections(user), true)
	view.SubscriptionURL, _ = asterManager.Default.SubscriptionURL(user.ID)
	render.JSON(writer, request, view)
}

func createAsterUser(writer http.ResponseWriter, request *http.Request) {
	input, ok := decodeAsterUserInput(writer, request)
	if !ok {
		return
	}
	name := ""
	if input.Name != nil {
		name = *input.Name
	}
	created, revision, err := asterManager.Default.CreateUser(asterManager.CreateUserInput{
		Inbound: input.Inbound, Name: name, UUID: stringValue(input.UUID), Password: stringValue(input.Password),
		Flow: stringValue(input.Flow), Enabled: input.Enabled,
	}, input.Revision)
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	view := makeAsterUserView(created, revision, 0, true)
	view.SubscriptionURL, _ = asterManager.Default.SubscriptionURL(created.ID)
	render.Status(request, http.StatusCreated)
	render.JSON(writer, request, view)
}

func updateAsterUser(writer http.ResponseWriter, request *http.Request) {
	input, ok := decodeAsterUserInput(writer, request)
	if !ok {
		return
	}
	updated, revision, err := asterManager.Default.UpdateUser(chi.URLParam(request, "id"), asterManager.UpdateUserInput{
		Name: input.Name, UUID: input.UUID, Password: input.Password, Flow: input.Flow, Enabled: input.Enabled,
	}, input.Revision)
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	view := makeAsterUserView(updated, revision, asterUserConnections(updated), true)
	view.SubscriptionURL, _ = asterManager.Default.SubscriptionURL(updated.ID)
	render.JSON(writer, request, view)
}

func deleteAsterUser(writer http.ResponseWriter, request *http.Request) {
	revision, err := asterRequestedRevision(request)
	if err != nil {
		writeAsterError(writer, request, http.StatusBadRequest, err.Error())
		return
	}
	if _, err = asterManager.Default.DeleteUser(chi.URLParam(request, "id"), revision); err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	render.NoContent(writer, request)
}

func resetAsterUserTraffic(writer http.ResponseWriter, request *http.Request) {
	input, ok := decodeAsterUserInput(writer, request)
	if !ok {
		return
	}
	user, revision, err := asterManager.Default.ResetTraffic(chi.URLParam(request, "id"), input.Revision)
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	view := makeAsterUserView(user, revision, asterUserConnections(user), true)
	view.SubscriptionURL, _ = asterManager.Default.SubscriptionURL(user.ID)
	render.JSON(writer, request, view)
}

func rotateAsterSubscription(writer http.ResponseWriter, request *http.Request) {
	input, ok := decodeAsterUserInput(writer, request)
	if !ok {
		return
	}
	_, revision, err := asterManager.Default.RotateSubscription(chi.URLParam(request, "id"), input.Revision)
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	url, err := asterManager.Default.SubscriptionURL(chi.URLParam(request, "id"))
	if err != nil {
		writeAsterManagerError(writer, request, err)
		return
	}
	render.JSON(writer, request, render.M{"revision": revision, "subscription_url": url})
}

func getAsterSubscription(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	token := chi.URLParam(request, "token")
	link, err := asterManager.Default.SubscriptionLink(token)
	if err != nil {
		if !errors.Is(err, asterManager.ErrNotFound) {
			log.Warnln("Build Aster subscription failed: %s", err)
		}
		http.NotFound(writer, request)
		return
	}
	body := base64.StdEncoding.EncodeToString([]byte(link))
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if request.Method == http.MethodGet {
		_, _ = writer.Write([]byte(body))
	}
}

func asterInboundSummaries() ([]asterInboundSummary, error) {
	listeners, err := asterManager.Default.ListListeners()
	if err != nil {
		return nil, err
	}
	return makeAsterInboundSummaries(listeners), nil
}

func makeAsterInboundSummary(name, protocol string, userCount, enabledUserCount int, revision, appliedRevision int64) asterInboundSummary {
	credential := "password"
	flow := false
	if protocol == "vless" {
		credential = "uuid"
		flow = true
	}
	return asterInboundSummary{
		Tag: name, Type: protocol, Managed: true, Credential: credential,
		Flow: flow, Traffic: true, UserCount: userCount, EnabledUserCount: enabledUserCount,
		Revision: revision, AppliedRevision: appliedRevision, Pending: revision != appliedRevision,
	}
}

func makeAsterInboundSummaries(listeners []asterManager.ListenerState) []asterInboundSummary {
	summaries := make([]asterInboundSummary, 0, len(listeners))
	for _, listener := range listeners {
		enabled := 0
		for _, user := range listener.Users {
			if user.Enabled {
				enabled++
			}
		}
		summaries = append(summaries, makeAsterInboundSummary(
			listener.Name, listener.Protocol, len(listener.Users), enabled,
			listener.Revision, listener.AppliedRevision,
		))
	}
	return summaries
}

func makeAsterSummaryInbounds(listeners []asterManager.ListenerSummary) []asterInboundSummary {
	summaries := make([]asterInboundSummary, 0, len(listeners))
	for _, listener := range listeners {
		summaries = append(summaries, makeAsterInboundSummary(
			listener.Name, listener.Protocol, listener.UserCount, listener.EnabledUserCount,
			listener.Revision, listener.AppliedRevision,
		))
	}
	return summaries
}

func makeAsterUserView(user asterManager.User, revision int64, connections int, includeCredentials bool) asterUserView {
	view := asterUserView{
		ID: user.ID, Inbound: user.Inbound, Type: user.Protocol, Name: user.Name, Flow: user.Flow,
		Enabled: user.Enabled, UploadBytes: user.UploadBytes, DownloadBytes: user.DownloadBytes,
		TrafficGeneration: user.TrafficGeneration, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
		ActiveConnections: connections, Revision: revision, AppliedRevision: revision,
	}
	if includeCredentials {
		view.UUID = user.UUID
		view.Password = user.Password
	}
	return view
}

func asterActiveConnections() map[statistic.Principal]int {
	return statistic.DefaultManager.ActiveConnectionsByPrincipal()
}

func asterUserConnections(user asterManager.User) int {
	return statistic.DefaultManager.ActiveConnections(statistic.Principal{Inbound: user.Inbound, UserID: user.ID})
}

func decodeAsterUserInput(writer http.ResponseWriter, request *http.Request) (asterUserInput, bool) {
	var input asterUserInput
	if err := decodeRequestJSON(writer, request, &input); err != nil {
		writeAsterError(writer, request, http.StatusBadRequest, err.Error())
		return asterUserInput{}, false
	}
	if input.Revision <= 0 {
		writeAsterError(writer, request, http.StatusBadRequest, "revision must be a positive integer")
		return asterUserInput{}, false
	}
	return input, true
}

func asterRequestedRevision(request *http.Request) (int64, error) {
	value := request.URL.Query().Get("revision")
	if value == "" {
		return 0, errors.New("revision is required")
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return 0, errors.New("revision must be a positive integer")
	}
	return revision, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func writeAsterManagerError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, asterManager.ErrDisabled), errors.Is(err, asterManager.ErrNotFound):
		writeAsterError(writer, request, http.StatusNotFound, err.Error())
	case errors.Is(err, asterManager.ErrConflict):
		writeAsterError(writer, request, http.StatusConflict, err.Error())
	case errors.Is(err, asterManager.ErrInvalid):
		writeAsterError(writer, request, http.StatusBadRequest, err.Error())
	default:
		log.Errorln("Aster admin request failed: %s", err)
		writeAsterError(writer, request, http.StatusInternalServerError, "Aster operation failed")
	}
}

func writeAsterError(writer http.ResponseWriter, request *http.Request, status int, message string) {
	render.Status(request, status)
	render.JSON(writer, request, newError(message))
}
