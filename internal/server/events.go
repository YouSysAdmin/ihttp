package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/response"
)

// eventStream is GET /api/events: server-sent events for everything the
// bus publishes, so the console shows a request the moment the proxy
// sees it rather than on its next poll.
//
// A heartbeat comment every 20 seconds keeps an idle stream from being
// cut by a proxy or the browser's own idle timer.
func eventStream(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			response.Fail(w, http.StatusInternalServerError, "streaming is not supported")

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		d.Logger.Debug("event stream opened", "client", r.RemoteAddr)
		defer d.Logger.Debug("event stream closed", "client", r.RemoteAddr)

		// The first event tells the tab which project is open, so a
		// stream that reconnects after the process restarted resyncs.
		writeEvent(w, "hello", map[string]string{"active_project_id": d.Projects.ActiveID()})
		flusher.Flush()

		events := d.Bus.Subscribe(r.Context())
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				_, _ = fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			case e, ok := <-events:
				if !ok {
					return
				}

				writeEvent(w, e.Type, e.Data)
				flusher.Flush()
			}
		}
	})
}

func writeEvent(w http.ResponseWriter, typ string, data any) {
	body, err := response.Marshal(data)
	if err != nil {
		body = []byte("null")
	}

	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, body)
}
