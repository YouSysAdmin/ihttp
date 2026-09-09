package intercept

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/proxy"
)

// pending is one held exchange. answer receives the replacement, or
// nil to drop. done is closed once the waiting goroutine has moved on,
// so a late answer is refused rather than blocking forever.
//
// view is the wire shape, built ONCE when the exchange is captured and
// never rebuilt from the live message: the proxy goroutine owns that
// message and its body reader, and a console request listing the queue
// must not touch either. Items hands out copies of view.
type pending[T any] struct {
	item   T
	view   Item
	answer chan T
	done   chan struct{}

	// claimed is set by the forward that is building the answer, so a
	// second forward of the same id does not touch the item under it.
	claimed atomic.Bool
}

// Service is the queue and the proxy hook that feeds it.
type Service struct {
	projects *project.Service
	bus      *eventbus.Bus
	log      *slog.Logger

	mu        sync.Mutex
	requests  map[string]*pending[*http.Request]
	responses map[string]*pending[*http.Response]
}

// NewService builds a Service and hooks it to project changes: when
// intercepting is switched off or the project closes, whatever is
// waiting is released unmodified rather than left hanging.
func NewService(projects *project.Service, bus *eventbus.Bus, log *slog.Logger) *Service {
	s := &Service{
		projects:  projects,
		bus:       bus,
		log:       log,
		requests:  make(map[string]*pending[*http.Request]),
		responses: make(map[string]*pending[*http.Response]),
	}

	projects.Watch(s.onProjectChange)

	return s
}

func (s *Service) onProjectChange(prev, next *project.Active) {
	if prev == nil {
		return
	}

	releaseRequests := next == nil || next.Project.ID != prev.Project.ID ||
		!next.Project.Settings.Intercept.RequestsEnabled
	releaseResponses := next == nil || next.Project.ID != prev.Project.ID ||
		!next.Project.Settings.Intercept.ResponsesEnabled

	if releaseRequests {
		s.releaseAllRequests()
	}

	if releaseResponses {
		s.releaseAllResponses()
	}
}

// responseKey carries a per-request override of the response setting:
// the console can ask, while forwarding a request, to also see its
// response, or to let it through even though responses are intercepted.
type responseKey struct{}

// Request implements proxy.Hook. It blocks until the console answers or
// the client gives up.
func (s *Service) Request(req *http.Request) (*http.Request, error) {
	ex := proxy.ExchangeFromContext(req.Context())
	active := s.projects.Active()

	if ex == nil || active == nil || !active.Project.Settings.Intercept.RequestsEnabled {
		return req, nil
	}

	if ex.RequestBodyTruncated {
		s.log.Debug("intercept: request body too large to hold", "id", ex.ID)

		return req, nil
	}

	if active.InterceptRequest != nil {
		ok, err := filter.Match(active.InterceptRequest, requestSubject(req))
		if err != nil {
			return nil, fmt.Errorf("intercept: request filter: %w", err)
		}

		if !ok {
			return req, nil
		}
	}

	body, _, err := httpmsg.ReadBody(req.Body, 1<<62)
	if err != nil {
		return nil, fmt.Errorf("intercept: read request body: %w", err)
	}

	req.Body = httpmsg.Replace(body)

	p := &pending[*http.Request]{
		item:   req,
		view:   requestView(ex.ID, req, body),
		answer: make(chan *http.Request),
		done:   make(chan struct{}),
	}

	s.mu.Lock()
	s.requests[ex.ID] = p
	s.mu.Unlock()

	s.bus.Emit("intercept.request", p.view)
	s.log.Debug("intercept: holding request", "id", ex.ID, "method", req.Method, "url", req.URL.String())

	defer func() {
		close(p.done)
		s.mu.Lock()
		delete(s.requests, ex.ID)
		s.mu.Unlock()
		s.bus.Emit("intercept.done", map[string]string{"id": ex.ID, "kind": "request"})
	}()

	select {
	case mod := <-p.answer:
		if mod == nil {
			s.log.Debug("intercept: request dropped", "id", ex.ID,
				"held_for", time.Since(p.view.ReceivedAt).Round(time.Millisecond))

			return nil, proxy.ErrDropped
		}

		s.log.Debug("intercept: request forwarded", "id", ex.ID,
			"held_for", time.Since(p.view.ReceivedAt).Round(time.Millisecond))

		return mod, nil
	case <-req.Context().Done():
		s.log.Debug("intercept: client gave up on a held request", "id", ex.ID)

		return nil, req.Context().Err()
	}
}

