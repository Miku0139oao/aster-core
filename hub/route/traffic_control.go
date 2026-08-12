package route

import (
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Miku0139oao/aster-core/component/kerneldirect"
	trafficControl "github.com/Miku0139oao/aster-core/component/trafficcontrol"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/tunnel"

	"github.com/metacubex/chi"
	"github.com/metacubex/chi/render"
	"github.com/metacubex/http"
)

const trafficControlBodyLimit = 1 << 20

func trafficControlRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/capabilities", getTrafficControlCapabilities)
	router.Get("/kernel-direct/status", getKernelDirectStatus)
	router.Get("/traffic-control/policies", getTrafficControlPolicies)
	router.Put("/traffic-control/policies", putTrafficControlPolicies)
	router.Get("/traffic-control/status", getTrafficControlStatus)
	router.Get("/traffic-control/rules", getTrafficControlRules)
	router.Post("/traffic-control/policies/{id}/reset", resetTrafficControlPolicy)
	router.Get("/traffic-control/reports", getTrafficControlReports)
	router.Get("/traffic-control/reports/summary", getTrafficControlReportSummary)
	router.Get("/traffic-control/reports/export.csv", exportTrafficControlReport)
	return router
}

func getTrafficControlCapabilities(writer http.ResponseWriter, request *http.Request) {
	render.JSON(writer, request, render.M{
		"traffic_control": render.M{
			"version": 1, "enabled": trafficControl.Default.Enabled(), "dimensions": []string{"global", "device", "rule", "target"},
			"reports": []string{"hour", "day", "month"}, "compression": "zstd", "persistence": true,
		},
		"kernel_direct": render.M{"version": 3, "backends": []string{"nftables", "ebpf-tc", "ebpf-tc-lpm-lru", "ebpf-tc-lpm-lru-redirect"}, "features": []string{"ipv4-lpm", "ipv6-lpm", "5-tuple-lru", "proxy-steering", "atomic-generation", "tc-tun-redirect", "local-address-bypass"}},
	})
}

func getKernelDirectStatus(writer http.ResponseWriter, request *http.Request) {
	paths := kerneldirect.FastPathStatuses()
	backend := "nftables"
	if len(paths) > 0 {
		backend = paths[0].Backend
	}
	render.JSON(writer, request, render.M{"backend": backend, "fast_paths": paths})
}

func getTrafficControlPolicies(writer http.ResponseWriter, request *http.Request) {
	config, revision := trafficControl.Default.Config()
	render.JSON(writer, request, render.M{"revision": revision, "config": config})
}

type trafficControlUpdate struct {
	Revision uint64                    `json:"revision"`
	Config   *trafficControl.RawConfig `json:"config"`
}

func putTrafficControlPolicies(writer http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(writer, request.Body, trafficControlBodyLimit)
	var update trafficControlUpdate
	if err := render.DecodeJSON(request.Body, &update); err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	_, revision := trafficControl.Default.Config()
	if revision != update.Revision {
		writeTrafficControlError(writer, request, http.StatusConflict, trafficControl.ErrRevisionConflict)
		return
	}
	config, err := trafficControl.ParseConfig(update.Config, func(path string) (string, error) {
		resolved := C.Path.Resolve(path)
		if !C.Path.IsSafePath(resolved) {
			return "", C.Path.ErrNotSafePath(resolved)
		}
		return resolved, nil
	})
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	if err := trafficControl.Default.ConfigureAtRevision(config, update.Revision); errors.Is(err, trafficControl.ErrRevisionConflict) {
		writeTrafficControlError(writer, request, http.StatusConflict, err)
		return
	} else if err != nil {
		writeTrafficControlError(writer, request, http.StatusInternalServerError, err)
		return
	}
	getTrafficControlPolicies(writer, request)
}

func getTrafficControlStatus(writer http.ResponseWriter, request *http.Request) {
	render.JSON(writer, request, trafficControl.Default.Status())
}

type trafficControlRuleView struct {
	Index       int    `json:"index"`
	Type        string `json:"type"`
	Payload     string `json:"payload"`
	Target      string `json:"target"`
	Fingerprint string `json:"fingerprint"`
}

