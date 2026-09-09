package automation

import (
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/models/automation"
)

// SaveJob is the body of POST /api/automation/jobs. ID names an existing
// job to rewrite, or is empty to create one. Body null on a rewrite keeps
// the stored bytes, so a binary body the console shows but does not edit
// survives a save. "" clears them.
type SaveJob struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Method      string                     `json:"method"`
	URL         string                     `json:"url"`
	Proto       string                     `json:"proto"`
	Headers     httpmsg.Headers            `json:"headers"`
	Body        *httpmsg.Body              `json:"body"`
	Placeholder string                     `json:"placeholder"`
	URLEncode   bool                       `json:"url_encode"`
	Payload     automation.Payload         `json:"payload"`
	Concurrency int                        `json:"concurrency"`
	StopMatch   string                     `json:"stop_match"`
	StopOn      []automation.StopCondition `json:"stop_on"`
}

// ListResponse is the body of GET /api/automation/jobs.
type ListResponse struct {
	Jobs []automation.Summary `json:"jobs"`
}

// JobResponse wraps one job.
type JobResponse struct {
	Job automation.Job `json:"job"`
}

// ResultsResponse is the body of GET /api/automation/jobs/{id}/results.
type ResultsResponse struct {
	Results []automation.ResultSummary `json:"results"`
}

// ResultResponse wraps one result.
type ResultResponse struct {
	Result automation.Result `json:"result"`
}