// Response implements proxy.Hook.
func (s *Service) Response(res *http.Response) error {
	ctx := res.Request.Context()
	ex := proxy.ExchangeFromContext(ctx)
	active := s.projects.Active()

	if ex == nil || active == nil {
		return nil
	}

	enabled := active.Project.Settings.Intercept.ResponsesEnabled
	if want, ok := ctx.Value(responseKey{}).(bool); ok {
		enabled = want
	}

	// A prefix or a live stream cannot be held: there is nothing whole to
	// show, and for a stream the body is the client connection itself.
	if !enabled || ex.ResponseBodyTruncated || ex.ResponseStreamed {
		return nil
	}

	if active.InterceptResponse != nil {
		ok, err := filter.Match(active.InterceptResponse, responseSubject(res))
		if err != nil {
			return fmt.Errorf("intercept: response filter: %w", err)
		}

		if !ok {
			return nil
		}
	}

	body, _, err := httpmsg.ReadBody(res.Body, 1<<62)
	if err != nil {
		return fmt.Errorf("intercept: read response body: %w", err)
	}

	res.Body = httpmsg.Replace(body)

	p := &pending[*http.Response]{
		item:   res,
		view:   responseView(ex.ID, res, body),
		answer: make(chan *http.Response),
		done:   make(chan struct{}),
	}

	s.mu.Lock()
	s.responses[ex.ID] = p
	s.mu.Unlock()

	s.bus.Emit("intercept.response", p.view)
	s.log.Debug("intercept: holding response", "id", ex.ID, "status", res.StatusCode, "url", res.Request.URL.String())

	defer func() {
		close(p.done)
		s.mu.Lock()
		delete(s.responses, ex.ID)
		s.mu.Unlock()
		s.bus.Emit("intercept.done", map[string]string{"id": ex.ID, "kind": "response"})
	}()

	select {
	case mod := <-p.answer:
		if mod == nil {
			s.log.Debug("intercept: response dropped", "id", ex.ID,
				"held_for", time.Since(p.view.ReceivedAt).Round(time.Millisecond))

			return proxy.ErrDropped
		}

		*res = *mod

		s.log.Debug("intercept: response forwarded", "id", ex.ID,
			"held_for", time.Since(p.view.ReceivedAt).Round(time.Millisecond))

		return nil
	case <-ctx.Done():
		s.log.Debug("intercept: client gave up on a held response", "id", ex.ID)

		return ctx.Err()
	}
}

var _ proxy.Hook = (*Service)(nil)

// Items lists what is waiting, oldest first. The items are the
// snapshots taken at capture, so listing never touches a message the
// proxy is holding.
func (s *Service) Items() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]Item, 0, len(s.requests)+len(s.responses))

	for _, p := range s.requests {
		items = append(items, p.view)
	}

	for _, p := range s.responses {
		items = append(items, p.view)
	}

	slices.SortFunc(items, func(a, b Item) int {
		return a.ReceivedAt.Compare(b.ReceivedAt)
	})

	return items
}

// Item returns one waiting exchange by request id.
func (s *Service) Item(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p, ok := s.requests[id]; ok {
		return p.view, nil
	}

	if p, ok := s.responses[id]; ok {
		return p.view, nil
	}

	return Item{}, ErrNotFound
}

// requestView is the wire shape of a held request, with the body the
// proxy already buffered.
func requestView(id string, req *http.Request, body httpmsg.Body) Item {
	return Item{
		ID:         id,
		Kind:       KindRequest,
		ReceivedAt: time.Now(),
		Request: &Request{
			Method:     req.Method,
			URL:        req.URL.String(),
			Proto:      req.Proto,
			Headers:    httpmsg.FromHTTP(req.Header),
			Body:       body,
			BodyBinary: httpmsg.IsBinary(req.Header.Get("Content-Type"), body),
			BodySize:   len(body),
		},
	}
}

// responseView is the wire shape of a held response and the request it
// answers.
func responseView(id string, res *http.Response, body httpmsg.Body) Item {
	return Item{
		ID:         id,
		Kind:       KindResponse,
		ReceivedAt: time.Now(),
		Request: &Request{
			Method:  res.Request.Method,
			URL:     res.Request.URL.String(),
			Proto:   res.Request.Proto,
			Headers: httpmsg.FromHTTP(res.Request.Header),
		},
		Response: &Response{
			Proto:      res.Proto,
			StatusCode: res.StatusCode,
			Status:     httpmsg.StatusReason(res.Status),
			Headers:    httpmsg.FromHTTP(res.Header),
			Trailers:   httpmsg.FromHTTP(res.Trailer),
			Body:       body,
			BodyBinary: httpmsg.IsBinary(res.Header.Get("Content-Type"), body),
			BodySize:   len(body),
		},
	}
}

