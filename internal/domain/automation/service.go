package automation

import (
	"context"
	"fmt"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/models/automation"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/pkg"
)

// Service composes, stores and runs automation jobs.
type Service struct {
	store    *Store
	projects *project.Service
	logs     *reqlog.Service
	bus      *eventbus.Bus
	log      *slog.Logger

	h2 http.RoundTripper
	h1 http.RoundTripper

	timeout time.Duration
	maxBody int64

	mu      sync.Mutex
	running map[string]*run
}

// run is one job in flight: how to end it early, and how to know it has
// wound down. Delete and Clear wait on done so no result lands after the
// job is gone.
type run struct {
	projectID string
	cancel    context.CancelFunc
	done      chan struct{}
}

// Options tune the client and the run.
type Options struct {
	// Timeout bounds one request. Zero means 30 seconds.
	Timeout time.Duration

	// MaxBody bounds the response body kept per result. Zero means
	// 256 KiB, small on purpose because a run keeps one body per payload.
	MaxBody int64

	// InsecureSkipVerify accepts any server certificate.
	InsecureSkipVerify bool

	// UpstreamProxy is the proxy this instance goes out through, so a
	// run leaves the machine the way a proxied request does. Nil is
	// http.ProxyFromEnvironment.
	UpstreamProxy func(*http.Request) (*url.URL, error)

	// ClientCerts answers an upstream that asks for a client
	// certificate, so a run reaches an mTLS service. Nil presents none.
	ClientCerts *clientcert.Keeper

	// Logger takes what a run cannot report to a caller, such as a result
	// that would not store. Nil means the default logger.
	Logger *slog.Logger
}

// NewService builds a Service with the two transports it switches
// between, like the sender: one that attempts HTTP/2 and one that never
// does.
func NewService(store *Store, projects *project.Service, logs *reqlog.Service, bus *eventbus.Bus, opts Options) *Service {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	if opts.MaxBody <= 0 {
		opts.MaxBody = 256 << 10
	}

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	h2t, h1t := sender.NewTransports(opts.InsecureSkipVerify, opts.UpstreamProxy)
	h2, h1 := opts.ClientCerts.Wrap(h2t), opts.ClientCerts.Wrap(h1t)

	return &Service{
		store:    store,
		projects: projects,
		logs:     logs,
		bus:      bus,
		log:      opts.Logger,
		h2:       h2,
		h1:       h1,
		timeout:  opts.Timeout,
		maxBody:  opts.MaxBody,
		running:  map[string]*run{},
	}
}

// List returns the open project's jobs, newest first, narrowed by a
// case-insensitive substring of the name or URL.
func (s *Service) List(ctx context.Context, search string) ([]automation.Summary, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	all, err := s.store.ListJobs(ctx, active.Project.ID)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(strings.TrimSpace(search))
	out := make([]automation.Summary, 0, len(all))

	for _, j := range all {
		if q != "" && !strings.Contains(strings.ToLower(j.Name), q) && !strings.Contains(strings.ToLower(j.URL), q) {
			continue
		}

		out = append(out, j.Summarize())
	}

	return out, nil
}

// Get returns one job of the open project.
func (s *Service) Get(ctx context.Context, id string) (automation.Job, error) {
	active := s.projects.Active()
	if active == nil {
		return automation.Job{}, project.ErrNoActiveProject
	}

	j, err := s.store.GetJob(ctx, active.Project.ID, id)
	if err != nil {
		return automation.Job{}, err
	}

	if j == nil {
		return automation.Job{}, ErrNotFound
	}

	j.Annotate()

	return *j, nil
}

