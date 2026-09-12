// Package proxy is the machine-in-the-middle: an http.Handler that
// forwards plain requests, tunnels CONNECT with a certificate minted for
// the host, and runs every exchange through the hooks the product hangs
// on it - the intercept queue and the request log.
//
// The hooks run inside the transport rather than in the reverse proxy's
// director, so a hook that must block (an intercepted request waiting
// for a person) or refuse (a dropped one) does so at the one point where
// both the request and its eventual response pass through.
package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/certgen"
	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/core/hostmap"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/ids"
)

// ErrDropped is what a hook returns to refuse an exchange. The client is
// answered with a 502 that says so, and no later hook runs.
var ErrDropped = errors.New("proxy: dropped")

// Hook sees every exchange. Request may return a replacement request -
// one it edited, or the same one - and Response may edit the response in
// place. Hooks run in registration order for both halves. A streamed
// response (see Exchange.ResponseStreamed) reaches Response with an
// empty body, and any body edit made to it is discarded.
type Hook interface {
	Request(req *http.Request) (*http.Request, error)
	Response(res *http.Response) error
}

// Exchange is what the proxy knows about one request as it passes
// through, carried on the request context for the hooks.
type Exchange struct {
	ID      string
	Started time.Time

	// The body the hooks see is a prefix when a side sent more than
	// MaxBody. The rest is streamed after it and never seen by a hook.
	RequestBodyTruncated  bool
	ResponseBodyTruncated bool

	// ResponseStreamed says the response body is relayed to the client
	// as it arrives and was not captured at all: a protocol switch, or a
	// feed such as an event stream. The hooks see no body.
	ResponseStreamed bool

	// Timings is where the time went, nil for a response a hook answered
	// locally, which never reached the transport. Read it from the
	// response hook, by when every phase but a streamed body's receive
	// has been measured.
	Timings *Timings

	// WrapResponseBody, when a hook sets it, wraps the response body on
	// its way to the client, after the hooks have run.
	//
	// It is the ONLY way to touch a streamed body: that body never
	// reaches a hook, and the live stream is put back after them
	// whatever they did to res.Body. It works on a buffered body too,
	// and there it wraps the whole thing, prefix and remainder both.
	// Used by the throttle rule to pace what the client receives.
	WrapResponseBody func(io.ReadCloser) io.ReadCloser
}

type exchangeKey struct{}

// ExchangeFromContext returns the Exchange for a request the proxy is
// carrying, or nil for a request that did not come through it.
func ExchangeFromContext(ctx context.Context) *Exchange {
	e, _ := ctx.Value(exchangeKey{}).(*Exchange)

	return e
}

// responseKey carries a response a hook decided to send instead of
// asking the upstream - a mock, a local file, a CORS preflight answer.
type responseKey struct{}

// WithResponse attaches res to ctx as the answer to give. The transport
// skips the upstream and runs the response hooks on res as if it had
// arrived, so it is logged and can be intercepted like any other.
func WithResponse(ctx context.Context, res *http.Response) context.Context {
	return context.WithValue(ctx, responseKey{}, res)
}

func responseFromContext(ctx context.Context) *http.Response {
	res, _ := ctx.Value(responseKey{}).(*http.Response)

	return res
}

// WithExchange is for tests and for the sender, which reuses the hooks'
// context contract without going through the proxy.
func WithExchange(ctx context.Context, e *Exchange) context.Context {
	return context.WithValue(ctx, exchangeKey{}, e)
}

