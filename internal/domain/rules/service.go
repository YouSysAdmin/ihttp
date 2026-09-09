package rules

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/models/project"
	"github.com/yousysadmin/ihttp/internal/proxy"
)

// Provider hands the hook the compiled rules of the open project, or nil
// when nothing is open. A function rather than the project service, so
// this package can be imported by it.
type Provider func() []Compiled

// Service is the proxy hook that applies rules.
type Service struct {
	rules Provider
	log   *slog.Logger

	// vars are the values captured from traffic, for ${name} to put
	// back. Runtime only, see Var.
	vars vars

	// announce is told about a captured value, so the console can show
	// one arriving. Nil is allowed and means nobody is listening.
	announce func(Var)
}

// NewService builds the hook.
func NewService(rules Provider, log *slog.Logger) *Service {
	return &Service{rules: rules, log: log}
}

// OnCapture registers what to do when a value is captured. One
// listener, set at wiring time: this is the console's event stream, not
// a general subscription.
func (s *Service) OnCapture(fn func(Var)) {
	s.announce = fn
}

// matchedKey carries the rules that matched the request, so the response
// half applies the same set even if a rule rewrote the URL.
type matchedKey struct{}

// Request implements proxy.Hook. Every matched rule applies, whatever its
// position: a rule that answers instead of the upstream (mock, local file,
// CORS preflight) does not cut the chain, so a header set by a later rule
// is on the logged request and a delay still holds the answer back. The
// first such answer wins.
func (s *Service) Request(req *http.Request) (*http.Request, error) {
	var matched []Compiled

	// Built at most once per exchange, and only if a rule carries a
	// filter: reading the body back is a copy, but it is not free, and
	// most rules never need it.
	var subject filter.Subject

	compiled := s.rules()

	for _, c := range compiled {
		if c.Rule.Enabled && c.NeedsSubject() {
			subject = s.subjectOf(req)

			break
		}
	}

	for _, c := range compiled {
		if c.Matches(req, subject) {
			matched = append(matched, c)
			s.log.Debug("rules: matched", "rule", c.Rule.Name, "action", c.Rule.Action.Type,
				"method", req.Method, "url", req.URL.String())
		}
	}

	if len(matched) == 0 {
		return req, nil
	}

	req = req.WithContext(context.WithValue(req.Context(), matchedKey{}, matched))

	var answer *http.Response

	for _, c := range matched {
		a := c.Rule.Action

		switch a.Type {
		case project.ActionRewriteURL:
			if err := rewriteURL(req, c, s.expand); err != nil {
				s.log.Warn("rules: rewrite url", "rule", c.Rule.Name, "err", err)
			}
		case project.ActionSetRequestHeader:
			for _, h := range a.Headers {
				req.Header.Set(h.Name, s.expand(h.Value))
			}
		case project.ActionRemoveRequestHeader:
			for _, h := range a.Headers {
				req.Header.Del(h.Name)
			}
		case project.ActionReplaceRequestBody:
			if err := replaceRequestBody(req, c, s.expand); err != nil {
				s.log.Warn("rules: replace request body", "rule", c.Rule.Name, "err", err)
			}
		case project.ActionDelay:
			select {
			case <-time.After(time.Duration(a.DelayMS) * time.Millisecond):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		case project.ActionMock:
			if answer == nil {
				answer = mockResponse(a, s.expand)
			}
		case project.ActionBlock:
			if answer == nil {
				answer = blockResponse(a, c.Rule.Name)
			}
		case project.ActionMapLocal:
			if answer == nil {
				answer = localFileResponse(a.Path)
			}
		case project.ActionAllowCORS:
			if answer == nil && req.Method == http.MethodOptions {
				answer = preflightResponse(req)
			}
		}
	}

	if answer != nil {
		req = req.WithContext(proxy.WithResponse(req.Context(), answer))
	}

	return req, nil
}

// Response implements proxy.Hook. On a streamed response only the header
// actions apply: a new status or body would have to be framed again, and
// the frames are already on their way to the client.
func (s *Service) Response(res *http.Response) error {
	ctx := res.Request.Context()
	matched, _ := ctx.Value(matchedKey{}).([]Compiled)
	ex := proxy.ExchangeFromContext(ctx)
	streamed := ex != nil && ex.ResponseStreamed

	var captureSubject filter.Subject

	for _, c := range matched {
		a := c.Rule.Action

		// Throttle is the one action a streamed response still takes:
		// it wraps what goes to the client rather than editing a body
		// the hook was never handed.
		if streamed && (a.Type == project.ActionSetStatus || a.Type == project.ActionReplaceBody) {
			s.log.Debug("rules: action skipped on a streamed response", "rule", c.Rule.Name, "action", a.Type)

			continue
		}

		switch a.Type {
		case project.ActionSetResponseHeader:
			for _, h := range a.Headers {
				res.Header.Set(h.Name, s.expand(h.Value))
			}
		case project.ActionRemoveResponseHeader:
			for _, h := range a.Headers {
				res.Header.Del(h.Name)
			}
		case project.ActionSetStatus:
			setStatus(res, a.Status)
		case project.ActionReplaceBody:
			if err := replaceBody(res, c, s.expand); err != nil {
				return fmt.Errorf("rules: replace body: %w", err)
			}
		case project.ActionAllowCORS:
			allowCORS(res)
		case project.ActionDelayResponse:
			select {
			case <-time.After(time.Duration(a.DelayMS) * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		case project.ActionThrottle:
			throttleBody(ex, a.RateBPS)
		case project.ActionCapture:
			// Built once, and only if a capture asked for it: the
			// subject copies both bodies.
			if captureSubject == nil {
				captureSubject = s.exchangeSubject(res)
			}

			if captureSubject == nil {
				continue
			}

			if entry, ok := s.capture(c, captureSubject); ok && s.announce != nil {
				s.announce(entry)
			}
		}
	}

	return nil
}

// exchangeSubject adapts the finished exchange to the filter language,
// so a capture names what it reads with the same words a filter does.
//
// The response body is read and put back, a copy rather than a network
// read - and a STREAMED body is not there to read at all, which is why
// a capture from one finds nothing and says so.
func (s *Service) exchangeSubject(res *http.Response) filter.Subject {
	req := res.Request
	if req == nil {
		return nil
	}

	reqBody, _, err := httpmsg.ReadBody(req.Body, 1<<62)
	if err != nil {
		s.log.Warn("rules: read request body for a capture", "err", err)

		return nil
	}

	req.Body = httpmsg.Replace(reqBody)

	resBody, _, err := httpmsg.ReadBody(res.Body, 1<<62)
	if err != nil {
		s.log.Warn("rules: read response body for a capture", "err", err)

		return nil
	}

	res.Body = httpmsg.Replace(resBody)

	var (
		id      string
		started time.Time
	)

	ex := proxy.ExchangeFromContext(req.Context())
	if ex != nil {
		id, started = ex.ID, ex.Started
	}

	return filter.NewHTTPSubject(
		filter.FromRequest(req, reqBody, id, started),
		&filter.Response{
			StatusCode: res.StatusCode,
			Status:     res.Status,
			Proto:      res.Proto,
			Header:     res.Header,
			Trailer:    res.Trailer,
			Body:       resBody,
			Streamed:   ex != nil && ex.ResponseStreamed,
		},
	)
}

// subjectOf adapts the live request to the filter language. The body has
// been buffered by the proxy, so reading it and putting it back is a
// copy rather than a network read.
func (s *Service) subjectOf(req *http.Request) filter.Subject {
	body, _, err := httpmsg.ReadBody(req.Body, 1<<62)
	if err != nil {
		s.log.Warn("rules: read request body for a filter", "err", err)

		return nil
	}

	req.Body = httpmsg.Replace(body)

	var (
		id      string
		started time.Time
	)

	if ex := proxy.ExchangeFromContext(req.Context()); ex != nil {
		id = ex.ID
		started = ex.Started
	}

	return filter.NewHTTPSubject(filter.FromRequest(req, body, id, started), nil)
}

var _ proxy.Hook = (*Service)(nil)

func rewriteURL(req *http.Request, c Compiled, expand expander) error {
	next := c.pattern.ReplaceAllString(req.URL.String(), expand(c.Rule.Action.Replace))

	u, err := url.Parse(next)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%q is not an absolute url", next)
	}

	req.URL = u
	req.Host = u.Host

	return nil
}

// blockResponse is what a blocked request is answered with: a status and
// a line saying which rule did it, so a failure the operator arranged is
// not mistaken for one the network produced.
func blockResponse(a project.RuleAction, name string) *http.Response {
	code := a.Status
	if code == 0 {
		code = http.StatusBadGateway
	}

	body := fmt.Sprintf("blocked by ihttp rule %q\n", name)
	if name == "" {
		body = "blocked by an ihttp rule\n"
	}

	return &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Header: http.Header{
			"Content-Type":  []string{"text/plain; charset=utf-8"},
			"X-Ihttp-Block": []string{"1"},
		},
		Body: httpmsg.Replace([]byte(body)),
	}
}

// throttleBody paces what the client receives at bps bytes a second.
//
// It goes through the exchange rather than res.Body on purpose: a
// streamed body never reaches this hook, and the live stream is put back
// after the hooks whatever they do to res.Body. The wrapper the proxy
// applies afterwards catches both cases with one mechanism.
func throttleBody(ex *proxy.Exchange, bps int) {
	if bps <= 0 || ex == nil {
		return
	}

	ex.WrapResponseBody = func(body io.ReadCloser) io.ReadCloser {
		return &pacedReader{r: body, bps: bps}
	}
}

// pacedReader hands out at most bps bytes a second, waiting between
// reads rather than sleeping up front.
type pacedReader struct {
	r   io.ReadCloser
	bps int

	started time.Time
	read    int64
}

func (p *pacedReader) Read(out []byte) (int, error) {
	if p.started.IsZero() {
		p.started = time.Now()
	}

	// One second's worth at a time at most, so the wait between reads
	// stays small and a client sees a steady trickle rather than bursts.
	if len(out) > p.bps {
		out = out[:p.bps]
	}

	n, err := p.r.Read(out)
	p.read += int64(n)

	// How far ahead of the rate this read has put us.
	want := time.Duration(float64(p.read) / float64(p.bps) * float64(time.Second))
	if ahead := want - time.Since(p.started); ahead > 0 {
		time.Sleep(ahead)
	}

	return n, err
}

func (p *pacedReader) Close() error {
	return p.r.Close()
}

func setStatus(res *http.Response, code int) {
	res.StatusCode = code
	res.Status = fmt.Sprintf("%d %s", code, http.StatusText(code))
}

// replaceBody edits the buffered response body. A body too large to be
// buffered is on its way to the client already and is left alone.
func replaceBody(res *http.Response, c Compiled, expand expander) error {
	if ex := proxy.ExchangeFromContext(res.Request.Context()); ex != nil && ex.ResponseBodyTruncated {
		return fmt.Errorf("body larger than the buffer, not replaced")
	}

	body, _, err := httpmsg.ReadBody(res.Body, 1<<62)
	if err != nil {
		return err
	}

	out := c.substitute(body, expand)
	res.Body = httpmsg.Replace(out)
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", strconv.Itoa(len(out)))

	return nil
}

// replaceRequestBody edits the buffered request body. A body too large
// to be buffered is on its way to the upstream already and is left alone.
func replaceRequestBody(req *http.Request, c Compiled, expand expander) error {
	if ex := proxy.ExchangeFromContext(req.Context()); ex != nil && ex.RequestBodyTruncated {
		return fmt.Errorf("body larger than the buffer, not replaced")
	}

	body, _, err := httpmsg.ReadBody(req.Body, 1<<62)
	if err != nil {
		return err
	}

	out := c.substitute(body, expand)
	req.Body = httpmsg.Replace(out)
	req.ContentLength = int64(len(out))
	req.Header.Set("Content-Length", strconv.Itoa(len(out)))

	return nil
}

// mockResponse builds the canned answer. A body with no content type
// declared is guessed the cheap way: JSON if it looks like it.
func mockResponse(a project.RuleAction, expand expander) *http.Response {
	status := a.Status
	if status == 0 {
		status = http.StatusOK
	}

	h := http.Header{}
	for _, hd := range a.Headers {
		h.Add(hd.Name, expand(hd.Value))
	}

	// A mock body carries values too: an id echoed back, a token, a
	// timestamp - which is what makes it a stub rather than one fixed
	// answer.
	mocked := expand(a.Body)

	body := []byte(mocked)
	if len(body) > 0 && h.Get("Content-Type") == "" {
		trimmed := strings.TrimSpace(mocked)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			h.Set("Content-Type", "application/json")
		} else {
			h.Set("Content-Type", "text/plain; charset=utf-8")
		}
	}

	h.Set("Content-Length", strconv.Itoa(len(body)))

	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        h,
		Body:          httpmsg.Replace(body),
		ContentLength: int64(len(body)),
	}
}

