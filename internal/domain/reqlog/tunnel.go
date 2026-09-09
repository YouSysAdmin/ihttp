package reqlog

import (
	"context"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/proxy"
)

// tunnelWriteTimeout bounds a store write made off the connection's own
// goroutine, whose context is gone by then.
const tunnelWriteTimeout = 10 * time.Second

// TunnelOpened implements proxy.TunnelHook: a connection the project
// asked not to decrypt is written to the log as a CONNECT the moment it
// stands, so a tunnel held open for an hour is visible for that hour
// rather than at the end of it.
//
// The point is that passthrough is never silently blind. What can be
// known is written down - which host, since when - and what cannot is
// absent rather than guessed at. There is no body, because nobody in
// the middle saw one.
func (s *Service) TunnelOpened(t *proxy.Tunnel) {
	active := s.projects.Active()
	if active == nil {
		return
	}

	if active.Project.Settings.RequestLog.Paused {
		s.log.Debug("reqlog: paused, tunnel not logged", "id", t.ID, "host", t.Host)

		return
	}

	// The scope and the ignore filter are decided on a URL, so a tunnel
	// is judged by the one it stands for.
	url := "https://" + t.Host

	if active.Project.Settings.RequestLog.BypassOutOfScope &&
		!active.Scope.Match(url, nil, nil) {
		s.log.Debug("reqlog: tunnel out of scope, not logged", "id", t.ID, "host", t.Host)

		return
	}

	entry := reqlog.Entry{
		ID:        t.ID,
		ProjectID: active.Project.ID,
		CreatedAt: t.Started,
		Method:    "CONNECT",
		URL:       url,
		Proto:     "HTTP/1.1",
		Headers:   httpmsg.Headers{{Name: "Host", Value: t.Host}},

		// No Response yet, exactly as a request whose answer has not
		// arrived has none. An open tunnel reads as pending because it
		// is pending.
		Tunnel: &reqlog.TunnelInfo{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), tunnelWriteTimeout)
	defer cancel()

	if err := s.store.Put(ctx, entry); err != nil {
		s.log.Error("reqlog: store tunnel", "id", t.ID, "err", err)

		return
	}

	// Which project it was written to, for the closing half: the open
	// project may be a different one by the time a long tunnel ends,
	// and the entry belongs where it was written.
	s.tunnels.Store(t.ID, active.Project.ID)

	s.bus.Emit("reqlog.request", entry.Summarize())
	s.maybeTrim(active)
}

// TunnelClosed fills in what only the end of a connection knows: how
// long it stood and how much went each way. The entry is left as it is
// when the opening half did not write one.
func (s *Service) TunnelClosed(t *proxy.Tunnel) {
	id, ok := s.tunnels.LoadAndDelete(t.ID)
	if !ok {
		return
	}

	projectID, _ := id.(string)

	info := reqlog.TunnelInfo{
		BytesOut: t.BytesOut,
		BytesIn:  t.BytesIn,
		ClosedAt: t.Closed,
	}

	if t.Err != nil {
		info.Error = t.Err.Error()
	}

	// A tunnel has no status of its own, so the one the client was told
	// is what is recorded: the 200 the proxy answered the CONNECT with.
	res := reqlog.Response{
		Proto:      "HTTP/1.1",
		StatusCode: 200,
		Status:     "Connection established",
		ReceivedAt: t.Closed,
		DurationMS: t.Closed.Sub(t.Started).Milliseconds(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), tunnelWriteTimeout)
	defer cancel()

	err := s.store.Update(ctx, projectID, t.ID, func(e *reqlog.Entry) error {
		e.Tunnel = &info
		e.Response = &res

		return nil
	})
	if err != nil {
		s.log.Error("reqlog: close tunnel", "id", t.ID, "err", err)

		return
	}

	s.bus.Emit("reqlog.response", reqlog.Entry{
		ID: t.ID, ProjectID: projectID, CreatedAt: t.Started,
		Method: "CONNECT", URL: "https://" + t.Host, Proto: "HTTP/1.1",
		Response: &res, Tunnel: &info,
	}.Summarize())
}

var _ proxy.TunnelHook = (*Service)(nil)
