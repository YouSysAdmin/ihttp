// Package upstreams is the instance's list of proxies it can go out
// through, and which one the open project uses.
//
// The list is instance state: which ways out this machine has is a fact
// about the machine. A project holds only a REFERENCE to one of them, so
// switching project switches the way out without a restart, and a
// project exported to a teammate carries no credential of ours.
package upstreams

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/core/upstream"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	projectmodels "github.com/yousysadmin/ihttp/internal/models/project"
	upstreammodels "github.com/yousysadmin/ihttp/internal/models/upstream"
)

// The refusals a caller can act on.
var (
	ErrInvalidName = errors.New("upstreams: a proxy needs a name")
	ErrNameTaken   = errors.New("upstreams: that name is taken")
	ErrTooMany     = errors.New("upstreams: the list is full")

	// ErrInvalidConfig is a URL or a bypass glob that will not compile.
	// The message beneath it is core/upstream's, which names what is
	// wrong in the terms it was typed in.
	ErrInvalidConfig = errors.New("upstreams: invalid")
)

// Service owns the list and resolves what the open project goes out
// through.
type Service struct {
	store    *Store
	projects *project.Service
	log      *slog.Logger

	// fallback is the instance-wide --upstream-proxy, used by a project
	// that has not chosen one.
	fallback      upstream.Config
	fallbackProxy func(*http.Request) (*url.URL, error)

	// compiled caches one proxy function per server, keyed by id and the
	// time it was last written, so an edit takes effect at once and a
	// request pays for no parsing.
	mu       sync.RWMutex
	compiled map[string]cached

	// warned remembers a selection that could not be resolved, so the
	// log says so once rather than per request.
	warned sync.Map
}

type cached struct {
	at time.Time
	fn func(*http.Request) (*url.URL, error)
}

// NewService builds the service. fallback is the instance-wide
// configuration from the command line, used when a project has not
// chosen a proxy.
func NewService(store *Store, projects *project.Service, fallback upstream.Config, log *slog.Logger) (*Service, error) {
	fallbackProxy, err := fallback.Compile()
	if err != nil {
		return nil, err
	}

	return &Service{
		store:         store,
		projects:      projects,
		log:           log,
		fallback:      fallback,
		fallbackProxy: fallbackProxy,
		compiled:      map[string]cached{},
	}, nil
}

// List returns the summaries, which carry no credential: Host is the
// URL with any userinfo removed.
func (s *Service) List(ctx context.Context) ([]upstreammodels.Summary, error) {
	servers, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]upstreammodels.Summary, 0, len(servers))
	for _, srv := range servers {
		out = append(out, summarize(srv))
	}

	return out, nil
}

// Get returns one server WITH its URL, credentials included. Only the
// editor asks for this.
func (s *Service) Get(ctx context.Context, id string) (upstreammodels.Server, error) {
	return s.store.Get(ctx, id)
}

// SaveRequest is a new server or an edit to one.
type SaveRequest struct {
	ID     string   `json:"id,omitzero"`
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Bypass []string `json:"bypass,omitzero"`
}

// Save writes a server, refusing what cannot work before it is stored.
func (s *Service) Save(ctx context.Context, in SaveRequest) (upstreammodels.Server, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > upstreammodels.MaxNameLen {
		return upstreammodels.Server{}, fmt.Errorf("%w, of at most %d characters", ErrInvalidName, upstreammodels.MaxNameLen)
	}

	bypass := make([]string, 0, len(in.Bypass))
	for _, b := range in.Bypass {
		if b = strings.TrimSpace(b); b != "" {
			bypass = append(bypass, b)
		}
	}

	// The same check the command line gets, so a bad URL or a bad glob
	// is refused here rather than on the first request that uses it.
	cfg := upstream.Config{URL: in.URL, Bypass: bypass}
	if _, err := cfg.Compile(); err != nil {
		return upstreammodels.Server{}, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	if !cfg.Configured() {
		return upstreammodels.Server{}, fmt.Errorf("%w: %q needs a proxy URL", ErrInvalidConfig, name)
	}

	existing, err := s.store.List(ctx)
	if err != nil {
		return upstreammodels.Server{}, err
	}

	for _, srv := range existing {
		if srv.ID != in.ID && strings.EqualFold(srv.Name, name) {
			return upstreammodels.Server{}, fmt.Errorf("%w: %s", ErrNameTaken, name)
		}
	}

	now := time.Now().UTC()

	srv := upstreammodels.Server{
		ID:        in.ID,
		Name:      name,
		URL:       strings.TrimSpace(in.URL),
		Bypass:    bypass,
		UpdatedAt: now,
	}

	if srv.ID == "" {
		if len(existing) >= upstreammodels.MaxServers {
			return upstreammodels.Server{}, fmt.Errorf("%w at %d", ErrTooMany, upstreammodels.MaxServers)
		}

		srv.ID = ids.New()
		srv.CreatedAt = now
	} else {
		prev, err := s.store.Get(ctx, srv.ID)
		if err != nil {
			return upstreammodels.Server{}, err
		}

		srv.CreatedAt = prev.CreatedAt
	}

	if err := s.store.Put(ctx, srv); err != nil {
		return upstreammodels.Server{}, err
	}

	s.forget(srv.ID)
	s.log.Info("upstream proxy saved", "name", srv.Name, "host", upstream.Host(srv.URL))

	return srv, nil
}

// Delete removes a server. A project still pointing at it falls back to
// the instance default, which Resolve reports once.
func (s *Service) Delete(ctx context.Context, id string) error {
	srv, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}

	s.forget(id)
	s.log.Info("upstream proxy deleted", "name", srv.Name)

	return nil
}