// literal expands nothing, for the answers this package writes itself:
// a 404 from a missing local file, a CORS preflight. There is no
// operator text in either, and ${...} in one would be ours to explain.
func literal(text string) string { return text }

// localFileResponse serves a file from disk. A missing file is a 404 that
// says which path was looked for, so a typo in a rule is visible in the
// browser rather than silently proxied.
func localFileResponse(path string) *http.Response {
	data, err := os.ReadFile(path)
	if err != nil {
		// literal: this message is ours, not the operator's text.
		return mockResponse(project.RuleAction{
			Status: http.StatusNotFound,
			Body:   "ihttp map local: " + err.Error(),
		}, literal)
	}

	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = http.DetectContentType(data)
	}

	h := http.Header{}
	h.Set("Content-Type", ct)
	h.Set("Content-Length", strconv.Itoa(len(data)))

	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        h,
		Body:          httpmsg.Replace(data),
		ContentLength: int64(len(data)),
	}
}

// preflightResponse answers an OPTIONS request the way a permissive
// server would, echoing what the browser asked for.
func preflightResponse(req *http.Request) *http.Response {
	res := mockResponse(project.RuleAction{Status: http.StatusNoContent}, literal)
	res.Request = req
	allowCORS(res)

	if method := req.Header.Get("Access-Control-Request-Method"); method != "" {
		res.Header.Set("Access-Control-Allow-Methods", method)
	}

	if headers := req.Header.Get("Access-Control-Request-Headers"); headers != "" {
		res.Header.Set("Access-Control-Allow-Headers", headers)
	}

	res.Header.Set("Access-Control-Max-Age", "600")

	return res
}

// allowCORS adds the headers that let any origin read the response. The
// origin is echoed rather than starred so credentials keep working.
func allowCORS(res *http.Response) {
	origin := "*"
	if res.Request != nil {
		if o := res.Request.Header.Get("Origin"); o != "" {
			origin = o
		}
	}

	res.Header.Set("Access-Control-Allow-Origin", origin)
	res.Header.Set("Access-Control-Expose-Headers", "*")

	if origin != "*" {
		res.Header.Set("Access-Control-Allow-Credentials", "true")
		res.Header.Set("Vary", "Origin")
	}
}
