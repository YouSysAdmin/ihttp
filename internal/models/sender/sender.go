// Package sender is the stored shape of a request composed by hand.
package sender

import (
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// Request is one hand-made request and the last response it received.
// SourceLogID names the log entry it was cloned from, or "".
type Request struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	SourceLogID string    `json:"source_log_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Method  string          `json:"method"`
	URL     string          `json:"url"`
	Proto   string          `json:"proto"`
	Headers httpmsg.Headers `json:"headers"`
	Body    httpmsg.Body    `json:"body"`

	// BodyBinary and BodySize are derived on read, see Annotate. Not stored.
	BodyBinary bool `json:"body_binary"`
	BodySize   int  `json:"body_size"`

	Response *reqlog.Response `json:"response"`
}

// Annotate fills the fields that are derived rather than stored.
func (r *Request) Annotate() {
	r.BodyBinary = httpmsg.IsBinary(r.Headers.Get("Content-Type"), r.Body)
	r.BodySize = len(r.Body)

	if r.Response != nil {
		r.Response.Annotate()
	}
}

// Summary is a Request without its bodies, for the history list.
type Summary struct {
	ID          string    `json:"id"`
	SourceLogID string    `json:"source_log_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Method      string    `json:"method"`
	URL         string    `json:"url"`
	Proto       string    `json:"proto"`
	StatusCode  int       `json:"status_code,omitzero"`
	Status      string    `json:"status,omitempty"`
	DurationMS  int64     `json:"duration_ms,omitzero"`
}

// Summarize projects a Request onto its list row.
func (r Request) Summarize() Summary {
	s := Summary{
		ID:          r.ID,
		SourceLogID: r.SourceLogID,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		Method:      r.Method,
		URL:         r.URL,
		Proto:       r.Proto,
	}

	if r.Response != nil {
		s.StatusCode = r.Response.StatusCode
		s.Status = r.Response.Status
		s.DurationMS = r.Response.DurationMS
	}

	return s
}