// Options configure a Proxy.
type Options struct {
	// MaxBody bounds how much of a body is buffered for the hooks. The
	// rest streams through untouched. Zero means 16 MiB.
	MaxBody int64

	// ConsoleURL is where the landing page sends a browser that hits the
	// proxy port directly.
	ConsoleURL string

	// InsecureSkipVerify makes the proxy accept any upstream certificate.
	// Off by default: the operator is inspecting their own traffic and a
	// target with a bad certificate is worth a visible 502. A research
	// target with a self-signed one is what the switch is for.
	InsecureSkipVerify bool

	// ClientCerts answers an upstream that asks for a client
	// certificate. Nil is the ordinary case: none is presented, which is
	// what the proxy did before it could.
	ClientCerts *clientcert.Keeper

	// DisableHTTP2 turns the tunnel back to HTTP/1.1 towards the client.
	// On by default, a client that offers h2 gets h2 inside a CONNECT
	// tunnel. Some clients misbehave against an h2 middlebox, and this
	// is the way out. The upstream side is unaffected.
	DisableHTTP2 bool

	// Passthrough decides, at CONNECT time, that a host is NOT to be
	// decrypted: the bytes are relayed between client and upstream and
	// nobody in the middle sees them. Nil decrypts everything.
	//
	// A callback rather than a list, so the decision can come from the
	// open project without this package importing a domain. Called with
	// the host from the CONNECT line, port stripped.
	Passthrough func(host string) bool

	// HostOverrides is where a name is dialled: a hosts file for the
	// proxy. The Host header, the SNI and the certificate check all come
	// from the URL, which is untouched, so only the TCP target moves and
	// the target sees the request it would have seen - unless the
	// override named a scheme, which also decides whether the hop to it
	// is TLS.
	//
	// A provider rather than a list, so the map can come from the open
	// project without this package importing a domain, and so an edit
	// takes effect on the next request. Nil overrides nothing.
	HostOverrides func() *hostmap.Map

	// ProbeHost is a host the proxy answers itself, so a setup can be
	// checked without a network and without a real target. Zero means
	// DefaultProbeHost. The answer is injected the way a rule's mock is,
	// so the exchange is logged - which is the point: a probe that only
	// proved the proxy saw it would prove less than one that turns up in
	// the log.
	ProbeHost string

	// UpstreamProxy is the proxy this proxy goes out through, for a lab
	// or a corporate network. Nil is http.ProxyFromEnvironment, so the
	// HTTP_PROXY family works as it always has. Build it with
	// upstream.Config.Compile.
	UpstreamProxy func(*http.Request) (*url.URL, error)

	// KeepWebSocketExtensions passes Sec-WebSocket-Extensions to the
	// upstream. Off by default, so no compression is negotiated and every
	// frame the proxy sees is readable. On, the wire is as the client
	// asked and a compressed message is recorded as opaque bytes.
	KeepWebSocketExtensions bool

	Logger *slog.Logger
}

// Proxy is the handler.
type Proxy struct {
	ca               *certgen.Authority
	hooks            []Hook
	keepWSExtensions bool

	// tlsConfig is what a tunnel handshakes with, built once. protocols
	// is what the tunnel's server speaks after the handshake.
	tlsConfig *tls.Config
	protocols *http.Protocols

	rp            *httputil.ReverseProxy
	log           *slog.Logger
	maxBody       int64
	landing       http.Handler
	probeHost     string
	passthrough   func(string) bool
	upstreamProxy func(*http.Request) (*url.URL, error)
	hostOverrides func() *hostmap.Map

	// transport is the base upstream transport and certs holds the
	// per-host clones of it, both kept so their idle connections can be
	// dropped when the overrides change.
	transport *http.Transport
	certs     *clientcert.Keeper
}