// Save creates a job, or rewrites the one in.ID names. A rewrite returns
// the job to a draft and drops its results, because they answered a
// template that no longer exists.
func (s *Service) Save(ctx context.Context, in SaveJob) (automation.Job, error) {
	active := s.projects.Active()
	if active == nil {
		return automation.Job{}, project.ErrNoActiveProject
	}

	if err := s.validate(in); err != nil {
		return automation.Job{}, err
	}

	now := time.Now()
	j := automation.Job{
		ID:          in.ID,
		ProjectID:   active.Project.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Name:        in.Name,
		Method:      in.method(),
		URL:         in.URL,
		Proto:       in.proto(),
		Headers:     in.headers(),
		Placeholder: in.placeholder(),
		URLEncode:   in.URLEncode,
		Payload:     in.Payload,
		Concurrency: clampConcurrency(in.Concurrency),
		StopMatch:   in.stopMatch(),
		StopOn:      in.StopOn,
		Status:      automation.StatusDraft,
	}

	if in.Body != nil {
		j.Body = *in.Body
	}

	if j.ID == "" {
		j.ID = ids.New()
	} else {
		existing, err := s.store.GetJob(ctx, active.Project.ID, j.ID)
		if err != nil {
			return automation.Job{}, err
		}

		if existing == nil {
			return automation.Job{}, ErrNotFound
		}

		if s.isRunning(j.ID) {
			return automation.Job{}, ErrAlreadyRunning
		}

		j.CreatedAt = existing.CreatedAt

		if in.Body == nil {
			j.Body = existing.Body
		}

		if err := s.store.DeleteResults(ctx, active.Project.ID, j.ID); err != nil {
			return automation.Job{}, err
		}
	}

	if err := s.store.PutJob(ctx, j); err != nil {
		return automation.Job{}, err
	}

	s.bus.Emit("automation.job", j.Summarize())
	j.Annotate()

	return j, nil
}

// CloneFromLog makes a new draft job out of a log entry, ready to mark
// with placeholders and run.
func (s *Service) CloneFromLog(ctx context.Context, logID string) (automation.Job, error) {
	active := s.projects.Active()
	if active == nil {
		return automation.Job{}, project.ErrNoActiveProject
	}

	e, err := s.logs.Get(ctx, logID)
	if err != nil {
		return automation.Job{}, err
	}

	now := time.Now()
	j := automation.Job{
		ID:          ids.New(),
		ProjectID:   active.Project.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Method:      e.Method,
		URL:         e.URL,
		Proto:       httpmsg.ProtoHTTP20,
		Headers:     e.Headers,
		Body:        e.Body,
		Placeholder: "AUTO",
		URLEncode:   true,
		Payload:     automation.Payload{Kind: automation.PayloadLibrary},
		Concurrency: 10,
		Status:      automation.StatusDraft,
	}

	if err := s.store.PutJob(ctx, j); err != nil {
		return automation.Job{}, err
	}

	s.bus.Emit("automation.job", j.Summarize())
	j.Annotate()

	return j, nil
}

// Start expands the payloads, drops any previous results and runs the
// job in the background. It returns the job in its running state.
func (s *Service) Start(ctx context.Context, id string) (automation.Job, error) {
	active := s.projects.Active()
	if active == nil {
		return automation.Job{}, project.ErrNoActiveProject
	}

	stored, err := s.store.GetJob(ctx, active.Project.ID, id)
	if err != nil {
		return automation.Job{}, err
	}

	if stored == nil {
		return automation.Job{}, ErrNotFound
	}

	job := *stored

	positions, columns := job.CountPositions()
	if positions == 0 {
		return automation.Job{}, ErrNoPlaceholder
	}

	payloads, err := Expand(job.Payload)
	if err != nil {
		return automation.Job{}, &SpecError{Err: err}
	}

	// Every row must reach as far as the template does: $3 in the URL
	// wants three columns in each line.
	for i, row := range payloads {
		if len(row.Values) < columns {
			return automation.Job{}, &SpecError{Err: fmt.Errorf("line %d has %d values but the template uses $%d", i+1, len(row.Values), columns)}
		}
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	rn := &run{projectID: job.ProjectID, cancel: cancel, done: make(chan struct{})}

	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		cancel()

		return automation.Job{}, ErrAlreadyRunning
	}

	s.running[id] = rn
	s.mu.Unlock()

	// A failure before the goroutine starts must release the slot itself.
	abort := func() {
		cancel()
		s.deregister(id)
		close(rn.done)
	}

	if err := s.store.DeleteResults(ctx, job.ProjectID, job.ID); err != nil {
		abort()

		return automation.Job{}, err
	}

	now := time.Now()
	job.Status = automation.StatusRunning
	job.Total = len(payloads)
	job.Completed = 0
	job.Error = ""
	job.StartedAt = now
	job.FinishedAt = time.Time{}
	job.UpdatedAt = now

	if err := s.store.PutJob(ctx, job); err != nil {
		abort()

		return automation.Job{}, err
	}

	s.bus.Emit("automation.job", job.Summarize())
	s.log.Debug("automation: job started", "job", job.ID, "name", job.Name, "rows", len(payloads),
		"workers", clampConcurrency(job.Concurrency))

	go s.run(runCtx, rn, job, payloads)

	job.Annotate()

	return job, nil
}

