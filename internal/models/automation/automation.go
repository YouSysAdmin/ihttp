// Package automation is the stored shape of an automation job: a request template
// with a placeholder token, a set of payloads to put in its place, and
// the response each payload drew. It is a developer tool for checking
// how an application under test handles many shapes of input.
package automation

import (
	"regexp"
	"strconv"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// Status is where a job is in its life.
type Status string

// The statuses a job moves through.
const (
	StatusDraft   Status = "draft"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
)

// PayloadKind names how the values to try are produced.
type PayloadKind string

// The payload kinds.
const (
	// PayloadList is lines, one request each, typed into the console or
	// read from a file on disk. A Separator splits a line into columns,
	// which the template reaches as $1, $2 and so on.
	PayloadList PayloadKind = "list"

	// PayloadNumbers is a numeric range, From to To by Step.
	PayloadNumbers PayloadKind = "numbers"

	// PayloadRandom is Count random strings of Length from Charset.
	PayloadRandom PayloadKind = "random"

	// PayloadLibrary is the built-in set of awkward inputs.
	PayloadLibrary PayloadKind = "library"
)

// Payload says which values to try in the placeholder's place. Every kind
// yields rows of values, and only a list can have more than one column.
type Payload struct {
	Kind PayloadKind `json:"kind"`

	// List values. File names a file on the server's disk to read the
	// lines from instead of List. Separator splits each line into
	// columns, or leaves it whole when empty.
	List      []string `json:"list,omitempty"`
	File      string   `json:"file,omitempty"`
	Separator string   `json:"separator,omitempty"`

	// Numbers range.
	From int `json:"from,omitzero"`
	To   int `json:"to,omitzero"`
	Step int `json:"step,omitzero"`

	// Random parameters.
	Count   int    `json:"count,omitzero"`
	Length  int    `json:"length,omitzero"`
	Charset string `json:"charset,omitempty"`
}

// StopCondition ends a run when a response matches it. Field is one of
// status, header or body, and Op is how Value is compared. Header names
// the response header for a header condition.
type StopCondition struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Header string `json:"header,omitempty"`
	Value  string `json:"value,omitempty"`
}

// Job is a request template and the run it drives.
type Job struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `json:"name"`

	// The template. Placeholder is the token replaced in the URL, the
	// header values and the body by the first column of each row. $1, $2
	// and so on name a column outright, see Tokens. URLEncode
	// percent-encodes the payload where it lands in the URL. Off, the
	// bytes are written as they are.
	Method      string          `json:"method"`
	URL         string          `json:"url"`
	Proto       string          `json:"proto"`
	Headers     httpmsg.Headers `json:"headers"`
	Body        httpmsg.Body    `json:"body"`
	Placeholder string          `json:"placeholder"`
	URLEncode   bool            `json:"url_encode"`

	Payload     Payload `json:"payload"`
	Concurrency int     `json:"concurrency"`

	// StopOn ends the run early when a response matches. StopMatch is
	// "any" (stop on the first condition that matches) or "all".
	StopMatch string          `json:"stop_match"`
	StopOn    []StopCondition `json:"stop_on,omitempty"`

	// Run state.
	Status     Status    `json:"status"`
	Total      int       `json:"total"`
	Completed  int       `json:"completed"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`

	// Derived on read, see Annotate. Not stored. Positions is how many
	// tokens the template carries, Columns the highest column any of them
	// names, so a row needs at least that many values.
	BodyBinary bool `json:"body_binary"`
	BodySize   int  `json:"body_size"`
	Positions  int  `json:"positions"`
	Columns    int  `json:"columns"`
}

// Annotate fills the fields derived rather than stored.
func (j *Job) Annotate() {
	j.BodyBinary = httpmsg.IsBinary(j.Headers.Get("Content-Type"), j.Body)
	j.BodySize = len(j.Body)
	j.Positions, j.Columns = j.CountPositions()
}

// Tokens matches every place a value is written into a template: $N for
// column N, and the placeholder for column 1. One pass never re-reads
// what it wrote, so a value that contains a token is left alone. An empty
// placeholder matches only $N.
func Tokens(placeholder string) *regexp.Regexp {
	expr := `\$([1-9][0-9]*)`
	if placeholder != "" {
		expr += "|" + regexp.QuoteMeta(placeholder)
	}

	return regexp.MustCompile(expr)
}

