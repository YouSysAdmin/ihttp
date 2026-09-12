package sender

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/hostmap"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	models "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/models/sender"
	"github.com/yousysadmin/ihttp/pkg"
)

// Service composes, stores and sends requests.
type Service struct {
	store    *Store
	projects *project.Service
	logs     *reqlog.Service
	bus      *eventbus.Bus
	log      *slog.Logger

	h2 http.RoundTripper
	h1 http.RoundTripper

	// The transports behind h2 and h1, and the per-host clones of them,
	// kept so their idle connections can be dropped when the host
	// overrides change.
	transports []*http.Transport
	certs      *clientcert.Keeper

	timeout time.Duration
	maxBody int64
}

// Options tune the client.
type Options struct {
	// Timeout bounds one send. Zero means 30 seconds.
	Timeout time.Duration

	// MaxBody bounds the response body kept. Zero means 16 MiB.
	MaxBody int64

	// InsecureSkipVerify accepts any server certificate. A research
	// target often has none worth trusting.
	InsecureSkipVerify bool

	// UpstreamProxy is the proxy this instance goes out through, so a
	// send leaves the machine the way a proxied request does. Nil is
	// http.ProxyFromEnvironment.
	UpstreamProxy func(*http.Request) (*url.URL, error)

	// ClientCerts answers an upstream that asks for a client
	// certificate, so a replay reaches an mTLS service the way the
	// proxied original did. Nil presents none.
	ClientCerts *clientcert.Keeper

	// HostOverrides is where a name is dialled, so a replay reaches the
	// machine the proxied original reached and speaks to it the same
	// way. Nil overrides nothing.
	HostOverrides func() *hostmap.Map

	// Logger gets one debug line per send. Nil means the default.
	Logger *slog.Logger
}

// NewTransports builds the two upstream transports a tool sends through:
// one that offers h2 and one that speaks HTTP/1 only, for a request that
// names its protocol. Compression is off so the body is read as sent.
//
// upstreamProxy and hostOverride are the same ones the MITM uses, so a
// replayed request leaves the machine the way the proxied original did
// and reaches the same place. Nil is http.ProxyFromEnvironment and no
// overrides.
func NewTransports(insecure bool, upstreamProxy func(*http.Request) (*url.URL, error),
	hostOverrides func() *hostmap.Map,
) (h2, h1 *http.Transport) {
	if upstreamProxy == nil {
		upstreamProxy = http.ProxyFromEnvironment
	}

	if hostOverrides == nil {
		hostOverrides = func() *hostmap.Map { return nil }
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if to := hostOverrides().Addr(addr); to != "" {
			addr = to
		}

		return dialer.DialContext(ctx, network, addr)
	}

	base := func() *http.Transport {
		return &http.Transport{
			Proxy:                 upstreamProxy,
			DialContext:           dial,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
			DisableCompression:    true,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec // Operator's choice, see Options.
		}
	}

	h2 = base()
	h2.ForceAttemptHTTP2 = true

	h1 = base()
	h1.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}

	return h2, h1
}

