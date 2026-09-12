package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/hostmap"
	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/domain/rules"
	"github.com/yousysadmin/ihttp/internal/domain/scope"
	"github.com/yousysadmin/ihttp/internal/models/project"
)

// Active is the open project with its settings compiled. Immutable once
// built: a change produces a new Active and swaps the pointer, so a hook
// mid-exchange keeps reading the snapshot it started with.
type Active struct {
	Project project.Project
	Scope   *scope.Scope

	// The intercept filters, nil where the setting is empty.
	InterceptRequest  filter.Expr
	InterceptResponse filter.Expr

	// LogIgnore is the request log's ignore filter, nil when empty.
	LogIgnore filter.Expr

	// Rules are the proxy rules, compiled.
	Rules []rules.Compiled

	// NoDecrypt says a host is relayed without being decrypted, nil
	// when the list is empty. Asked at CONNECT time, so it takes a host
	// rather than a URL.
	NoDecrypt func(host string) bool

	// Muted says a host is hidden from the log VIEW, nil when the list
	// is empty. Nothing about what is written reads it.
	Muted func(host string) bool

	// HostOverrides is where a name is dialled, nil when the list is
	// empty. Asked with the address a dialler was about to use, so it
	// can keep the port the request asked for.
	HostOverrides *hostmap.Map
}

// Watcher is told when the active project changes. prev or next is nil
// when a project was closed or opened from nothing. Called synchronously
// under the service lock, so a watcher must not call back into it.
type Watcher func(prev, next *Active)

// Service is the project list and the open project.
type Service struct {
	log   *slog.Logger
	store *Store
	bus   *eventbus.Bus

	mu       sync.Mutex
	active   atomic.Pointer[Active]
	watchers []Watcher
}

// NewService builds a Service.
func NewService(store *Store, bus *eventbus.Bus, log *slog.Logger) *Service {
	return &Service{store: store, bus: bus, log: log}
}

// maxNameLen bounds a project name, in characters.
const maxNameLen = 64

// Watch registers fn for activation changes.
func (s *Service) Watch(fn Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.watchers = append(s.watchers, fn)
}

// Active returns the open project snapshot, or nil.
func (s *Service) Active() *Active {
	return s.active.Load()
}

// ActiveID returns the open project's id, or "".
func (s *Service) ActiveID() string {
	if a := s.active.Load(); a != nil {
		return a.Project.ID
	}

	return ""
}

// List returns every project with IsActive filled in.
func (s *Service) List(ctx context.Context) ([]project.Project, error) {
	list, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}

	activeID := s.ActiveID()
	for i := range list {
		list[i].IsActive = list[i].ID == activeID
	}

	return list, nil
}

// Get returns one project, with IsActive filled in.
func (s *Service) Get(ctx context.Context, id string) (project.Project, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return project.Project{}, err
	}

	if p == nil {
		return project.Project{}, ErrNotFound
	}

	p.IsActive = p.ID == s.ActiveID()

	return *p, nil
}

// Create adds a project. It is not opened - the console does that as a
// second step so creating and switching stay two visible acts.
func (s *Service) Create(ctx context.Context, name string) (project.Project, error) {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return project.Project{}, ErrInvalidName
	}

	now := time.Now()
	p := project.Project{
		ID:        ids.New(),
		Name:      name,
		Settings:  project.Settings{Scope: []project.ScopeRule{}, Rules: []project.Rule{}},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.Insert(ctx, p); err != nil {
		return project.Project{}, err
	}

	s.bus.Emit("project.created", p)

	return p, nil
}

func validName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return false
	}

	for _, r := range name {
		if !unicode.IsPrint(r) {
			return false
		}
	}

	return true
}