// Column says which column a token names, counting from 1. The
// placeholder is column 1.
func Column(token string) int {
	if len(token) > 1 && token[0] == '$' {
		if n, err := strconv.Atoi(token[1:]); err == nil {
			return n
		}
	}

	return 1
}

// CountPositions reports how many tokens the template carries across the
// URL, the header values and the body, and the highest column they name.
func (j *Job) CountPositions() (positions, columns int) {
	re := Tokens(j.Placeholder)

	parts := []string{j.URL, string(j.Body)}
	for _, h := range j.Headers {
		parts = append(parts, h.Value)
	}

	for _, part := range parts {
		for _, tok := range re.FindAllString(part, -1) {
			positions++
			columns = max(columns, Column(tok))
		}
	}

	return positions, columns
}

// Summary is a Job without its template bodies, for the job list.
type Summary struct {
	ID          string      `json:"id"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	Name        string      `json:"name"`
	Method      string      `json:"method"`
	URL         string      `json:"url"`
	Status      Status      `json:"status"`
	Total       int         `json:"total"`
	Completed   int         `json:"completed"`
	Error       string      `json:"error,omitempty"`
	PayloadKind PayloadKind `json:"payload_kind"`
}

// Summarize projects a Job onto its list row.
func (j Job) Summarize() Summary {
	return Summary{
		ID:          j.ID,
		CreatedAt:   j.CreatedAt,
		UpdatedAt:   j.UpdatedAt,
		Name:        j.Name,
		Method:      j.Method,
		URL:         j.URL,
		Status:      j.Status,
		Total:       j.Total,
		Completed:   j.Completed,
		Error:       j.Error,
		PayloadKind: j.Payload.Kind,
	}
}

// Result is one payload and the response it drew.
type Result struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`
	Index int    `json:"index"`

	Payload string `json:"payload"`

	// The request as sent, with the payload written in.
	ReqMethod  string          `json:"req_method,omitempty"`
	ReqURL     string          `json:"req_url,omitempty"`
	ReqHeaders httpmsg.Headers `json:"req_headers,omitempty"`
	ReqBody    httpmsg.Body    `json:"req_body,omitempty"`

	// Matched is set on the result whose response tripped a stop
	// condition and ended the run.
	Matched bool `json:"matched,omitzero"`

	// Set once the request returns. Error is a transport failure, in
	// which case the response fields are empty.
	StatusCode int    `json:"status_code,omitzero"`
	Status     string `json:"status,omitempty"`
	DurationMS int64  `json:"duration_ms,omitzero"`
	Error      string `json:"error,omitempty"`

	// The response, kept capped for inspection.
	Proto         string          `json:"proto,omitempty"`
	Headers       httpmsg.Headers `json:"headers,omitempty"`
	Body          httpmsg.Body    `json:"body,omitempty"`
	BodyTruncated bool            `json:"body_truncated,omitzero"`

	// Derived on read, see Annotate. Not stored.
	BodyBinary    bool `json:"body_binary"`
	BodySize      int  `json:"body_size"`
	ReqBodyBinary bool `json:"req_body_binary"`
	ReqBodySize   int  `json:"req_body_size"`
}

// Annotate fills the fields derived rather than stored.
func (r *Result) Annotate() {
	r.BodyBinary = httpmsg.IsBinary(r.Headers.Get("Content-Type"), r.Body)
	r.BodySize = len(r.Body)
	r.ReqBodyBinary = httpmsg.IsBinary(r.ReqHeaders.Get("Content-Type"), r.ReqBody)
	r.ReqBodySize = len(r.ReqBody)
}

// ResultSummary is a Result without its response headers and body, for
// the results table.
type ResultSummary struct {
	ID         string `json:"id"`
	Index      int    `json:"index"`
	Payload    string `json:"payload"`
	StatusCode int    `json:"status_code,omitzero"`
	Status     string `json:"status,omitempty"`
	Size       int    `json:"size,omitzero"`
	DurationMS int64  `json:"duration_ms,omitzero"`
	Error      string `json:"error,omitempty"`
	Matched    bool   `json:"matched,omitzero"`
}

// Summarize projects a Result onto its table row.
func (r Result) Summarize() ResultSummary {
	return ResultSummary{
		ID:         r.ID,
		Index:      r.Index,
		Payload:    r.Payload,
		StatusCode: r.StatusCode,
		Status:     r.Status,
		Size:       len(r.Body),
		DurationMS: r.DurationMS,
		Error:      r.Error,
		Matched:    r.Matched,
	}
}
