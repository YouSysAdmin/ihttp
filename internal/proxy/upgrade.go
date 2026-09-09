package proxy

import (
	"errors"
	"io"
	"maps"
	"net/http"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// hopByHop are the headers that belong to one connection and never
// travel to the next. The same list net/http/httputil keeps.
var hopByHop = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// isWebSocketUpgrade reports a request asking to become a WebSocket.
func isWebSocketUpgrade(r *http.Request) bool {
	return httpmsg.IsWebSocketUpgrade(r.Header)
}

// handleUpgrade carries a WebSocket handshake to the upstream through the
// hooks, and when it is accepted, owns both connections and relays the
// frames between them with a look at each. The reverse proxy could relay
// the bytes, but not tell one frame from the next.
func (p *Proxy) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	outreq := r.Clone(r.Context())
	outreq.RequestURI = ""
	outreq.Close = false

	for _, h := range hopByHop {
		outreq.Header.Del(h)
	}

	outreq.Header.Set("Connection", "Upgrade")
	outreq.Header.Set("Upgrade", "websocket")
	outreq.Header.Del("X-Forwarded-For")
	outreq.Header.Del("X-Forwarded-Host")
	outreq.Header.Del("X-Forwarded-Proto")

	// An extension is an offer. Not passing it on means the server does
	// not negotiate it, and the frames stay readable.
	if !p.keepWSExtensions {
		outreq.Header.Del("Sec-WebSocket-Extensions")
	}

	res, err := p.rp.Transport.RoundTrip(outreq)
	if err != nil {
		p.errorHandler(w, r, err)

		return
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		// Refused, or answered by a rule. A plain response goes back.
		defer func() { _ = res.Body.Close() }()

		maps.Copy(w.Header(), res.Header)
		for _, h := range hopByHop {
			w.Header().Del(h)
		}

		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)

		return
	}

	back, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		_ = res.Body.Close()
		p.errorHandler(w, r, errors.New("proxy: upgrade answered with a body that cannot be written"))

		return
	}

	conn, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = back.Close()
		p.errorHandler(w, r, err)

		return
	}

	res.Body = nil

	if err := res.Write(brw); err != nil {
		_ = back.Close()
		_ = conn.Close()

		return
	}

	if err := brw.Flush(); err != nil {
		_ = back.Close()
		_ = conn.Close()

		return
	}

	// The client may have sent its first frame right behind the
	// handshake, and it sits in the reader the hijack handed back.
	client := &bufferedConn{Conn: conn, r: brw.Reader}

	// The transport attached the exchange to the request it sent, which
	// is the one the response points back at.
	ex := ExchangeFromContext(res.Request.Context())
	subprotocol := res.Header.Get("Sec-WebSocket-Protocol")

	relay := newWSRelay(p, ex, p.maxBody)

	p.log.Debug("proxy: websocket opened", "id", ex.ID, "url", r.URL.String(), "subprotocol", subprotocol)

	for _, h := range relay.hooks {
		h.WSOpened(ex, subprotocol)
	}

	code, reason, err := relay.run(client, back)

	for _, h := range relay.hooks {
		h.WSClosed(ex, code, reason, err)
	}

	p.log.Debug("proxy: websocket closed", "id", ex.ID, "url", r.URL.String(), "code", code, "reason", reason,
		"open_for", time.Since(ex.Started).Round(time.Millisecond), "err", err)
}

// wsHooks picks the hooks that want messages.
func (p *Proxy) wsHooks() []WebSocketHook {
	var out []WebSocketHook

	for _, h := range p.hooks {
		if wh, ok := h.(WebSocketHook); ok {
			out = append(out, wh)
		}
	}

	return out
}
