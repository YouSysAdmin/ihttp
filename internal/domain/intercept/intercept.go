// Package intercept holds exchanges for a person to review. A request
// the filter matches waits in the queue until the console forwards it -
// edited or not - or drops it, and the same for a response.
//
// Nothing here is stored: a pending item is a goroutine in the proxy
// blocked on a channel, and the queue is the map of those.
package intercept

import "errors"

// The refusals a caller can act on.
var (
	ErrNotFound = errors.New("intercept: item not found")
	ErrGone     = errors.New("intercept: item is no longer waiting")

	// ErrBadForward is a forwarded request the console described that
	// cannot be built.
	ErrBadForward = errors.New("intercept: the forwarded request is not valid")
)
