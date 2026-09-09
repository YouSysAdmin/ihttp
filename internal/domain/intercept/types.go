package intercept

import (
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// Kind says which half of an exchange is waiting.
type Kind string

// The two kinds.
const (
	KindRequest  Kind = "request"
	KindResponse Kind = "response"
)

// Item is one waiting exchange as the console sees it. Request is always
// set - for a waiting response it is the request that produced it - and
// Response only for KindResponse.
type Item struct {
	ID         string    `json:"id"`
	Kind       Kind      `json:"kind"`
	ReceivedAt time.Time `json:"received_at"`
	Request    *Request  `json:"request"`
	Response   *Response `json:"response,omitzero"`
}

// Request is the request half of an Item.
type Request struct {
	Method     string          `json:"method"`
	URL        string          `json:"url"`
	Proto      string          `json:"proto"`
	Headers    httpmsg.Headers `json:"headers"`
	Body       httpmsg.Body    `json:"body"`
	BodyBinary bool            `json:"body_binary"`
	BodySize   int             `json:"body_size"`
}

// Response is the response half of an Item.
type Response struct {
	Proto      string          `json:"proto"`
	StatusCode int             `json:"status_code"`
	Status     string          `json:"status"`
	Headers    httpmsg.Headers `json:"headers"`
	Trailers   httpmsg.Headers `json:"trailers,omitempty"`
	Body       httpmsg.Body    `json:"body"`
	BodyBinary bool            `json:"body_binary"`
	BodySize   int             `json:"body_size"`
}

// ForwardRequest is the body of POST /api/intercept/requests/{id}/forward.
// Every field is optional and an absent one keeps the original. Body is
// kept when null or absent and REPLACED when "" - a binary body the
// console cannot edit is sent as null so it goes through untouched.
type ForwardRequest struct {
	Method  string          `json:"method"`
	URL     string          `json:"url"`
	Headers httpmsg.Headers `json:"headers"`
	Body    httpmsg.Body    `json:"body"`

	// InterceptResponse overrides the project setting for this one
	// exchange: true holds the response, false lets it through.
	InterceptResponse *bool `json:"intercept_response"`
}

// ForwardResponse is the body of POST /api/intercept/responses/{id}/forward.
type ForwardResponse struct {
	StatusCode int             `json:"status_code"`
	Status     string          `json:"status"`
	Headers    httpmsg.Headers `json:"headers"`
	Body       httpmsg.Body    `json:"body"`
}

// ListResponse is the body of GET /api/intercept/items.
type ListResponse struct {
	Items []Item `json:"items"`
}

// ItemResponse wraps one item.
type ItemResponse struct {
	Item Item `json:"item"`
}