// Stop cancels a running job of the open project. It becomes Stopped once
// its workers wind down.
func (s *Service) Stop(ctx context.Context, id string) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	s.mu.Lock()
	rn, ok := s.running[id]
	s.mu.Unlock()

	if !ok || rn.projectID != active.Project.ID {
		return ErrNotRunning
	}

	rn.cancel()

	return nil
}

// run fires the payloads through a bounded pool of workers, stores each
// result and reports progress, then records how the run ended. A result
// that trips a stop condition cancels the run. A request that fails after
// that is not recorded, since "context canceled" is not something the
// target said.
func (s *Service) run(ctx context.Context, rn *run, job automation.Job, payloads []Row) {
	defer close(rn.done)
	defer s.deregister(job.ID)

	workers := clampConcurrency(job.Concurrency)
	indices := make(chan int)
	tokens := automation.Tokens(job.Placeholder)

	var completed, failed atomic.Int64
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for i := range indices {
				if ctx.Err() != nil {
					continue
				}

				r := s.sendOne(ctx, job, tokens, i, payloads[i])

				if r.Error != "" && ctx.Err() != nil {
					continue
				}

				if matchesStop(job, r) {
					r.Matched = true
				}

				if err := s.store.PutResult(context.WithoutCancel(ctx), job.ProjectID, r); err != nil {
					failed.Add(1)
					s.log.Error("automation: result not stored", "job", job.ID, "index", i, "err", err)

					continue
				}

				done := completed.Add(1)
				s.bus.Emit("automation.result", resultEvent{JobID: job.ID, Result: r.Summarize()})

				if r.Matched {
					rn.cancel()
				}

				if done%20 == 0 {
					s.persist(job, int(done), automation.StatusRunning, "")
				}
			}
		})
	}

	stopped := false

feed:
	for i := range payloads {
		select {
		case <-ctx.Done():
			stopped = true

			break feed
		case indices <- i:
		}
	}

	close(indices)
	wg.Wait()

	status := automation.StatusDone
	reason := ""

	switch {
	case failed.Load() > 0:
		status = automation.StatusError
		reason = fmt.Sprintf("%d results could not be stored", failed.Load())
	case stopped || ctx.Err() != nil:
		status = automation.StatusStopped
	}

	s.persist(job, int(completed.Load()), status, reason)
	s.log.Debug("automation: job finished", "job", job.ID, "status", status, "completed", completed.Load(),
		"total", len(payloads), "took", time.Since(job.StartedAt).Round(time.Millisecond))
}

