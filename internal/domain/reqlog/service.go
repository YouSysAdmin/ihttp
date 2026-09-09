package reqlog

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/proxy"
)

// Service is the request log: the proxy hook that writes it and the
// queries the console reads it with.
type Service struct {
	store    *Store
	projects *project.Service
	bus      *eventbus.Bus
	log      *slog.Logger

	// ws holds, by exchange id, a wsPending for a logged 101 and then the
	// wsRecorder of the connection it became.
	ws sync.Map

	// tunnels holds, by tunnel id, the project the opening half wrote
	// the entry to. Bounded by the tunnels open at once.
	tunnels sync.Map

	// writes counts entries written since the last trim, and trimming
	// says whether one is already running. Retention runs off the
	// request path and at most one at a time.
	writes   atomic.Int64
	trimming atomic.Bool
}

// trimEvery is how many writes go by between retention passes. Counting
// the log is a walk, so it is not done per request: the cap is honoured
// within this many entries of itself, which the setting's own text says.
const trimEvery = 64

// NewService builds a Service.
func NewService(store *Store, projects *project.Service, bus *eventbus.Bus, log *slog.Logger) *Service {
	return &Service{store: store, projects: projects, bus: bus, log: log}
}

// ListParams selects a page of the open project's log.
type ListParams struct {
	// Search is a filter query, or "" for everything.
	Search string

	// OnlyInScope keeps entries at least one scope rule matches.
	OnlyInScope bool

	// Saved selects the saved view. The log view and the saved view
	// share nothing: an entry is in one of them.
	Saved bool

	// Before is the id to page back from, or "" for the newest.
	Before string

	// Limit is the page size, clamped to [1, 500].
	Limit int

	// HonourMutes hides entries whose host the project muted. The
	// console's list sets it and an export never does, because muting is
	// about what you are looking at rather than what you have.
	HonourMutes bool
}

// scanBatch bounds how many entries one walk decodes when a search
// matches nothing, so a filter over a large log still returns.
const scanBatch = 5000

// List returns a page of summaries, newest first, and whether more
// remain. A search that walks scanBatch entries without filling the page
// stops there and reports more, so the console can ask again from the
// last id it saw.
func (s *Service) List(ctx context.Context, p ListParams) ([]reqlog.Summary, bool, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, false, project.ErrNoActiveProject
	}

	limit := clampLimit(p.Limit, 100, 500)

	match, err := s.matcher(active, p)
	if err != nil {
		return nil, false, err
	}

	out := make([]reqlog.Summary, 0, limit)
	scanned := 0

	more, err := s.store.Walk(ctx, active.Project.ID, p.Before, limit, func(e reqlog.Entry) (bool, bool, error) {
		scanned++

		ok, err := match(e)
		if err != nil {
			return false, true, err
		}

		if !ok {
			return false, scanned >= scanBatch, nil
		}

		out = append(out, e.Summarize())

		return true, false, nil
	})
	if err != nil {
		return nil, false, err
	}

	return out, more, nil
}

// TopBy names what Top ranks by.
type TopBy string

// The rankings. Duration is the whole exchange, Size is the response
// body, which is what a page that is too big is made of.
const (
	TopDuration TopBy = "duration"
	TopSize     TopBy = "size"
)

