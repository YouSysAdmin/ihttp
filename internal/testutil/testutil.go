// Package testutil stands up the whole product in memory for a test:
// the database, every service, the proxy and the console API, wired the
// way `ihttp serve` wires them.
package testutil

import (
	"bufio"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/yousysadmin/ihttp/internal/core/blobstore"
	"github.com/yousysadmin/ihttp/internal/core/certgen"
	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	upstreamcore "github.com/yousysadmin/ihttp/internal/core/upstream"
	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/automation"
	"github.com/yousysadmin/ihttp/internal/domain/intercept"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/protoschema"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/domain/rules"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/domain/transfer"
	"github.com/yousysadmin/ihttp/internal/domain/upstreams"
	"github.com/yousysadmin/ihttp/internal/proxy"
	"github.com/yousysadmin/ihttp/internal/server"
)

// Stack is a running product.
type Stack struct {
	Projects   *project.Service
	ReqLogs    *reqlog.Service
	Intercept  *intercept.Service
	Sender     *sender.Service
	Automation *automation.Service
	Transfer   *transfer.Service
	Schemas    *protoschema.Service
	Upstreams  *upstreams.Service
	Bus        *eventbus.Bus
	CA         *certgen.Authority

	// Proxy is the MITM listening on a loopback port.
	Proxy *httptest.Server

	// Console is the API listening on a loopback port.
	Console *httptest.Server

	// DB is the open database, for a test that builds a second service
	// over the same file.
	DB *bolt.DB

	// Rules is the rules hook, for a test about captured values.
	Rules *rules.Service

	// LogStore is the log's store, for a test about storage itself -
	// what is in the database and what is in a file beside it.
	LogStore *reqlog.Store

	// BodyDir is where offloaded bodies are kept, for the same reason.
	BodyDir string
}

// Option adjusts how a Stack is built.
type Option func(*options)

type options struct {
	proxy    func(*proxy.Options)
	sender   func(*sender.Options)
	upstream upstreamcore.Config
	certs    *clientcert.Keeper

	// blobThreshold is the body size past which a body is kept in a
	// file. Small by default so the path is exercised, and a test that
	// wants everything in the database sets 0.
	blobThreshold int
}

// WithUpstreamDefault sets the instance-wide upstream proxy, the one a
// project that has chosen none goes out through.
func WithUpstreamDefault(cfg upstreamcore.Config) Option {
	return func(o *options) { o.upstream = cfg }
}

// WithBodyBlobs sets the size past which a body is kept in a file
// rather than in the database. Zero keeps every body in the database.
func WithBodyBlobs(threshold int) Option {
	return func(o *options) { o.blobThreshold = threshold }
}

// WithClientCerts gives every way out the certificates to present to an
// upstream that asks for one - the proxy, the sender and the automation
// together, the way serve.go wires them.
func WithClientCerts(k *clientcert.Keeper) Option {
	return func(o *options) { o.certs = k }
}

// WithProxy adjusts the proxy's options before it is built.
func WithProxy(fn func(*proxy.Options)) Option {
	return func(o *options) { o.proxy = fn }
}

// WithSender adjusts the sender's options before it is built. The
// automation gets the same upstream proxy, since it shares the sender's
// transports.
func WithSender(fn func(*sender.Options)) Option {
	return func(o *options) { o.sender = fn }
}