// sendOne performs one request with the row written into the template,
// and returns the result. A transport failure is recorded on the result,
// not returned, so one bad payload does not end the run.
func (s *Service) sendOne(ctx context.Context, job automation.Job, tokens *regexp.Regexp, index int, row Row) (r automation.Result) {
	r = automation.Result{ID: ids.New(), JobID: job.ID, Index: index, Payload: row.Raw}

	rawURL := fillURL(job.URL, tokens, row, job.URLEncode)
	body := []byte(fill(string(job.Body), tokens, row, nil))

	h := make(httpmsg.Headers, 0, len(job.Headers))
	for _, hd := range job.Headers {
		h = append(h, httpmsg.Header{Name: hd.Name, Value: fill(hd.Value, tokens, row, nil)})
	}

	r.ReqMethod = job.Method
	r.ReqURL = rawURL
	r.ReqHeaders = h
	r.ReqBody = body

	sendCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(sendCtx, job.Method, rawURL, httpmsg.Replace(body))
	if err != nil {
		r.Error = err.Error()

		return r
	}

	req.Header = h.ToHTTP()
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(body))

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", pkg.AppName+"/"+pkg.Version)
	}

	transport := s.h2
	if job.Proto == httpmsg.ProtoHTTP10 || job.Proto == httpmsg.ProtoHTTP11 {
		transport = s.h1
	}

	started := time.Now()
	defer func() { r.DurationMS = time.Since(started).Milliseconds() }()

	res, err := transport.RoundTrip(req)
	if err != nil {
		r.Error = err.Error()

		return r
	}
	defer func() { _ = res.Body.Close() }()

	if err := httpmsg.Decompress(res); err != nil {
		r.Error = err.Error()

		return r
	}

	respBody, truncated, err := httpmsg.ReadBody(res.Body, s.maxBody)
	if err != nil {
		r.Error = err.Error()

		return r
	}

	r.Proto = res.Proto
	r.StatusCode = res.StatusCode
	r.Status = httpmsg.StatusReason(res.Status)
	r.Headers = httpmsg.FromHTTP(res.Header)
	r.Body = respBody
	r.BodyTruncated = truncated

	return r
}

// fill writes the row into a template in one pass over its tokens: $N
// takes column N, the placeholder column 1. A missing column is written
// empty, though Start refuses such a row before the run. encode, when
// given, transforms each value on its way in.
func fill(tmpl string, tokens *regexp.Regexp, row Row, encode func(string) string) string {
	return tokens.ReplaceAllStringFunc(tmpl, func(tok string) string {
		n := automation.Column(tok)
		if n > len(row.Values) {
			return ""
		}

		v := row.Values[n-1]
		if encode != nil {
			v = encode(v)
		}

		return v
	})
}

// fillURL writes the row into the URL template. With encode, each value
// is percent-encoded for where it lands: the path before the first "?",
// the query after it.
func fillURL(tmpl string, tokens *regexp.Regexp, row Row, encode bool) string {
	if !encode {
		return fill(tmpl, tokens, row, nil)
	}

	path, query, hasQuery := strings.Cut(tmpl, "?")
	path = fill(path, tokens, row, url.PathEscape)

	if !hasQuery {
		return path
	}

	return path + "?" + fill(query, tokens, row, url.QueryEscape)
}

// Results returns the results of a job, in the order they ran.
func (s *Service) Results(ctx context.Context, jobID string) ([]automation.ResultSummary, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, project.ErrNoActiveProject
	}

	return s.store.ListResults(ctx, active.Project.ID, jobID)
}

// Result returns one result of a job, with its response.
func (s *Service) Result(ctx context.Context, jobID, id string) (automation.Result, error) {
	active := s.projects.Active()
	if active == nil {
		return automation.Result{}, project.ErrNoActiveProject
	}

	r, err := s.store.GetResult(ctx, active.Project.ID, jobID, id)
	if err != nil {
		return automation.Result{}, err
	}

	if r == nil {
		return automation.Result{}, ErrResultNotFound
	}

	r.Annotate()

	return *r, nil
}

// ExportHAR prepares a HAR document of a job's results: each request as
// sent and the response it drew. A result that failed in transport is a
// pending entry. Results carry no clock of their own, so every entry is
// stamped with the run's start. name is the job's, for the file.
func (s *Service) ExportHAR(ctx context.Context, id string) (name string, write func(io.Writer) error, err error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return "", nil, err
	}

	write = func(w io.Writer) error {
		return reqlog.WriteHAR(w, func(emit func(reqlogmodels.Entry) error) error {
			return s.store.EachResult(ctx, job.ProjectID, job.ID, func(r automation.Result) error {
				return emit(entryOf(job, r))
			})
		})
	}

	if name = job.Name; name == "" {
		name = "automation-" + job.ID
	}

	return name, write, nil
}