// Test tries one server, or the instance default when id is empty.
func (s *Service) Test(ctx context.Context, id, target string) (upstream.Result, string, error) {
	cfg := s.fallback
	name := "the instance default"

	if id != "" {
		srv, err := s.store.Get(ctx, id)
		if err != nil {
			return upstream.Result{}, "", err
		}

		cfg = upstream.Config{URL: srv.URL, Bypass: srv.Bypass}
		name = srv.Name
	}

	res, err := cfg.Test(ctx, target)

	// Never the URL: the answer is shown in the console, and the name is
	// what the console knows this proxy by anyway.
	res.Proxy = name

	return res, name, err
}

// ProxyFunc is what an http.Transport takes. It asks the open project
// which way out to use on every request, so switching project switches
// the way out with no restart and no rebuilt transport.
//
// http.Transport keys its pooled connections by the proxy it dialled
// through, so a changed answer opens a new connection rather than
// reusing one to the wrong proxy.
func (s *Service) ProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		return s.resolve()(req)
	}
}

// resolve picks the proxy function for the open project.
func (s *Service) resolve() func(*http.Request) (*url.URL, error) {
	active := s.projects.Active()
	if active == nil {
		return s.fallbackProxy
	}

	switch choice := strings.TrimSpace(active.Project.Settings.Upstream); choice {
	case "":
		// Nothing chosen: the instance default, which is what every
		// project did before there was a list.
		return s.fallbackProxy
	case projectmodels.UpstreamDirect:
		return direct
	default:
		fn, err := s.serverProxy(choice)
		if err != nil {
			// A project pointing at a proxy this instance does not have
			// - deleted here, or imported from a machine that had it -
			// falls back rather than failing every request, and says so
			// once.
			if _, seen := s.warned.LoadOrStore(choice, true); !seen {
				s.log.Warn("the open project names an upstream proxy this instance does not have, going out the default way",
					"project", active.Project.Name, "upstream", choice, "err", err)
			}

			return s.fallbackProxy
		}

		return fn
	}
}

// serverProxy is the compiled function for one server, built once and
// rebuilt after an edit.
func (s *Service) serverProxy(id string) (func(*http.Request) (*url.URL, error), error) {
	srv, err := s.store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	hit, ok := s.compiled[id]
	s.mu.RUnlock()

	if ok && hit.at.Equal(srv.UpdatedAt) {
		return hit.fn, nil
	}

	fn, err := (upstream.Config{URL: srv.URL, Bypass: srv.Bypass}).Compile()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.compiled[id] = cached{at: srv.UpdatedAt, fn: fn}
	s.mu.Unlock()

	return fn, nil
}

func (s *Service) forget(id string) {
	s.mu.Lock()
	delete(s.compiled, id)
	s.mu.Unlock()

	s.warned.Delete(id)
}

// direct is the proxy function of a project that has said so: out on our
// own, whatever the instance default is.
func direct(*http.Request) (*url.URL, error) {
	return nil, nil
}

func summarize(srv upstreammodels.Server) upstreammodels.Summary {
	return upstreammodels.Summary{
		ID:             srv.ID,
		Name:           srv.Name,
		Host:           upstream.Host(srv.URL),
		Bypass:         srv.Bypass,
		HasCredentials: upstream.HasCredentials(srv.URL),
	}
}