// New builds a Proxy that issues certificates from ca and runs hooks.
func New(ca *certgen.Authority, hooks []Hook, opts Options) *Proxy {
	if opts.MaxBody <= 0 {
		opts.MaxBody = 16 << 20
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// Defaulted before the struct, so the tunnel path and the transport
	// agree about the way out. Nil would send a passthrough tunnel
	// direct while everything else went through the environment's proxy.
	upstreamProxy := opts.UpstreamProxy
	if upstreamProxy == nil {
		upstreamProxy = http.ProxyFromEnvironment
	}

	// Defaulted here for the same reason: both dial paths and the
	// transport read it, and none should have to ask whether there is
	// one. A nil *Map matches nothing, so this is the whole of it.
	hostOverrides := opts.HostOverrides
	if hostOverrides == nil {
		hostOverrides = func() *hostmap.Map { return nil }
	}

	p := &Proxy{
		ca:               ca,
		hooks:            hooks,
		keepWSExtensions: opts.KeepWebSocketExtensions,
		log:              opts.Logger,
		maxBody:          opts.MaxBody,
		probeHost:        firstNonEmptyHost(opts.ProbeHost, DefaultProbeHost),
		passthrough:      opts.Passthrough,
		upstreamProxy:    upstreamProxy,
		hostOverrides:    hostOverrides,
		tlsConfig:        ca.TLSConfig(),
		protocols:        new(http.Protocols),
	}

	p.protocols.SetHTTP1(true)
	p.protocols.SetHTTP2(!opts.DisableHTTP2)

	if opts.DisableHTTP2 {
		p.tlsConfig.NextProtos = []string{"http/1.1"}
	}

	p.landing = newLanding(ca, opts.ConsoleURL)

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	upstream := &http.Transport{
		Proxy: upstreamProxy,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, p.dialAddr(addr))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,

		// The proxy decides compression itself: it narrows what the
		// client asks for to what it can decode, and decodes that, so
		// the hooks and the log see plain bodies.
		DisableCompression: true,

		TLSClientConfig: &tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}, //nolint:gosec // Operator's choice, see Options.
	}

	p.transport = upstream
	p.certs = opts.ClientCerts

	p.rp = &httputil.ReverseProxy{
		// The scheme switch goes INSIDE the hooks, so a request sent to
		// a plain dev server is still logged as the https the client
		// asked for.
		Transport:    &hookedTransport{p: p, next: hostmap.WrapTransport(opts.ClientCerts.Wrap(upstream), hostOverrides)},
		Rewrite:      rewrite,
		ErrorHandler: p.errorHandler,
		ErrorLog:     slog.NewLogLogger(opts.Logger.Handler(), slog.LevelDebug),
	}

	return p
}

// dialAddr applies the host overrides to an address about to be dialled
// and says which one was taken. Logged, because "it reached the wrong
// machine" is otherwise the hardest thing here to see: nothing in the
// request, the log or the certificate changes when an override fires.
func (p *Proxy) dialAddr(addr string) string {
	to := p.hostOverrides().Addr(addr)
	if to == "" || to == addr {
		return addr
	}

	p.log.Debug("proxy: host override", "addr", addr, "dialled", to)

	return to
}

// CloseIdleConnections drops the pooled upstream connections. The pool
// is keyed by the host a request named and NOT by the address that was
// dialled for it, so an override that changed would otherwise be
// ignored for as long as an idle socket to the old address lived.
func (p *Proxy) CloseIdleConnections() {
	p.transport.CloseIdleConnections()
	p.certs.CloseIdleConnections()
}

// ServeHTTP implements http.Handler. A CONNECT opens a tunnel, an
// absolute-form request is proxied, and anything else - a browser that
// opened the proxy port as if it were a site - gets the landing page.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.handleConnect(w, r)
	case r.URL.Host == "" || r.Host == landingHost:
		p.landing.ServeHTTP(w, r)
	case isWebSocketUpgrade(r):
		p.handleUpgrade(w, r)
	default:
		p.rp.ServeHTTP(w, r)
	}
}

// rewrite is the reverse proxy's director. The request arrived in
// absolute form (or was reconstructed by the tunnel), so the outbound
// URL is the inbound one and no host is rewritten.
func rewrite(pr *httputil.ProxyRequest) {
	pr.Out.URL = pr.In.URL
	pr.Out.Host = pr.In.Host

	// Forwarding headers would announce the proxy to the target.
	pr.Out.Header.Del("X-Forwarded-For")
	pr.Out.Header.Del("X-Forwarded-Host")
	pr.Out.Header.Del("X-Forwarded-Proto")

	narrowAcceptEncoding(pr.Out.Header)
}

// narrowAcceptEncoding keeps only the encodings the proxy can decode, so
// a server never answers with one the hooks would see as noise.
func narrowAcceptEncoding(h http.Header) {
	raw := h.Get("Accept-Encoding")
	if raw == "" {
		return
	}

	var kept []string

	for part := range strings.SplitSeq(raw, ",") {
		enc := strings.TrimSpace(part)
		name, _, _ := strings.Cut(enc, ";")

		switch strings.ToLower(strings.TrimSpace(name)) {
		case "gzip", "deflate", "identity":
			kept = append(kept, enc)
		}
	}

	if len(kept) == 0 {
		h.Del("Accept-Encoding")

		return
	}

	h.Set("Accept-Encoding", strings.Join(kept, ", "))
}