// Import creates a project from one read out of a file: the settings and
// timestamps are the file's, the id is the caller's, and the name is the
// file's or, when taken, the file's with an "(imported)" suffix. The
// open project is not touched.
func (s *Service) Import(ctx context.Context, p project.Project, id string) (project.Project, error) {
	now := time.Now()

	p.ID = id
	p.IsActive = false
	p.UpdatedAt = now

	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}

	if p.Settings.Scope == nil {
		p.Settings.Scope = []project.ScopeRule{}
	}

	if p.Settings.Rules == nil {
		p.Settings.Rules = []project.Rule{}
	}

	for i := range p.Settings.Rules {
		p.Settings.Rules[i].Action.Normalize()
	}

	if _, err := Compile(p); err != nil {
		return project.Project{}, fmt.Errorf("%w: %w", ErrInvalidSettings, err)
	}

	base := strings.TrimSpace(p.Name)
	if !validName(base) {
		base = "imported"
	}

	for i := range 100 {
		p.Name = importName(base, i)

		err := s.store.Insert(ctx, p)
		if errors.Is(err, ErrNameTaken) {
			continue
		}

		if err != nil {
			return project.Project{}, err
		}

		s.bus.Emit("project.created", p)

		return p, nil
	}

	return project.Project{}, ErrNameTaken
}

// importName is base for the first attempt, then base with a suffix
// that counts up, trimmed so the whole stays a valid name.
func importName(base string, attempt int) string {
	suffix := ""

	switch attempt {
	case 0:
	case 1:
		suffix = " (imported)"
	default:
		suffix = fmt.Sprintf(" (imported %d)", attempt)
	}

	room := maxNameLen - utf8.RuneCountInString(suffix)
	if utf8.RuneCountInString(base) > room {
		base = string([]rune(base)[:room])
	}

	return base + suffix
}

// Open makes id the active project, replacing whatever was.
func (s *Service) Open(ctx context.Context, id string) (project.Project, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return project.Project{}, err
	}

	if p == nil {
		return project.Project{}, ErrNotFound
	}

	next, err := Compile(*p)
	if err != nil {
		return project.Project{}, fmt.Errorf("project: settings of %q do not compile: %w", p.Name, err)
	}

	s.swap(next)
	s.bus.Emit("project.opened", next.Project)
	s.log.Debug("project: opened", "id", p.ID, "name", p.Name, "rules", len(p.Settings.Rules),
		"scope_rules", len(p.Settings.Scope))

	// Remembered so a restart comes back to the same project. A failure
	// here costs the memory, not the open, so it is reported and not
	// returned.
	if err := s.store.SetActiveID(ctx, id); err != nil {
		s.log.Warn("project: could not remember the open project", "err", err)
	}

	return next.Project, nil
}

// Close closes the open project. Closing when nothing is open is fine.
func (s *Service) Close() {
	if prev := s.swap(nil); prev != nil {
		s.bus.Emit("project.closed", prev.Project)
		s.log.Debug("project: closed", "id", prev.Project.ID, "name", prev.Project.Name)

		if err := s.store.SetActiveID(context.Background(), ""); err != nil {
			s.log.Warn("project: could not forget the open project", "err", err)
		}
	}
}

// Restore reopens the project that was open when the process last ran.
// Nothing remembered, or a project since deleted, is not an error.
func (s *Service) Restore(ctx context.Context) error {
	id, err := s.store.ActiveID(ctx)
	if err != nil || id == "" {
		return err
	}

	_, err = s.Open(ctx, id)
	if errors.Is(err, ErrNotFound) {
		s.log.Debug("project: the remembered project is gone, forgetting it", "id", id)

		return s.store.SetActiveID(ctx, "")
	}

	return err
}

// Delete removes a project. The open one is refused: close it first, so
// the proxy is never left pointing at a project that is gone.
func (s *Service) Delete(ctx context.Context, id string) error {
	if id == s.ActiveID() {
		return ErrActive
	}

	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}

	s.bus.Emit("project.deleted", map[string]string{"id": id})

	return nil
}

