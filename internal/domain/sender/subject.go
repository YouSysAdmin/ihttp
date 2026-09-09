package sender

import (
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	models "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/models/sender"
)

// Subject adapts a sender request to the filter language by viewing it
// as a log entry, which it is shaped like. One vocabulary, two lists.
func Subject(r sender.Request) filter.Subject {
	return reqlog.Subject(models.Entry{
		ID:        r.ID,
		ProjectID: r.ProjectID,
		CreatedAt: r.CreatedAt,
		Method:    r.Method,
		URL:       r.URL,
		Proto:     r.Proto,
		Headers:   r.Headers,
		Body:      r.Body,
		Response:  r.Response,
	})
}