func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrDropped):
		p.log.Debug("proxy: dropped", "method", r.Method, "url", r.URL.String())
		http.Error(w, "request dropped by ihttp", http.StatusBadGateway)
	case errors.Is(err, context.Canceled):
		p.log.Debug("proxy: client went away", "url", r.URL.String())
	default:
		p.log.Warn("proxy: upstream failed", "url", r.URL.String(), "err", err)
		http.Error(w, "ihttp could not reach the upstream: "+err.Error(), http.StatusBadGateway)
	}
}

// hookedTransport is where the hooks run: around the one RoundTrip.
type hookedTransport struct {
	p    *Proxy
	next http.RoundTripper
}

func (t *hookedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ex := &Exchange{ID: ids.New(), Started: time.Now()}
	req = req.WithContext(WithExchange(req.Context(), ex))

	// Answered here rather than beside the landing page: the landing
	// page is served before the transport and so is never logged, and a
	// probe that does not reach the log cannot tell anyone their setup
	// works. This goes through the hooks like any local answer.
	if t.p.probeHost != "" && req.URL != nil && strings.EqualFold(req.URL.Hostname(), t.p.probeHost) {
		req = req.WithContext(WithResponse(req.Context(), probeResponse(req)))
	}

	body, rest, truncated, err := buffer(req.Body, t.p.maxBody)
	if err != nil {
		return nil, fmt.Errorf("proxy: read request body: %w", err)
	}

	ex.RequestBodyTruncated = truncated
	req.Body = httpmsg.Replace(body)

	for _, h := range t.p.hooks {
		req, err = h.Request(req)
		if err != nil {
			return nil, err
		}
	}

	if truncated {
		// The hooks saw the prefix and put it back. What the client is
		// still sending follows it on the wire.
		req.Body = joinBody(req.Body, rest)
		req.ContentLength = -1
	}

	res := responseFromContext(req.Context())
	local := res != nil

	// nil for a local answer, which never reaches the transport.
	var tc *tracer

	if local {
		res.Request = req
		if res.Proto == "" {
			res.Proto, res.ProtoMajor, res.ProtoMinor = "HTTP/1.1", 1, 1
		}

		if res.Body == nil {
			res.Body = http.NoBody
		}
	} else {
		// The trace goes on last, after the hooks have had the request:
		// a hook may replace it, and the replacement would not carry
		// the trace. Only the upstream path is measured - a local
		// answer has no phases to measure.
		tc = &tracer{}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), tc.trace()))

		res, err = t.next.RoundTrip(req)
		if err != nil {
			return nil, err
		}
	}

	rest, resBytes, err := t.captureResponse(ex, res)
	if err != nil {
		_ = res.Body.Close()

		return nil, err
	}

	// After the body was read, so the receive phase is closed, and
	// before the hooks run, so the log sees it. A streamed body is never
	// read here and leaves receive at zero on purpose.
	if tc != nil {
		if !ex.ResponseStreamed {
			tc.bodyRead()
		}

		timings := tc.timings()
		ex.Timings = &timings
	}

	// A live stream is kept out of the hooks' hands: they get no body,
	// and the stream is put back after them whatever they did to it.
	live := res.Body
	if ex.ResponseStreamed {
		res.Body = http.NoBody
	}

	for _, h := range t.p.hooks {
		if err := h.Response(res); err != nil {
			_ = live.Close()

			return nil, err
		}
	}

	switch {
	case ex.ResponseStreamed:
		res.Body = live
	case rest != nil:
		// The hooks saw the prefix and put it back. The rest of the
		// upstream body follows it and is never theirs to see.
		res.Body = joinBody(res.Body, rest)
	}

	// Last, so it wraps what the client actually gets: the live stream a
	// hook could not reach, or the whole buffered body with its
	// remainder already attached.
	if ex.WrapResponseBody != nil && res.Body != nil && res.Body != http.NoBody {
		res.Body = ex.WrapResponseBody(res.Body)
	}

	if t.p.log.Enabled(req.Context(), slog.LevelDebug) {
		t.p.log.Debug("proxy: exchange", exchangeAttrs(ex, req, res, len(body), resBytes, local)...)
	}

	return res, nil
}