// ForwardRequest releases a waiting request as in describes it. A nil
// in forwards the request unchanged.
func (s *Service) ForwardRequest(id string, in *ForwardRequest) error {
	s.mu.Lock()
	p, ok := s.requests[id]
	s.mu.Unlock()

	if !ok || !p.claimed.CompareAndSwap(false, true) {
		return ErrNotFound
	}

	out := p.item

	if in != nil {
		var err error
		if out, err = in.apply(p.item); err != nil {
			// The item is still waiting, and may be forwarded again.
			p.claimed.Store(false)

			return err
		}
	}

	if in != nil && in.InterceptResponse != nil {
		out = out.WithContext(context.WithValue(out.Context(), responseKey{}, *in.InterceptResponse))
	}

	select {
	case p.answer <- out:
		return nil
	case <-p.done:
		return ErrGone
	}
}

// DropRequest refuses a waiting request. The client gets a 502.
func (s *Service) DropRequest(id string) error {
	s.mu.Lock()
	p, ok := s.requests[id]
	s.mu.Unlock()

	if !ok {
		return ErrNotFound
	}

	select {
	case p.answer <- nil:
		return nil
	case <-p.done:
		return ErrGone
	}
}

// ForwardResponse releases a waiting response as in describes it.
func (s *Service) ForwardResponse(id string, in *ForwardResponse) error {
	s.mu.Lock()
	p, ok := s.responses[id]
	s.mu.Unlock()

	if !ok || !p.claimed.CompareAndSwap(false, true) {
		return ErrNotFound
	}

	out := p.item
	if in != nil {
		out = in.apply(p.item)
	}

	select {
	case p.answer <- out:
		return nil
	case <-p.done:
		return ErrGone
	}
}

// DropResponse refuses a waiting response. The client gets a 502.
func (s *Service) DropResponse(id string) error {
	s.mu.Lock()
	p, ok := s.responses[id]
	s.mu.Unlock()

	if !ok {
		return ErrNotFound
	}

	select {
	case p.answer <- nil:
		return nil
	case <-p.done:
		return ErrGone
	}
}

func (s *Service) releaseAllRequests() {
	s.mu.Lock()
	waiting := slices.Collect(maps.Values(s.requests))
	s.mu.Unlock()

	for _, p := range waiting {
		select {
		case p.answer <- p.item:
		case <-p.done:
		}
	}
}

func (s *Service) releaseAllResponses() {
	s.mu.Lock()
	waiting := slices.Collect(maps.Values(s.responses))
	s.mu.Unlock()

	for _, p := range waiting {
		select {
		case p.answer <- p.item:
		case <-p.done:
		}
	}
}

// apply builds the request the console described, on the original's
// context so the proxy's exchange and the client's cancellation ride
// along.
func (in ForwardRequest) apply(orig *http.Request) (*http.Request, error) {
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = orig.Method
	}

	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" {
		rawURL = orig.URL.String()
	}

	body := in.Body
	if body == nil {
		body, _, _ = httpmsg.ReadBody(orig.Body, 1<<62)
		orig.Body = httpmsg.Replace(body)
	}

	req, err := http.NewRequestWithContext(orig.Context(), method, rawURL, httpmsg.Replace(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadForward, err)
	}

	if req.URL.Scheme == "" || req.URL.Host == "" {
		return nil, fmt.Errorf("%w: url must be absolute", ErrBadForward)
	}

	if in.Headers != nil {
		req.Header = in.Headers.ToHTTP()
	} else {
		req.Header = orig.Header.Clone()
	}

	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(body))
	req.Host = req.URL.Host

	return req, nil
}

// apply builds the edited response, keeping the request it answers.
func (in ForwardResponse) apply(orig *http.Response) *http.Response {
	out := *orig

	if in.StatusCode > 0 {
		out.StatusCode = in.StatusCode
	}

	reason := strings.TrimSpace(in.Status)
	if reason == "" {
		reason = http.StatusText(out.StatusCode)
	}

	out.Status = fmt.Sprintf("%d %s", out.StatusCode, reason)

	if in.Headers != nil {
		out.Header = in.Headers.ToHTTP()
	} else {
		out.Header = orig.Header.Clone()
	}

	body := in.Body
	if body == nil {
		body, _, _ = httpmsg.ReadBody(orig.Body, 1<<62)
		orig.Body = httpmsg.Replace(body)
	}

	out.Header.Del("Content-Length")
	out.Header.Del("Content-Encoding")
	out.Body = httpmsg.Replace(body)
	out.ContentLength = int64(len(body))

	return &out
}
