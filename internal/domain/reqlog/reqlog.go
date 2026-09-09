// Package reqlog is the request log: every exchange the proxy carried
// while a project was open, searchable with the filter language.
package reqlog

import "errors"

// ErrNotFound is an entry the open project does not have.
var ErrNotFound = errors.New("reqlog: entry not found")

// ErrInvalidMarks is a tag, note or color the entry cannot take.
var ErrInvalidMarks = errors.New("reqlog: invalid marks")

// The per-project bucket names under project.BucketData. Messages hold
// the WebSocket messages keyed "<entry id>:<seq>", and the index holds a
// summary per message under the same key so the list never decodes a
// payload.
var (
	BucketLogs         = []byte("request_logs")
	BucketMessages     = []byte("ws_messages")
	BucketMessageIndex = []byte("ws_message_index")
)

// ErrBadRanking is a Top ranking the log cannot compute.
var ErrBadRanking = errors.New("reqlog: unknown ranking")

// ErrMessageNotFound is a message the entry does not have.
var ErrMessageNotFound = errors.New("reqlog: message not found")