// New builds a Stack and tears it down with the test.
func New(t *testing.T, opts ...Option) *Stack {
	t.Helper()

	o := options{blobThreshold: 4096}
	for _, opt := range opts {
		opt(&o)
	}

	log := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db, err := database.Open(t.Context(), filepath.Join(t.TempDir(), "ihttp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	caCert, caKey, err := certgen.MintCA("ihttp test CA", "ihttp")
	if err != nil {
		t.Fatal(err)
	}

	ca, err := certgen.New(caCert, caKey)
	if err != nil {
		t.Fatal(err)
	}

	// Bodies go to files past the same threshold serve.go uses, so a
	// test exercises the offload path rather than a shortcut around it.
	// A small threshold, because a test body is small.
	bodyDir := filepath.Join(t.TempDir(), "bodies")

	blobs, err := blobstore.New(bodyDir, o.blobThreshold)
	if err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	projects := project.NewService(project.NewStore(db), bus, log)

	// The ways out, before anything that sends: the same wiring serve.go
	// does, so a test exercises the real path from a project's choice to
	// the connection.
	ways, err := upstreams.NewService(upstreams.NewStore(db), projects, o.upstream, log)
	if err != nil {
		t.Fatal(err)
	}

	upstreamProxy := ways.ProxyFunc()
	logStore := reqlog.NewStore(db, blobs)
	logs := reqlog.NewService(logStore, projects, bus, log)
	interceptor := intercept.NewService(projects, bus, log)
	sopts := sender.Options{InsecureSkipVerify: true, UpstreamProxy: upstreamProxy, ClientCerts: o.certs}
	if o.sender != nil {
		o.sender(&sopts)
	}

	sending := sender.NewService(sender.NewStore(db), projects, logs, bus, sopts)
	automating := automation.NewService(automation.NewStore(db), projects, logs, bus, automation.Options{
		InsecureSkipVerify: true,
		UpstreamProxy:      sopts.UpstreamProxy,
		ClientCerts:        o.certs,
		Logger:             log,
	})

	ruleHook := rules.NewService(projects.RulesProvider(), log)
	projects.Watch(func(_, _ *project.Active) { ruleHook.ClearVariables() })
	ruleHook.OnCapture(func(v rules.Var) { bus.Emit("rules.captured", v) })

	popts := proxy.Options{
		Logger:             log,
		MaxBody:            1 << 20,
		InsecureSkipVerify: true,
		UpstreamProxy:      upstreamProxy,
		ClientCerts:        o.certs,
		Passthrough:        projects.PassthroughProvider(),
	}
	if o.proxy != nil {
		o.proxy(&popts)
	}

	mitm := proxy.New(ca, []proxy.Hook{ruleHook, interceptor, logs}, popts)
	proxySrv := httptest.NewServer(mitm)
	t.Cleanup(proxySrv.Close)

	transferring := transfer.NewService(db, projects, log)
	schemas := protoschema.NewService(protoschema.NewStore(db), projects)

	consoleSrv := httptest.NewServer(server.New("", server.Deps{
		Projects: projects, ReqLogs: logs, Intercept: interceptor, Sender: sending, Automation: automating,
		Transfer: transferring, Schemas: schemas, Upstreams: ways, Rules: ruleHook,
		CA: ca, Bus: bus, Logger: log,
	}).Handler)
	t.Cleanup(consoleSrv.Close)

	return &Stack{
		Projects: projects, ReqLogs: logs, Intercept: interceptor, Sender: sending, Automation: automating,
		Transfer: transferring, Schemas: schemas, Upstreams: ways, Bus: bus, CA: ca,
		Proxy: proxySrv, Console: consoleSrv, DB: db, LogStore: logStore, BodyDir: bodyDir,
		Rules: ruleHook,
	}
}

// Client returns an http.Client that goes through the proxy and trusts
// its CA. It speaks HTTP/1.1 to the proxy.
func (s *Stack) Client() *http.Client {
	return s.client(false)
}

// ClientH2 is Client offering HTTP/2 inside the tunnel. A transport with
// its own TLS config does not attempt h2 unless told to.
func (s *Stack) ClientH2() *http.Client {
	return s.client(true)
}

// ClientWithTLS is Client with the TLS configuration a test wants inside
// the tunnel - for asserting WHICH certificate arrived, which is the only
// honest way to tell a decrypted connection from a relayed one.
func (s *Stack) ClientWithTLS(cfg *tls.Config) *http.Client {
	proxyURL, _ := url.Parse(s.Proxy.URL)

	return &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: cfg,
		},
	}
}

func (s *Stack) client(h2 bool) *http.Client {
	proxyURL, _ := url.Parse(s.Proxy.URL)

	return &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxyURL),
			TLSClientConfig:   &tls.Config{RootCAs: rootPool(s.CA)}, //nolint:gosec // Test only.
			ForceAttemptHTTP2: h2,
		},
	}
}

// DialTunnel opens a CONNECT tunnel through the proxy to host and, when
// tlsOn, completes a TLS handshake inside it against the stack's CA. The
// returned conn speaks to host as a client would after the CONNECT.
func (s *Stack) DialTunnel(t *testing.T, host string, tlsOn bool) net.Conn {
	t.Helper()

	proxyURL, _ := url.Parse(s.Proxy.URL)

	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: host}, Host: host, Header: http.Header{}}
	if err := connect.Write(conn); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(conn)

	// The body is not closed: a 200 to a CONNECT has no length, so Close
	// would drain the tunnel itself waiting for an end that never comes.
	res, err := http.ReadResponse(br, connect) //nolint:bodyclose // See above.
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT %s: %s", host, res.Status)
	}

	if br.Buffered() > 0 {
		t.Fatal("proxy sent bytes before the tunnel was used")
	}

	if !tlsOn {
		return conn
	}

	serverName, _, _ := net.SplitHostPort(host)

	tlsConn := tls.Client(conn, &tls.Config{RootCAs: rootPool(s.CA), ServerName: serverName}) //nolint:gosec // Test only.
	if err := tlsConn.Handshake(); err != nil {
		t.Fatal(err)
	}

	return tlsConn
}

// OpenProject creates and opens a project named name.
func (s *Stack) OpenProject(t *testing.T, name string) {
	t.Helper()

	p, err := s.Projects.Create(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Projects.Open(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
}

// Eventually polls cond every 20ms until it is true, failing the test
// with msg when timeout passes first.
func Eventually(t testing.TB, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %s", msg, timeout)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))

	return len(p), nil
}