// UpdateSettings applies change to the open project's settings, refusing
// anything that does not compile, then persists and re-activates.
func (s *Service) UpdateSettings(ctx context.Context, change func(*project.Settings) error) (project.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.active.Load()
	if prev == nil {
		return project.Project{}, ErrNoActiveProject
	}

	p := prev.Project
	if err := change(&p.Settings); err != nil {
		return project.Project{}, err
	}

	if p.Settings.Scope == nil {
		p.Settings.Scope = []project.ScopeRule{}
	}

	if p.Settings.Rules == nil {
		p.Settings.Rules = []project.Rule{}
	}

	for i := range p.Settings.Rules {
		if p.Settings.Rules[i].ID == "" {
			p.Settings.Rules[i].ID = ids.New()
		}

		p.Settings.Rules[i].Action.Normalize()
	}

	p.UpdatedAt = time.Now()

	next, err := Compile(p)
	if err != nil {
		return project.Project{}, fmt.Errorf("%w: %w", ErrInvalidSettings, err)
	}

	if err := s.store.UpdateSettings(ctx, p); err != nil {
		return project.Project{}, err
	}

	s.active.Store(next)
	s.notify(prev, next)
	s.bus.Emit("project.settings", next.Project)

	return next.Project, nil
}

// swap replaces the active pointer under the lock and tells watchers.
func (s *Service) swap(next *Active) *Active {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.active.Load()
	s.active.Store(next)
	s.notify(prev, next)

	return prev
}

func (s *Service) notify(prev, next *Active) {
	for _, w := range s.watchers {
		w(prev, next)
	}
}

// hostGlob compiles a list of hosts into one matcher over hostmap's
// glob, so there is one syntax for "these hosts" in the product. The
// caller names the list in its own error, since there is more than one
// of them.
func hostGlob(patterns []string) (func(host string) bool, error) {
	cleaned := make([]string, 0, len(patterns))

	for _, raw := range patterns {
		pattern := strings.ToLower(strings.TrimSpace(raw))
		if pattern == "" {
			continue
		}

		if err := hostmap.CheckPattern(pattern); err != nil {
			return nil, err
		}

		cleaned = append(cleaned, pattern)
	}

	if len(cleaned) == 0 {
		return nil, nil
	}

	return func(host string) bool {
		for _, pattern := range cleaned {
			if hostmap.MatchHost(pattern, host) {
				return true
			}
		}

		return false
	}, nil
}

// Compile builds the Active snapshot for p, which is also how settings
// are validated before they are stored.
func Compile(p project.Project) (*Active, error) {
	sc, err := scope.Compile(p.Settings.Scope)
	if err != nil {
		return nil, err
	}

	reqFilter, err := compileFilter(p.Settings.Intercept.RequestFilter)
	if err != nil {
		return nil, fmt.Errorf("request filter: %w", err)
	}

	resFilter, err := compileFilter(p.Settings.Intercept.ResponseFilter)
	if err != nil {
		return nil, fmt.Errorf("response filter: %w", err)
	}

	ignore, err := compileFilter(p.Settings.RequestLog.IgnoreFilter)
	if err != nil {
		return nil, fmt.Errorf("ignore filter: %w", err)
	}

	for _, key := range filter.Keys(ignore) {
		if strings.HasPrefix(key, "res.") || strings.HasPrefix(key, "ws.") {
			return nil, fmt.Errorf("ignore filter: %s is not known when a request is logged - only req.* fields apply", key)
		}
	}

	compiledRules, err := rules.Compile(p.Settings.Rules)
	if err != nil {
		return nil, err
	}

	// A view is only ever read by the console, so nothing compiled is
	// kept - but a query that does not parse is refused here rather than
	// failing every time the view is picked, and the message names the
	// view so it is clear which one is at fault.
	if err := checkViews(p.Settings.Views); err != nil {
		return nil, err
	}

	noDecrypt, err := hostGlob(p.Settings.NoDecrypt)
	if err != nil {
		return nil, fmt.Errorf("do not decrypt: %w", err)
	}

	muted, err := hostGlob(p.Settings.RequestLog.MutedHosts)
	if err != nil {
		return nil, fmt.Errorf("muted hosts: %w", err)
	}

	overrides, err := compileHostOverrides(p.Settings.HostOverrides)
	if err != nil {
		return nil, fmt.Errorf("host overrides: %w", err)
	}

	// The list lives in another domain, so whether the id EXISTS is
	// resolved when a request goes out - a project imported from another
	// machine may name one this one has never had. The shape is
	// checkable here, which catches a typo.
	if u := p.Settings.Upstream; u != "" && u != project.UpstreamDirect && !ids.Valid(u) {
		return nil, fmt.Errorf("upstream: %q is neither %q nor the id of a proxy", u, project.UpstreamDirect)
	}

	if n := p.Settings.RequestLog.MaxEntries; n < 0 || n > project.MaxEntriesLimit {
		return nil, fmt.Errorf("request log: max entries %d is outside 0 to %d, 0 for no cap", n, project.MaxEntriesLimit)
	}

	p.IsActive = true

	return &Active{
		Project:           p,
		Scope:             sc,
		InterceptRequest:  reqFilter,
		InterceptResponse: resFilter,
		LogIgnore:         ignore,
		Rules:             compiledRules,
		NoDecrypt:         noDecrypt,
		Muted:             muted,
		HostOverrides:     overrides,
	}, nil
}