// exchangeAttrs is the one debug line per exchange: what was asked, what
// came back, how long it took, and the flags that say the body the log
// holds is not the whole story.
func exchangeAttrs(ex *Exchange, req *http.Request, res *http.Response, reqBytes, resBytes int, local bool) []any {
	attrs := []any{
		"id", ex.ID,
		"method", req.Method,
		"url", req.URL.String(),
		"proto", req.Proto,
		"status", res.StatusCode,
		"upstream_proto", res.Proto,
		"took", time.Since(ex.Started).Round(time.Millisecond),
		"req_bytes", reqBytes,
		"res_bytes", resBytes,
	}

	if local {
		attrs = append(attrs, "local", true)
	}

	if ex.RequestBodyTruncated || ex.ResponseBodyTruncated {
		attrs = append(attrs, "truncated", true)
	}

	if ex.ResponseStreamed {
		attrs = append(attrs, "streamed", true)
	}

	return attrs
}

// joinBody is prefix followed by rest, closing rest when closed.
func joinBody(prefix io.Reader, rest io.ReadCloser) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(prefix, rest), rest}
}

// captureResponse buffers the response body for the hooks and decodes
// it. A streamed body is left alone, and a body known to be too large
// is not read at all: the hooks see an empty body and the whole stream
// is returned as rest. A body that turns out too large is cut at
// maxBody and the remainder is rest, which RoundTrip attaches after
// the hooks have run. n is how many body bytes were captured.
func (t *hookedTransport) captureResponse(ex *Exchange, res *http.Response) (rest io.ReadCloser, n int, err error) {
	if streaming(res) {
		ex.ResponseStreamed = true

		return nil, 0, nil
	}

	if res.ContentLength > t.p.maxBody {
		ex.ResponseBodyTruncated = true
		rest = res.Body
		res.Body = httpmsg.Replace(nil)

		return rest, 0, nil
	}

	body, rest, truncated, err := buffer(res.Body, t.p.maxBody)
	if err != nil {
		return nil, 0, fmt.Errorf("proxy: read response body: %w", err)
	}

	ex.ResponseBodyTruncated = truncated
	res.Body = httpmsg.Replace(body)

	if truncated {
		return rest, len(body), nil
	}

	if err := httpmsg.Decompress(res); err != nil {
		t.p.log.Debug("proxy: could not decode response body", "id", ex.ID, "err", err)
	}

	return nil, len(body), nil
}

// streaming reports whether a response must be relayed as it arrives: a
// protocol switch, whose body is the tunnel itself, or a media type that
// is a feed rather than a document and has no end to wait for. gRPC is
// not in the list on purpose. Its body has no Content-Length either, but
// it is bounded, and the log needs it buffered to read grpc-status from
// the trailers.
func streaming(res *http.Response) bool {
	if res.StatusCode == http.StatusSwitchingProtocols {
		return true
	}

	mt := httpmsg.MediaType(res.Header.Get("Content-Type"))

	switch mt {
	case "text/event-stream", "application/x-ndjson", "application/stream+json",
		"application/json-seq", "multipart/x-mixed-replace":
		return true
	}

	if res.ContentLength < 0 && (strings.HasPrefix(mt, "audio/") || strings.HasPrefix(mt, "video/")) {
		return true
	}

	return false
}

// buffer reads up to limit bytes of r. When r holds more, the bytes read
// are returned with rest positioned after them and truncated set.
func buffer(r io.ReadCloser, limit int64) (body []byte, rest io.ReadCloser, truncated bool, err error) {
	if r == nil || r == http.NoBody {
		return nil, nil, false, nil
	}

	var buf bytes.Buffer

	n, err := io.CopyN(&buf, r, limit+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, false, err
	}

	if n <= limit {
		_ = r.Close()

		return buf.Bytes(), nil, false, nil
	}

	data := buf.Bytes()

	return data[:limit:limit], joinBody(bytes.NewReader(data[limit:]), r), true, nil
}