// entryOf reshapes a result as a log entry, which is what HAR and the
// snippets are spelled from.
func entryOf(job automation.Job, r automation.Result) reqlogmodels.Entry {
	e := reqlogmodels.Entry{
		ID:        r.ID,
		ProjectID: job.ProjectID,
		CreatedAt: job.StartedAt,
		Method:    r.ReqMethod,
		URL:       r.ReqURL,
		Proto:     job.Proto,
		Headers:   r.ReqHeaders,
		Body:      r.ReqBody,
	}

	if r.Error == "" && r.StatusCode != 0 {
		e.Response = &reqlogmodels.Response{
			Proto:         r.Proto,
			StatusCode:    r.StatusCode,
			Status:        r.Status,
			Headers:       r.Headers,
			Body:          r.Body,
			BodyTruncated: r.BodyTruncated,
			ReceivedAt:    job.StartedAt,
			DurationMS:    r.DurationMS,
		}
	}

	return e
}

// Delete removes one job and its results. A running job is stopped and
// waited for first, so no worker writes a result after the job is gone.
func (s *Service) Delete(ctx context.Context, id string) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	s.stopAndWait(ctx, active.Project.ID, id)

	if err := s.store.DeleteJob(ctx, active.Project.ID, id); err != nil {
		return err
	}

	s.bus.Emit("automation.deleted", map[string]string{"id": id})

	return nil
}

// Clear drops every job of the open project, stopping the ones that run.
func (s *Service) Clear(ctx context.Context) error {
	active := s.projects.Active()
	if active == nil {
		return project.ErrNoActiveProject
	}

	for _, id := range s.runningIn(active.Project.ID) {
		s.stopAndWait(ctx, active.Project.ID, id)
	}

	if err := s.store.Clear(ctx, active.Project.ID); err != nil {
		return err
	}

	s.bus.Emit("automation.cleared", map[string]string{"project_id": active.Project.ID})

	return nil
}

// stopAndWait cancels a run of the project and waits for its workers to
// wind down, or returns at once when the job is not running. The wait
// ends with the caller's context.
func (s *Service) stopAndWait(ctx context.Context, projectID, id string) {
	s.mu.Lock()
	rn, ok := s.running[id]
	s.mu.Unlock()

	if !ok || rn.projectID != projectID {
		return
	}

	rn.cancel()

	select {
	case <-rn.done:
	case <-ctx.Done():
	}
}

func (s *Service) runningIn(projectID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []string
	for id, rn := range s.running {
		if rn.projectID == projectID {
			out = append(out, id)
		}
	}

	return out
}

// persist writes the run state onto the stored job and announces it. A
// status other than running is final and stamps FinishedAt. A job that
// was deleted under the run is left deleted.
func (s *Service) persist(job automation.Job, completed int, status automation.Status, reason string) {
	ctx := context.Background()

	stored, err := s.store.GetJob(ctx, job.ProjectID, job.ID)
	if err != nil {
		s.log.Error("automation: progress not read", "job", job.ID, "err", err)

		return
	}

	if stored == nil {
		return
	}

	now := time.Now()
	stored.Completed = completed
	stored.Status = status
	stored.Error = reason
	stored.UpdatedAt = now

	if status != automation.StatusRunning {
		stored.FinishedAt = now
	}

	if err := s.store.PutJob(ctx, *stored); err != nil {
		s.log.Error("automation: progress not stored", "job", job.ID, "err", err)

		return
	}

	s.bus.Emit("automation.job", stored.Summarize())
}

// resultEvent is what automation.result carries: which job the result belongs
// to, since a result summary alone does not say.
type resultEvent struct {
	JobID  string                   `json:"job_id"`
	Result automation.ResultSummary `json:"result"`
}

func (s *Service) isRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.running[id]

	return ok
}

func (s *Service) deregister(id string) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
}

func (s *Service) validate(in SaveJob) error {
	probe := automation.Tokens(in.placeholder()).ReplaceAllLiteralString(in.URL, "x")

	u, err := url.Parse(strings.TrimSpace(probe))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrInvalidURL
	}

	if in.Proto != "" && !httpmsg.ValidProto(in.Proto) {
		return ErrBadProto
	}

	if _, err := Count(in.Payload); err != nil {
		return &SpecError{Err: err}
	}

	if err := validateStop(in.StopOn); err != nil {
		return &SpecError{Err: err}
	}

	return nil
}