// Top returns the highest-ranking entries of the open project's log by
// duration or by response size, narrowed by the same search and scope
// switch a list takes.
//
// A ranking over the whole log rather than a sort of one page: sorting
// a page would lie across pages, and sorting the log needs an index it
// does not have. A full walk, like Tags and Hosts, so it is an action
// rather than something the list does as you type.
func (s *Service) Top(ctx context.Context, p ListParams, by TopBy, limit int) ([]reqlog.Summary, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	switch by {
	case TopDuration, TopSize:
	default:
		return nil, fmt.Errorf("%w: rank by %q, which is neither duration nor size", ErrBadRanking, by)
	}

	match, err := s.matcher(active, p)
	if err != nil {
		return nil, err
	}

	rank := func(e reqlog.Entry) int64 {
		if e.Response == nil {
			return -1
		}

		if by == TopSize {
			return int64(len(e.Response.Body))
		}

		return e.Response.DurationMS
	}

	limit = clampLimit(limit, 20, 200)
	out := make([]reqlog.Summary, 0, limit+1)
	scores := make([]int64, 0, limit+1)

	err = s.store.Each(ctx, active.Project.ID, func(e reqlog.Entry) error {
		ok, err := match(e)
		if err != nil || !ok {
			return err
		}

		// An exchange with no response has nothing to rank: it is
		// still in flight, or it never got an answer.
		score := rank(e)
		if score < 0 {
			return nil
		}

		// Highest first, so the insertion point is the first score
		// this one beats.
		at, _ := slices.BinarySearchFunc(scores, score, func(have, want int64) int {
			return cmp.Compare(want, have)
		})

		scores = slices.Insert(scores, at, score)
		out = slices.Insert(out, at, e.Summarize())

		if len(out) > limit {
			scores = scores[:limit]
			out = out[:limit]
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// Hosts counts the open project's log by host, busiest first, and says
// how many of each failed and whether the host is muted. The same full
// walk Tags does, which a log of tool size affords.
func (s *Service) Hosts(ctx context.Context) ([]HostCount, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	counts := map[string]*HostCount{}

	// Bodyless: a host count reads the URL and the status.
	err := s.store.EachBodyless(ctx, active.Project.ID, func(e reqlog.Entry) error {
		host := urlHost(e.URL)
		if host == "" {
			return nil
		}

		c, ok := counts[host]
		if !ok {
			c = &HostCount{Host: host, Muted: active.Muted != nil && active.Muted(host)}
			counts[host] = c
		}

		c.Count++

		if e.Response != nil && e.Response.StatusCode >= 500 {
			c.Errors++
		}

		// What a person scanning a host list is looking for: where the
		// failures are. A 4xx is the server refusing, which is often the
		// point of the request, so 5xx alone counts. An entry with no
		// response yet is not counted either way - it cannot be told
		// apart from one still in flight.

		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]HostCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}

	slices.SortFunc(out, func(a, b HostCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Host, b.Host))
	})

	return out, nil
}

// urlHost is the host of a stored URL, without the port, lowercased.
// Parsing is avoided: a log holds whatever the client sent, and a URL
// that will not parse still has a host worth counting.
func urlHost(rawURL string) string {
	rest := rawURL

	if _, after, ok := strings.Cut(rest, "://"); ok {
		rest = after
	}

	rest, _, _ = strings.Cut(rest, "/")
	rest, _, _ = strings.Cut(rest, "?")

	// An IPv6 host keeps its brackets in a URL, and its colons are not
	// a port separator.
	if strings.HasPrefix(rest, "[") {
		if host, _, ok := strings.Cut(strings.TrimPrefix(rest, "["), "]"); ok {
			return strings.ToLower(host)
		}
	}

	host, _, _ := strings.Cut(rest, ":")

	return strings.ToLower(host)
}

// matcher compiles the scope and search of p into one predicate, so a
// page of the log and an export of it agree on what is in.
func (s *Service) matcher(active *project.Active, p ListParams) (func(reqlog.Entry) (bool, error), error) {
	var expr filter.Expr
	if strings.TrimSpace(p.Search) != "" {
		var err error
		if expr, err = filter.Parse(p.Search); err != nil {
			return nil, err
		}
	}

	return func(e reqlog.Entry) (bool, error) {
		if p.Saved != e.Saved {
			return false, nil
		}

		// A muted host is hidden from a VIEW of the log and from nothing
		// else: the entry is written, kept and exported, and a query
		// that names the host finds it. Hence HonourMutes, which the
		// export does not set.
		if p.HonourMutes && active.Muted != nil && active.Muted(urlHost(e.URL)) {
			return false, nil
		}

		if p.OnlyInScope && !active.Scope.Match(e.URL, e.Headers.Lookup(), e.Body) {
			return false, nil
		}

		if expr == nil {
			return true, nil
		}

		return filter.Match(expr, Subject(e))
	}, nil
}

// ExportHAR prepares a HAR document of the entries p selects, oldest
// first. The filter is compiled here, before a byte is written, so a bad
// one is refused with a status. name is the project's, for the file.
func (s *Service) ExportHAR(ctx context.Context, p ListParams) (name string, write func(io.Writer) error, err error) {
	active := s.projects.Active()
	if active == nil {
		return "", nil, project.ErrNoActiveProject
	}

	match, err := s.matcher(active, p)
	if err != nil {
		return "", nil, err
	}

	projectID := active.Project.ID

	write = func(w io.Writer) error {
		return WriteHAR(w, func(emit func(reqlog.Entry) error) error {
			return s.store.Each(ctx, projectID, func(e reqlog.Entry) error {
				ok, err := match(e)
				if err != nil || !ok {
					return err
				}

				return emit(e)
			})
		})
	}

	return active.Project.Name, write, nil
}

// clampLimit is a page size: def when n is zero, never over cap.
func clampLimit(n, def, cap int) int {
	return max(1, min(cmp.Or(n, def), cap))
}

// Get returns one entry of the open project.
func (s *Service) Get(ctx context.Context, id string) (reqlog.Entry, error) {
	active := s.projects.Active()
	if active == nil {
		return reqlog.Entry{}, project.ErrNoActiveProject
	}

	e, err := s.store.Get(ctx, active.Project.ID, id)
	if err != nil {
		return reqlog.Entry{}, err
	}

	if e == nil {
		return reqlog.Entry{}, ErrNotFound
	}

	e.Annotate()

	return *e, nil
}

// trailersOf reads the trailers of a body the proxy drained. A streamed
// or truncated body was not read to its end, so there are none to read.
func trailersOf(res *http.Response, ex *proxy.Exchange) httpmsg.Headers {
	if ex.ResponseStreamed || ex.ResponseBodyTruncated || len(res.Trailer) == 0 {
		return nil
	}

	return httpmsg.FromHTTP(res.Trailer)
}

// Bounds on the marks, so a note is a note and a tag is a word.
const (
	MaxTags    = 32
	MaxTagLen  = 40
	MaxNoteLen = 4096
)

// SetMarks changes the tags, note or color of an entry, whichever the
// request names. Tags are trimmed, deduplicated without regard to case
// and bounded. The updated entry is announced as reqlog.updated.
func (s *Service) SetMarks(ctx context.Context, id string, in MarksRequest) (reqlog.Entry, error) {
	active := s.projects.Active()
	if active == nil {
		return reqlog.Entry{}, project.ErrNoActiveProject
	}

	var tags []string
	if in.Tags != nil {
		var err error
		if tags, err = normalizeTags(*in.Tags); err != nil {
			return reqlog.Entry{}, err
		}
	}

	if in.Note != nil && utf8.RuneCountInString(*in.Note) > MaxNoteLen {
		return reqlog.Entry{}, fmt.Errorf("%w: the note is longer than %d characters", ErrInvalidMarks, MaxNoteLen)
	}

	if in.Color != nil && !reqlog.ValidColor(*in.Color) {
		return reqlog.Entry{}, fmt.Errorf("%w: %q is not a color, use one of %s", ErrInvalidMarks, *in.Color, strings.Join(reqlog.Colors, ", "))
	}

	var out reqlog.Entry

	err := s.store.Update(ctx, active.Project.ID, id, func(e *reqlog.Entry) error {
		if in.Tags != nil {
			e.Tags = tags
		}

		if in.Note != nil {
			e.Note = strings.TrimSpace(*in.Note)
		}

		if in.Color != nil {
			e.Color = *in.Color
		}

		out = *e

		return nil
	})
	if err != nil {
		return reqlog.Entry{}, err
	}

	s.bus.Emit("reqlog.updated", out.Summarize())
	out.Annotate()

	return out, nil
}

// normalizeTags trims, drops empties, keeps the first spelling of a tag
// given twice in different cases, and bounds count and length.
func normalizeTags(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}

	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}

		if utf8.RuneCountInString(t) > MaxTagLen {
			return nil, fmt.Errorf("%w: tag %q is longer than %d characters", ErrInvalidMarks, t, MaxTagLen)
		}

		key := strings.ToLower(t)
		if seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, t)
	}

	if len(out) > MaxTags {
		return nil, fmt.Errorf("%w: more than %d tags", ErrInvalidMarks, MaxTags)
	}

	return out, nil
}