func getTrafficControlRules(writer http.ResponseWriter, request *http.Request) {
	rules := tunnel.Rules()
	views := make([]trafficControlRuleView, 0, len(rules))
	for index, rule := range rules {
		canonical := trafficControl.CanonicalRule(rule.RuleType().String(), rule.Payload(), rule.Adapter())
		views = append(views, trafficControlRuleView{Index: index, Type: rule.RuleType().String(), Payload: rule.Payload(), Target: rule.Adapter(), Fingerprint: canonical.Fingerprint})
	}
	render.JSON(writer, request, render.M{"rules": views})
}

func resetTrafficControlPolicy(writer http.ResponseWriter, request *http.Request) {
	if err := trafficControl.Default.Reset(getEscapeParam(request, "id")); err != nil {
		writeTrafficControlError(writer, request, http.StatusNotFound, err)
		return
	}
	render.JSON(writer, request, trafficControl.Default.Status())
}

func trafficControlReportQuery(request *http.Request) (string, string, time.Time, time.Time, error) {
	query := request.URL.Query()
	key := query.Get("key")
	if key == "" {
		key = "global:global"
	}
	granularity := query.Get("granularity")
	if granularity == "" {
		granularity = "hour"
	}
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	if value := query.Get("from"); value != "" {
		unix, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", "", time.Time{}, time.Time{}, errors.New("invalid report from")
		}
		from = time.Unix(unix, 0)
	}
	if value := query.Get("to"); value != "" {
		unix, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", "", time.Time{}, time.Time{}, errors.New("invalid report to")
		}
		to = time.Unix(unix, 0)
	}
	if !from.Before(to) || to.Sub(from) > 400*24*time.Hour {
		return "", "", time.Time{}, time.Time{}, errors.New("report range must be positive and at most 400 days")
	}
	return key, granularity, from, to, nil
}

func getTrafficControlReports(writer http.ResponseWriter, request *http.Request) {
	key, granularity, from, to, err := trafficControlReportQuery(request)
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	buckets, err := trafficControl.Default.Reports(key, granularity, from, to)
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	render.JSON(writer, request, render.M{"key": key, "granularity": granularity, "from": from.Unix(), "to": to.Unix(), "buckets": buckets})
}

func getTrafficControlReportSummary(writer http.ResponseWriter, request *http.Request) {
	key, granularity, from, to, err := trafficControlReportQuery(request)
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	buckets, err := trafficControl.Default.Reports(key, granularity, from, to)
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	var total trafficControl.Counters
	for _, bucket := range buckets {
		total.UploadBytes += bucket.Counters.UploadBytes
		total.DownloadBytes += bucket.Counters.DownloadBytes
		total.Connections += bucket.Counters.Connections
		total.ExceededEvents += bucket.Counters.ExceededEvents
	}
	render.JSON(writer, request, render.M{"key": key, "from": from.Unix(), "to": to.Unix(), "summary": total})
}

func exportTrafficControlReport(writer http.ResponseWriter, request *http.Request) {
	key, granularity, from, to, err := trafficControlReportQuery(request)
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	buckets, err := trafficControl.Default.Reports(key, granularity, from, to)
	if err != nil {
		writeTrafficControlError(writer, request, http.StatusBadRequest, err)
		return
	}
	writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	writer.Header().Set("Content-Disposition", `attachment; filename="aster-traffic-report.csv"`)
	writer.Header().Set("Cache-Control", "no-store")
	csvWriter := csv.NewWriter(writer)
	_ = csvWriter.Write([]string{"key", "granularity", "start", "upload_bytes", "download_bytes", "connections", "exceeded_events"})
	for _, bucket := range buckets {
		_ = csvWriter.Write([]string{key, granularity, strconv.FormatInt(bucket.Start, 10), strconv.FormatInt(bucket.Counters.UploadBytes, 10), strconv.FormatInt(bucket.Counters.DownloadBytes, 10), strconv.FormatInt(bucket.Counters.Connections, 10), strconv.FormatInt(bucket.Counters.ExceededEvents, 10)})
	}
	csvWriter.Flush()
}

func writeTrafficControlError(writer http.ResponseWriter, request *http.Request, status int, err error) {
	render.Status(request, status)
	render.JSON(writer, request, render.M{"error": fmt.Sprint(err)})
}
