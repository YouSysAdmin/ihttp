// Package sender is the hand-made request: composed in the console,
// cloned from a log entry, sent with the protocol of choice, and kept
// with the last response it got.
package sender

import "errors"

// The refusals a caller can act on.
var (
	ErrNotFound   = errors.New("sender: request not found")
	ErrInvalidURL = errors.New("sender: url must be absolute with an http or https scheme")
	ErrBadProto   = errors.New("sender: proto must be HTTP/1.0, HTTP/1.1 or HTTP/2.0")
)

// BucketRequests is the per-project bucket name under project.BucketData.
var BucketRequests = []byte("sender_requests")
