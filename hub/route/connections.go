package route

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Miku0139oao/aster-core/tunnel/statistic"

	"github.com/metacubex/chi"
	"github.com/metacubex/chi/render"
	"github.com/metacubex/http"
)

func connectionRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/", getConnections)
	r.Delete("/{id}", closeConnection)
	return r
}

func getConnections(w http.ResponseWriter, r *http.Request) {
	if !isWebSocketRequest(r) {
		snapshot := normalizedConnectionSnapshot(statistic.DefaultManager.Snapshot())
		render.JSON(w, r, snapshot)
		return
	}

	interval, err := connectionInterval(r)
	if err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, newError(err.Error()))
		return
	}

	conn, _, err := wsUpgrade(r, w)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return
	}
	defer conn.Close()

	buf := &bytes.Buffer{}
	sendSnapshot := func() error {
		buf.Reset()
		snapshot := normalizedConnectionSnapshot(statistic.DefaultManager.Snapshot())
		if err := json.NewEncoder(buf).Encode(snapshot); err != nil {
			return err
		}

		return wsWriteServerText(conn, buf.Bytes())
	}

	if err := sendSnapshot(); err != nil {
		return
	}

	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if err := sendSnapshot(); err != nil {
				return
			}
		}
	}
}

const maxConnectionIntervalMilliseconds int64 = (1<<63 - 1) / int64(time.Millisecond)

func connectionInterval(r *http.Request) (time.Duration, error) {
	interval := int64(1000)
	if value := r.URL.Query().Get("interval"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, errors.New("interval must be a positive integer in milliseconds")
		}
		interval = parsed
	}
	if interval <= 0 || interval > maxConnectionIntervalMilliseconds {
		return 0, errors.New("interval must be a positive integer in milliseconds")
	}
	return time.Duration(interval) * time.Millisecond, nil
}

func normalizedConnectionSnapshot(snapshot *statistic.Snapshot) *statistic.Snapshot {
	if snapshot.Connections == nil {
		snapshot.Connections = make([]*statistic.TrackerInfo, 0)
	}
	return snapshot
}

func closeConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if c := statistic.DefaultManager.Get(id); c != nil {
		_ = c.Close()
	}
	render.NoContent(w, r)
}