// Tags counts every tag in the open project's log, most used first. A
// full walk of the bucket, which a log of tool size affords.
func (s *Service) Tags(ctx context.Context) ([]TagCount, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	counts := map[string]*TagCount{}

	// Bodyless: a tag count reads the marks.
	err := s.store.EachBodyless(ctx, active.Project.ID, func(e reqlog.Entry) error {
		for _, t := range e.Tags {
			key := strings.ToLower(t)
			if c, ok := counts[key]; ok {
				c.Count++
			} else {
				counts[key] = &TagCount{Name: t, Count: 1}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]TagCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}

	slices.SortFunc(out, func(a, b TagCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Name, b.Name))
	})

	return out, nil
}

// Delete removes one entry of the open project.
func (s *Service) Delete(ctx context.Context, id string) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	if err := s.store.Delete(ctx, active.Project.ID, id); err != nil {
		return err
	}

	s.bus.Emit("reqlog.deleted", DeletedEvent{IDs: []string{id}})

	return nil
}

// DeleteMatching removes every entry the search and scope switch of p
// select, and reports how many went. With neither set it is Clear.
func (s *Service) DeleteMatching(ctx context.Context, p ListParams) (int, error) {
	active := s.projects.Active()
	if active == nil {
		return 0, project.ErrNoActiveProject
	}

	match, err := s.matcher(active, p)
	if err != nil {
		return 0, err
	}

	var ids []string

	// The matcher keeps to the view, so a delete from the log view never
	// reaches a saved entry.
	err = s.store.Each(ctx, active.Project.ID, func(e reqlog.Entry) error {
		ok, err := match(e)
		if err != nil {
			return err
		}

		if ok {
			ids = append(ids, e.ID)
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	if len(ids) == 0 {
		return 0, nil
	}

	// One transaction per batch: a filter over a large log may select
	// thousands, and bbolt holds the whole write in memory until commit.
	for chunk := range slices.Chunk(ids, 500) {
		if err := s.store.DeleteMany(ctx, active.Project.ID, chunk); err != nil {
			return 0, err
		}
	}

	s.bus.Emit("reqlog.deleted", DeletedEvent{IDs: ids})

	return len(ids), nil
}

// SetSaved moves an entry into the saved view, or back into the log. A
// saved entry survives Clear.
func (s *Service) SetSaved(ctx context.Context, id string, saved bool) (reqlog.Entry, error) {
	active := s.projects.Active()
	if active == nil {
		return reqlog.Entry{}, project.ErrNoActiveProject
	}

	var out reqlog.Entry

	err := s.store.Update(ctx, active.Project.ID, id, func(e *reqlog.Entry) error {
		e.Saved = saved
		e.SavedAt = time.Time{}

		if saved {
			e.SavedAt = time.Now()
		}

		out = *e

		return nil
	})
	if err != nil {
		return reqlog.Entry{}, err
	}

	s.bus.Emit("reqlog.updated", out.Summarize())
	out.Annotate()

	return out, nil
}

// Clear drops the open project's log, except what was saved. With no
// saved entry the buckets go in one stroke, otherwise the unsaved
// entries go one batch at a time.
func (s *Service) Clear(ctx context.Context) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	var unsaved []string

	keep := 0

	// Bodyless: this reads an id and a flag, and everything it collects
	// is about to be deleted.
	err := s.store.EachBodyless(ctx, active.Project.ID, func(e reqlog.Entry) error {
		if e.Saved {
			keep++
		} else {
			unsaved = append(unsaved, e.ID)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if keep == 0 {
		if err := s.store.Clear(ctx, active.Project.ID); err != nil {
			return err
		}
	} else {
		for chunk := range slices.Chunk(unsaved, 500) {
			if err := s.store.DeleteMany(ctx, active.Project.ID, chunk); err != nil {
				return err
			}
		}
	}

	s.bus.Emit("reqlog.cleared", map[string]string{"project_id": active.Project.ID})

	return nil
}

// logKey marks a request whose entry was written, so the response half
// knows where it belongs. Absent means the request was not logged.
type logKey struct{}

type logged struct {
	projectID string
}

// Request implements proxy.Hook: the request is written to the log of
// the open project, unless nothing is open, the log is paused, or the
// project bypasses requests outside its scope.
func (s *Service) Request(req *http.Request) (*http.Request, error) {
	ex := proxy.ExchangeFromContext(req.Context())
	active := s.projects.Active()

	if ex == nil || active == nil {
		return req, nil
	}

	// Before the body is read: a paused log does no work at all. The
	// response half only acts on what this half marked, so nothing else
	// needs to know.
	if active.Project.Settings.RequestLog.Paused {
		s.log.Debug("reqlog: paused, not logged", "id", ex.ID, "method", req.Method, "url", req.URL.String())

		return req, nil
	}

	body, _, err := httpmsg.ReadBody(req.Body, 1<<62)
	if err != nil {
		return nil, fmt.Errorf("reqlog: read request body: %w", err)
	}

	req.Body = httpmsg.Replace(body)

	if active.Project.Settings.RequestLog.BypassOutOfScope &&
		!active.Scope.Match(req.URL.String(), req.Header, body) {
		s.log.Debug("reqlog: out of scope, not logged", "id", ex.ID, "method", req.Method, "url", req.URL.String())

		return req, nil
	}

	if active.LogIgnore != nil {
		ignored, err := filter.Match(active.LogIgnore, filter.NewHTTPSubject(filter.Request{
			ID: ex.ID, Method: req.Method, URL: req.URL, Proto: req.Proto,
			Header: req.Header, Body: body, Timestamp: ex.Started,
		}, nil))
		if err != nil {
			s.log.Warn("reqlog: ignore filter failed, logging anyway", "err", err)
		} else if ignored {
			s.log.Debug("reqlog: ignored by filter", "id", ex.ID, "method", req.Method, "url", req.URL.String())

			return req, nil
		}
	}

	entry := reqlog.Entry{
		ID:            ex.ID,
		ProjectID:     active.Project.ID,
		CreatedAt:     ex.Started,
		Method:        req.Method,
		URL:           req.URL.String(),
		Proto:         req.Proto,
		Headers:       httpmsg.FromHTTP(req.Header),
		Body:          body,
		BodyTruncated: ex.RequestBodyTruncated,
	}

	if err := s.store.Put(req.Context(), entry); err != nil {
		s.log.Error("reqlog: store request", "err", err)

		return req, nil
	}

	s.bus.Emit("reqlog.request", entry.Summarize())
	s.maybeTrim(active)

	return req.WithContext(context.WithValue(req.Context(), logKey{}, logged{projectID: entry.ProjectID})), nil
}

// timingsOf copies the proxy's measurement into the stored shape. The
// two are separate types on purpose: the proxy owns no stored shape and
// the log owns no measuring. Nil stays nil, so an exchange a rule
// answered locally is stored as not measured rather than as all zeros.
func timingsOf(ex *proxy.Exchange) *reqlog.Timings {
	if ex.Timings == nil {
		return nil
	}

	t := ex.Timings

	return &reqlog.Timings{
		Blocked:    t.Blocked,
		DNS:        t.DNS,
		Connect:    t.Connect,
		TLS:        t.TLS,
		Send:       t.Send,
		Wait:       t.Wait,
		Receive:    t.Receive,
		Reused:     t.Reused,
		ServerAddr: t.ServerAddr,
		TLSVersion: t.TLSVersion,
		ALPN:       t.ALPN,
	}
}

// Trim applies the open project's entry cap now and reports how many
// entries it dropped. No cap is no work. Called by the proxy hook every
// trimEvery writes, and by hand from a test.
func (s *Service) Trim(ctx context.Context) (int, error) {
	active := s.projects.Active()
	if active == nil {
		return 0, project.ErrNoActiveProject
	}

	max := active.Project.Settings.RequestLog.MaxEntries
	if max <= 0 {
		return 0, nil
	}

	dropped, err := s.store.Trim(ctx, active.Project.ID, max)
	if err != nil {
		return 0, err
	}

	if dropped > 0 {
		s.log.Debug("reqlog: trimmed the log", "dropped", dropped, "max", max)
		s.bus.Emit("reqlog.deleted", DeletedEvent{IDs: nil, Trimmed: dropped})
	}

	return dropped, nil
}

// maybeTrim runs retention when the project asks for a cap and enough
// entries have gone by. It never blocks the request: the pass takes its
// own context and its own goroutine, and a second one is skipped while
// the first is still going.
func (s *Service) maybeTrim(active *project.Active) {
	if active.Project.Settings.RequestLog.MaxEntries <= 0 {
		return
	}

	if s.writes.Add(1) < trimEvery {
		return
	}

	s.writes.Store(0)

	if !s.trimming.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer s.trimming.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if _, err := s.Trim(ctx); err != nil {
			s.log.Error("reqlog: trim", "err", err)
		}
	}()
}

// Response implements proxy.Hook.
func (s *Service) Response(res *http.Response) error {
	ctx := res.Request.Context()

	l, ok := ctx.Value(logKey{}).(logged)
	ex := proxy.ExchangeFromContext(ctx)

	if !ok || ex == nil {
		return nil
	}

	body, _, err := httpmsg.ReadBody(res.Body, 1<<62)
	if err != nil {
		return fmt.Errorf("reqlog: read response body: %w", err)
	}

	res.Body = httpmsg.Replace(body)

	now := time.Now()
	stored := reqlog.Response{
		Proto:         res.Proto,
		StatusCode:    res.StatusCode,
		Status:        httpmsg.StatusReason(res.Status),
		Headers:       httpmsg.FromHTTP(res.Header),
		Trailers:      trailersOf(res, ex),
		Body:          body,
		BodyTruncated: ex.ResponseBodyTruncated,
		BodyStreamed:  ex.ResponseStreamed,
		ReceivedAt:    now,
		DurationMS:    now.Sub(ex.Started).Milliseconds(),
		Timings:       timingsOf(ex),
	}

	if res.StatusCode == http.StatusSwitchingProtocols {
		s.markUpgrade(ctx, l.projectID, ex, res)
	}

	// Off the response path: the client is waiting on this body and to
	// write is not its concern. The request context is gone once the
	// response is written, so to write gets a context of its own.
	go func() {
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		if err := s.store.SetResponse(wctx, l.projectID, ex.ID, stored); err != nil {
			s.log.Error("reqlog: store response", "id", ex.ID, "err", err)

			return
		}

		s.bus.Emit("reqlog.response", reqlog.Entry{
			ID: ex.ID, ProjectID: l.projectID, CreatedAt: ex.Started,
			Method: res.Request.Method, URL: res.Request.URL.String(), Proto: res.Request.Proto,
			Response: &stored,
		}.Summarize())
	}()

	return nil
}

var _ proxy.Hook = (*Service)(nil)
