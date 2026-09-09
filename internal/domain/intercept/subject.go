package intercept

import (
	"net/http"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/proxy"
)

// requestSubject adapts a live request to the filter language. The body
// has been buffered by the proxy, so reading it and putting it back is a
// copy, not a network read.
func requestSubject(req *http.Request) filter.Subject {
	body, _, _ := httpmsg.ReadBody(req.Body, 1<<62)
	req.Body = httpmsg.Replace(body)

	return filter.NewHTTPSubject(requestHalf(req, body), nil)
}

// responseSubject adapts a live response, with its request beside it so
// req.* keys resolve too.
func responseSubject(res *http.Response) filter.Subject {
	body, _, _ := httpmsg.ReadBody(res.Body, 1<<62)
	res.Body = httpmsg.Replace(body)

	reqBody, _, _ := httpmsg.ReadBody(res.Request.Body, 1<<62)
	res.Request.Body = httpmsg.Replace(reqBody)

	var (
		duration int64
		timings  *filter.Timings
	)

	if ex := proxy.ExchangeFromContext(res.Request.Context()); ex != nil {
		duration = time.Since(ex.Started).Milliseconds()

		// The phases up to the first byte are measured by now, and receive
		// is not, since the body is what this decision holds up.
		if t := ex.Timings; t != nil {
			timings = &filter.Timings{
				Blocked: t.Blocked, DNS: t.DNS, Connect: t.Connect, TLS: t.TLS,
				Send: t.Send, Wait: t.Wait, Receive: t.Receive,
				Reused: t.Reused, ServerAddr: t.ServerAddr,
				TLSVersion: t.TLSVersion, ALPN: t.ALPN,
			}
		}
	}

	return filter.NewHTTPSubject(requestHalf(res.Request, reqBody), &filter.Response{
		StatusCode: res.StatusCode,
		Status:     httpmsg.StatusReason(res.Status),
		Proto:      res.Proto,
		Header:     res.Header,
		Trailer:    res.Trailer,
		Body:       body,
		DurationMS: duration,
		Timings:    timings,
	})
}

// requestHalf is filter.FromRequest with the exchange's id and start
// time filled in, which is the one thing this side knows and the filter
// package does not.
func requestHalf(req *http.Request, body []byte) filter.Request {
	var (
		id      string
		started time.Time
	)

	if ex := proxy.ExchangeFromContext(req.Context()); ex != nil {
		id = ex.ID
		started = ex.Started
	}

	return filter.FromRequest(req, body, id, started)
}
