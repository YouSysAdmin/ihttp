// Package server is the console's HTTP edge: the JSON API under /api,
// the live event stream, and the embedded single-page console at /.
//
// A separate listener from the proxy on purpose: two ports are two
// unambiguous roles, and the console is never reachable through the
// proxy.
package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/certgen"
	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/automation"
	"github.com/yousysadmin/ihttp/internal/domain/intercept"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/protoschema"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/domain/rules"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/domain/transfer"
	"github.com/yousysadmin/ihttp/internal/domain/upstreams"
)

// Deps is everything the routes reach.
type Deps struct {
	Projects   *project.Service
	ReqLogs    *reqlog.Service
	Intercept  *intercept.Service
	Sender     *sender.Service
	Automation *automation.Service
	Transfer   *transfer.Service
	Schemas    *protoschema.Service
	Upstreams  *upstreams.Service
	Rules      *rules.Service
	CA         *certgen.Authority
	Bus        *eventbus.Bus
	Console    fs.FS
	Logger     *slog.Logger

	// Info is what GET /api/info reports about this process.
	Info Info
}

// Info describes the running instance to the console.
type Info struct {
	Version    string `json:"version"`
	ProxyAddr  string `json:"proxy_addr"`
	ProxyURL   string `json:"proxy_url"`
	ConsoleURL string `json:"console_url"`
	DataDir    string `json:"data_dir"`

	// CAPath is where the certificate authority is, which a setup
	// snippet has to name exactly - it is not always under DataDir,
	// since --ca-cert may put it anywhere.
	CAPath string `json:"ca_path"`

	// ProbeURL is the host the proxy answers itself, for checking a
	// setup with no network and no real target.
	ProbeURL string `json:"probe_url"`

	// UpstreamProxy is the proxy this instance goes out through, with
	// any password replaced by a mark. Empty when none is configured.
	UpstreamProxy string `json:"upstream_proxy,omitempty"`

	// UpstreamBypass are the host globs reached directly, as the
	// operator gave them. localhost is always direct and is not listed.
	UpstreamBypass []string `json:"upstream_bypass,omitempty"`

	// ClientCerts are the certificates this instance presents to an
	// upstream that asks for one, without the key material or the key
	// path. Instance configuration, so it is reported here rather than
	// given a route of its own.
	ClientCerts []clientcert.Summary `json:"client_certs,omitempty"`
}

// New builds the console server. Addr is the listen address.
func New(addr string, d Deps) *http.Server {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}

	mux := http.NewServeMux()
	registerRoutes(mux, d)

	return &http.Server{
		Addr:              addr,
		Handler:           accessLog(d.Logger, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          slog.NewLogLogger(d.Logger.Handler(), slog.LevelWarn),
	}
}

// accessLog writes one debug line per request. Debug, because the
// console polls nothing and the stream is one long request, so at info
// this would be noise beside the proxy's own traffic.
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		log.Debug("console", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"took", time.Since(started).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush keeps the event stream flushable through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// notFound answers an unknown API path in the error envelope, so a
// client never has to parse a plain-text 404.
func notFound(w http.ResponseWriter, r *http.Request) {
	response.Fail(w, http.StatusNotFound, "not found")
}