// compileHostOverrides turns the settings list into the map the dialler
// asks. A disabled override is dropped here rather than checked on
// every connection, so what is compiled is what is in force.
func compileHostOverrides(list []project.HostOverride) (*hostmap.Map, error) {
	if len(list) > project.MaxHostOverrides {
		return nil, fmt.Errorf("%d overrides is more than the %d this holds", len(list), project.MaxHostOverrides)
	}

	entries := make([]hostmap.Entry, 0, len(list))

	for _, o := range list {
		if !o.Enabled {
			continue
		}

		entries = append(entries, hostmap.Entry{Host: o.Host, Address: o.Address})
	}

	return hostmap.Compile(entries)
}

// PassthroughProvider is what the proxy asks at CONNECT time: whether
// the open project said not to decrypt this host. Nothing open decrypts
// as usual, which is what the proxy did before there was a choice.
func (s *Service) PassthroughProvider() func(host string) bool {
	return func(host string) bool {
		a := s.Active()
		if a == nil || a.NoDecrypt == nil {
			return false
		}

		return a.NoDecrypt(host)
	}
}

// HostOverridesProvider is the open project's compiled overrides, or
// nil. Read per connection rather than held, so an edit takes effect on
// the next one.
func (s *Service) HostOverridesProvider() func() *hostmap.Map {
	return func() *hostmap.Map {
		if a := s.Active(); a != nil {
			return a.HostOverrides
		}

		return nil
	}
}

// RulesProvider is what the rules hook reads: the open project's compiled
// rules, or nil.
func (s *Service) RulesProvider() rules.Provider {
	return func() []rules.Compiled {
		if a := s.Active(); a != nil {
			return a.Rules
		}

		return nil
	}
}

// checkViews refuses a list that cannot be shown: too many, unnamed,
// two by one name, or a query that does not parse.
func checkViews(views []project.View) error {
	if len(views) > project.MaxViews {
		return fmt.Errorf("views: %d is more than the %d a project may keep", len(views), project.MaxViews)
	}

	seen := make(map[string]bool, len(views))

	for i, v := range views {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			return fmt.Errorf("views: view %d has no name", i+1)
		}

		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("views: two views are called %q", name)
		}

		seen[key] = true

		if _, err := compileFilter(v.Query); err != nil {
			return fmt.Errorf("views: %s: %w", name, err)
		}
	}

	return nil
}

// compileFilter parses a stored filter and checks its field names
// against the one HTTP vocabulary. Parsing alone accepts a typo -
// `req.nothing = 1` is a well-formed query - and the mistake would then
// surface every time the filter ran instead of once, here, when it was
// saved.
func compileFilter(src string) (filter.Expr, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}

	expr, err := filter.Parse(src)
	if err != nil {
		return nil, err
	}

	// A zero subject is the vocabulary: it answers every key the real
	// one does, and nothing else.
	if err := filter.Validate(expr, filter.HTTPSubject{}); err != nil {
		return nil, err
	}

	return expr, nil
}
