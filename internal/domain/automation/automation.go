// Package automation runs a request template many times, once per payload, and
// keeps the response each drew. It is a developer tool for exercising an
// application under test with a range of inputs - lists, numeric ranges,
// random strings or a built-in set of awkward values - written into the
// URL, header values and body wherever a placeholder token appears.
package automation

import "errors"

// The refusals a caller can act on.
var (
	ErrNotFound       = errors.New("automation: job not found")
	ErrResultNotFound = errors.New("automation: result not found")
	ErrInvalidURL     = errors.New("automation: url must be absolute with an http or https scheme")
	ErrBadProto       = errors.New("automation: proto must be HTTP/1.0, HTTP/1.1 or HTTP/2.0")
	ErrNoPlaceholder  = errors.New("automation: the template has no placeholder to fill")
	ErrAlreadyRunning = errors.New("automation: the job is already running")
	ErrNotRunning     = errors.New("automation: the job is not running")
)

// Bucket names, per project, under project.BucketData. The index holds a
// ResultSummary per result under the same key as the result itself.
var (
	BucketJobs        = []byte("automation_jobs")
	BucketResults     = []byte("automation_results")
	BucketResultIndex = []byte("automation_result_index")
)