// SpecError is a payload the operator described wrongly - an empty list,
// a backwards range. The endpoint answers 400.
type SpecError struct {
	Err error
}

// Error implements error.
func (e *SpecError) Error() string {
	return "automation: " + e.Err.Error()
}

// Unwrap exposes the underlying reason.
func (e *SpecError) Unwrap() error {
	return e.Err
}

func (in SaveJob) method() string {
	if m := strings.ToUpper(strings.TrimSpace(in.Method)); m != "" {
		return m
	}

	return http.MethodGet
}

func (in SaveJob) proto() string {
	if in.Proto == "" {
		return httpmsg.ProtoHTTP20
	}

	return in.Proto
}

func (in SaveJob) headers() httpmsg.Headers {
	if in.Headers == nil {
		return httpmsg.Headers{}
	}

	return in.Headers
}

func (in SaveJob) placeholder() string {
	if p := strings.TrimSpace(in.Placeholder); p != "" {
		return p
	}

	return "AUTO"
}

func (in SaveJob) stopMatch() string {
	if in.StopMatch == "all" {
		return "all"
	}

	return "any"
}

// stopOps is the comparison each stop-condition field admits. The
// console offers the same lists. A condition outside them would never
// match, so it is refused at save.
var stopOps = map[string][]string{
	"status": {"eq", "ne", "gte", "lte"},
	"header": {"exists", "eq", "contains"},
	"body":   {"contains", "not_contains", "eq"},
}

// validateStop checks the shape of the stop conditions at save.
func validateStop(conds []automation.StopCondition) error {
	for _, c := range conds {
		ops, ok := stopOps[c.Field]
		if !ok {
			return fmt.Errorf("stop condition: unknown field %q", c.Field)
		}

		if !slices.Contains(ops, c.Op) {
			return fmt.Errorf("stop condition: %q cannot be compared with %q", c.Field, c.Op)
		}

		switch c.Field {
		case "status":
			if _, err := strconv.Atoi(strings.TrimSpace(c.Value)); err != nil {
				return fmt.Errorf("stop condition: status value %q is not a number", c.Value)
			}
		case "header":
			if strings.TrimSpace(c.Header) == "" {
				return fmt.Errorf("stop condition: a header condition needs a header name")
			}
		}
	}

	return nil
}

// matchesStop reports whether a result's response trips the job's stop
// conditions. With StopMatch "all" every condition must match, otherwise
// any one does.
func matchesStop(job automation.Job, r automation.Result) bool {
	if len(job.StopOn) == 0 || r.Error != "" {
		return false
	}

	if job.StopMatch == "all" {
		for _, c := range job.StopOn {
			if !evalCond(c, r) {
				return false
			}
		}

		return true
	}

	for _, c := range job.StopOn {
		if evalCond(c, r) {
			return true
		}
	}

	return false
}

func evalCond(c automation.StopCondition, r automation.Result) bool {
	switch c.Field {
	case "status":
		n, err := strconv.Atoi(strings.TrimSpace(c.Value))
		if err != nil {
			return false
		}

		switch c.Op {
		case "eq":
			return r.StatusCode == n
		case "ne":
			return r.StatusCode != n
		case "gte":
			return r.StatusCode >= n
		case "lte":
			return r.StatusCode <= n
		}
	case "header":
		switch c.Op {
		case "exists":
			return headerPresent(r.Headers, c.Header)
		case "eq":
			return r.Headers.Get(c.Header) == c.Value
		case "contains":
			return strings.Contains(r.Headers.Get(c.Header), c.Value)
		}
	case "body":
		body := string(r.Body)

		switch c.Op {
		case "contains":
			return strings.Contains(body, c.Value)
		case "not_contains":
			return !strings.Contains(body, c.Value)
		case "eq":
			return body == c.Value
		}
	}

	return false
}

func headerPresent(h httpmsg.Headers, name string) bool {
	return slices.ContainsFunc(h, func(x httpmsg.Header) bool { return strings.EqualFold(x.Name, name) })
}

func clampConcurrency(n int) int {
	if n <= 0 {
		return 10
	}

	return min(n, MaxConcurrency)
}
