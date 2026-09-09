package sender

import (
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/models/sender"
)

// SaveRequest is the body of POST /api/sender/requests. ID names an
// existing request to rewrite, or is empty to create one. Body null on a
// rewrite keeps the stored bytes, so a binary body the console shows but
// does not edit survives a save. "" clears them.
type SaveRequest struct {
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	URL     string          `json:"url"`
	Proto   string          `json:"proto"`
	Headers httpmsg.Headers `json:"headers"`
	Body    *httpmsg.Body   `json:"body"`
}

// ListResponse is the body of GET /api/sender/requests.
type ListResponse struct {
	Requests []sender.Summary `json:"requests"`
}

// RequestResponse wraps one request.
type RequestResponse struct {
	Request sender.Request `json:"request"`
}