// NewService builds a Service with the two transports it switches
// between: one that attempts HTTP/2 and one that never does.
func NewService(store *Store, projects *project.Service, logs *reqlog.Service, bus *eventbus.Bus, opts Options) *Service {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	if opts.MaxBody <= 0 {
		opts.MaxBody = 16 << 20
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	h2t, h1t := NewTransports(opts.InsecureSkipVerify, opts.UpstreamProxy, opts.HostOverrides)
	h2 := hostmap.WrapTransport(opts.ClientCerts.Wrap(h2t), opts.HostOverrides)
	h1 := hostmap.WrapTransport(opts.ClientCerts.Wrap(h1t), opts.HostOverrides)

	return &Service{
		store:      store,
		projects:   projects,
		logs:       logs,
		bus:        bus,
		log:        opts.Logger,
		h2:         h2,
		h1:         h1,
		transports: []*http.Transport{h2t, h1t},
		certs:      opts.ClientCerts,
		timeout:    opts.Timeout,
		maxBody:    opts.MaxBody,
	}
}

// CloseIdleConnections drops the pooled connections. The pool is keyed
// by the host a request named and not by the address that was dialled
// for it, so a changed host override would otherwise be ignored for as
// long as an idle socket to the old address lived.
func (s *Service) CloseIdleConnections() {
	for _, tr := range s.transports {
		tr.CloseIdleConnections()
	}

	s.certs.CloseIdleConnections()
}

// List returns the open project's history, newest first, narrowed by
// search and scope like the request log.
func (s *Service) List(ctx context.Context, search string, onlyInScope bool) ([]sender.Summary, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	var expr filter.Expr
	if strings.TrimSpace(search) != "" {
		var err error
		if expr, err = filter.Parse(search); err != nil {
			return nil, err
		}
	}

	all, err := s.store.List(ctx, active.Project.ID)
	if err != nil {
		return nil, err
	}

	out := make([]sender.Summary, 0, len(all))

	for _, r := range all {
		if onlyInScope && !active.Scope.Match(r.URL, r.Headers.Lookup(), r.Body) {
			continue
		}

		if expr != nil {
			ok, err := filter.Match(expr, Subject(r))
			if err != nil {
				return nil, err
			}

			if !ok {
				continue
			}
		}

		out = append(out, r.Summarize())
	}

	return out, nil
}

// Get returns one request of the open project.
func (s *Service) Get(ctx context.Context, id string) (sender.Request, error) {
	active := s.projects.Active()
	if active == nil {
		return sender.Request{}, project.ErrNoActiveProject
	}

	r, err := s.store.Get(ctx, active.Project.ID, id)
	if err != nil {
		return sender.Request{}, err
	}

	if r == nil {
		return sender.Request{}, ErrNotFound
	}

	r.Annotate()

	return *r, nil
}

// Save creates a request, or rewrites the one in.ID names. A rewrite
// drops the stored response, because it answered a request that no
// longer exists.
func (s *Service) Save(ctx context.Context, in SaveRequest) (sender.Request, error) {
	active := s.projects.Active()
	if active == nil {
		return sender.Request{}, project.ErrNoActiveProject
	}

	if err := in.validate(); err != nil {
		return sender.Request{}, err
	}

	now := time.Now()
	r := sender.Request{
		ID:        in.ID,
		ProjectID: active.Project.ID,
		CreatedAt: now,
		UpdatedAt: now,
		Method:    in.method(),
		URL:       in.URL,
		Proto:     in.proto(),
		Headers:   in.headers(),
	}

	if in.Body != nil {
		r.Body = *in.Body
	}

	if r.ID == "" {
		r.ID = ids.New()
	} else {
		existing, err := s.store.Get(ctx, active.Project.ID, r.ID)
		if err != nil {
			return sender.Request{}, err
		}

		if existing == nil {
			return sender.Request{}, ErrNotFound
		}

		r.CreatedAt = existing.CreatedAt
		r.SourceLogID = existing.SourceLogID

		if in.Body == nil {
			r.Body = existing.Body
		}
	}

	if err := s.store.Put(ctx, r); err != nil {
		return sender.Request{}, err
	}

	s.bus.Emit("sender.saved", r.Summarize())

	return r, nil
}

// CloneFromLog makes a new request out of a log entry, ready to edit.
func (s *Service) CloneFromLog(ctx context.Context, logID string) (sender.Request, error) {
	active := s.projects.Active()
	if active == nil {
		return sender.Request{}, project.ErrNoActiveProject
	}

	e, err := s.logs.Get(ctx, logID)
	if err != nil {
		return sender.Request{}, err
	}

	now := time.Now()
	r := sender.Request{
		ID:          ids.New(),
		ProjectID:   active.Project.ID,
		SourceLogID: e.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Method:      e.Method,
		URL:         e.URL,
		Proto:       httpmsg.ProtoHTTP20,
		Headers:     e.Headers,
		Body:        e.Body,
	}

	if err := s.store.Put(ctx, r); err != nil {
		return sender.Request{}, err
	}

	s.bus.Emit("sender.saved", r.Summarize())

	return r, nil
}

// Send performs the request and stores what came back.
func (s *Service) Send(ctx context.Context, id string) (sender.Request, error) {
	active := s.projects.Active()
	if active == nil {
		return sender.Request{}, project.ErrNoActiveProject
	}

	// The stored shape, not Get's: what is annotated on read is not
	// written back.
	stored, err := s.store.Get(ctx, active.Project.ID, id)
	if err != nil {
		return sender.Request{}, err
	}

	r := *stored

	sendCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(sendCtx, r.Method, r.URL, httpmsg.Replace(r.Body))
	if err != nil {
		return sender.Request{}, fmt.Errorf("sender: build request: %w", err)
	}

	req.Header = r.Headers.ToHTTP()
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(r.Body))

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", pkg.AppName+"/"+pkg.Version)
	}

	transport := s.h2
	if r.Proto == httpmsg.ProtoHTTP10 || r.Proto == httpmsg.ProtoHTTP11 {
		transport = s.h1
	}

	started := time.Now()

	res, err := transport.RoundTrip(req)
	if err != nil {
		s.log.Debug("sender: send failed", "id", r.ID, "method", r.Method, "url", r.URL, "err", err)

		return sender.Request{}, &SendError{Err: err}
	}
	defer func() { _ = res.Body.Close() }()

	if err := httpmsg.Decompress(res); err != nil {
		return sender.Request{}, &SendError{Err: err}
	}

	body, truncated, err := httpmsg.ReadBody(res.Body, s.maxBody)
	if err != nil {
		return sender.Request{}, &SendError{Err: err}
	}

	now := time.Now()
	r.Response = &models.Response{
		Proto:         res.Proto,
		StatusCode:    res.StatusCode,
		Status:        httpmsg.StatusReason(res.Status),
		Headers:       httpmsg.FromHTTP(res.Header),
		Body:          body,
		BodyTruncated: truncated,
		ReceivedAt:    now,
		DurationMS:    now.Sub(started).Milliseconds(),
	}
	r.UpdatedAt = now

	// The body was read to its end, so the trailers have arrived.
	if !truncated && len(res.Trailer) > 0 {
		r.Response.Trailers = httpmsg.FromHTTP(res.Trailer)
	}

	s.log.Debug("sender: sent", "id", r.ID, "method", r.Method, "url", r.URL, "proto", res.Proto,
		"status", res.StatusCode, "took", now.Sub(started).Round(time.Millisecond),
		"res_bytes", len(body), "truncated", truncated)

	if err := s.store.Put(ctx, r); err != nil {
		return sender.Request{}, err
	}

	s.bus.Emit("sender.sent", r.Summarize())
	r.Annotate()

	return r, nil
}

