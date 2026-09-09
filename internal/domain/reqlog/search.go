package reqlog

import (
	"net/url"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// Subject adapts a stored entry to the filter language through the one
// HTTP subject every list shares - see filter.HTTPSubject for the keys.
func Subject(e reqlog.Entry) filter.Subject {
	u, err := url.Parse(e.URL)
	if err != nil {
		u = nil
	}

	req := filter.Request{
		ID:        e.ID,
		Method:    e.Method,
		URL:       u,
		Proto:     e.Proto,
		Header:    e.Headers.Lookup(),
		Body:      e.Body,
		Timestamp: e.CreatedAt,
		Tags:      e.Tags,
		Note:      e.Note,
		Color:     e.Color,
	}

	var res *filter.Response
	if e.Response != nil {
		res = &filter.Response{
			StatusCode: e.Response.StatusCode,
			Status:     e.Response.Status,
			Proto:      e.Response.Proto,
			Header:     e.Response.Headers.Lookup(),
			Trailer:    e.Response.Trailers.Lookup(),
			Body:       e.Response.Body,
			DurationMS: e.Response.DurationMS,
			Streamed:   e.Response.BodyStreamed,
			Timings:    filterTimings(e.Response.Timings),
		}
	}

	var ws *filter.WebSocket
	if e.WebSocket != nil {
		ws = &filter.WebSocket{
			Subprotocol: e.WebSocket.Subprotocol,
			Messages:    e.WebSocket.Messages,
			CloseCode:   e.WebSocket.CloseCode,
		}
	}

	subject := filter.NewHTTPSubject(req, res).WithWebSocket(ws)

	if e.Tunnel != nil {
		subject.Tun = &filter.Tunnel{BytesOut: e.Tunnel.BytesOut, BytesIn: e.Tunnel.BytesIn}
	}

	return subject
}

// filterTimings hands the stored breakdown to the filter in its own
// shape. Nil stays nil, so a query for a phase never matches an entry
// that was never measured.
func filterTimings(t *reqlog.Timings) *filter.Timings {
	if t == nil {
		return nil
	}

	return &filter.Timings{
		Blocked:    t.Blocked,
		DNS:        t.DNS,
		Connect:    t.Connect,
		TLS:        t.TLS,
		Send:       t.Send,
		Wait:       t.Wait,
		Receive:    t.Receive,
		Reused:     t.Reused,
		ServerAddr: t.ServerAddr,
		TLSVersion: t.TLSVersion,
		ALPN:       t.ALPN,
	}
}