// Delete removes one request of the open project.
func (s *Service) Delete(ctx context.Context, id string) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	if err := s.store.Delete(ctx, active.Project.ID, id); err != nil {
		return err
	}

	s.bus.Emit("sender.deleted", map[string]string{"id": id})

	return nil
}

// Clear drops the open project's history.
func (s *Service) Clear(ctx context.Context) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	if err := s.store.Clear(ctx, active.Project.ID); err != nil {
		return err
	}

	s.bus.Emit("sender.cleared", map[string]string{"project_id": active.Project.ID})

	return nil
}

// SendError is a request the upstream refused or never answered. It is a
// type of its own so the endpoint answers 502 and not 500: the fault is
// the target's, not ours.
type SendError struct {
	Err error
}

// Error implements error.
func (e *SendError) Error() string {
	return "sender: send failed: " + e.Err.Error()
}

// Unwrap exposes the transport error.
func (e *SendError) Unwrap() error {
	return e.Err
}

func (in SaveRequest) validate() error {
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrInvalidURL
	}

	if in.Proto != "" && !httpmsg.ValidProto(in.Proto) {
		return ErrBadProto
	}

	return nil
}

func (in SaveRequest) method() string {
	if m := strings.ToUpper(strings.TrimSpace(in.Method)); m != "" {
		return m
	}

	return http.MethodGet
}

func (in SaveRequest) proto() string {
	if in.Proto == "" {
		return httpmsg.ProtoHTTP20
	}

	return in.Proto
}

func (in SaveRequest) headers() httpmsg.Headers {
	if in.Headers == nil {
		return httpmsg.Headers{}
	}

	return in.Headers
}
