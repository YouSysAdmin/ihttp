package proxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/certgen"
)

// handleConnect turns a CONNECT into a served connection: the client is
// told the tunnel is open, and what it sends next is answered by this
// proxy speaking TLS with a certificate for the host it named.
//
// A client that speaks plain HTTP inside the tunnel is served too. The
// first byte decides: 0x16 is a TLS handshake, anything else is not.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "CONNECT is not supported on this listener", http.StatusServiceUnavailable)

		return
	}

	// Decided before anything is written back, so a refusal can still be
	// an HTTP answer rather than a dead tunnel.
	if p.passthroughHost(r.Host) {
		p.tunnel(w, r, hj)

		return
	}

	conn, rw, err := hj.Hijack()
	if err != nil {
		p.log.Error("proxy: hijack failed", "err", err)

		return
	}
	defer func() { _ = conn.Close() }()

	// Written by hand: a 2xx to CONNECT carries no framing headers, and
	// the server's own writer would add Transfer-Encoding and Date.
	if _, err := rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		return
	}

	if err := rw.Flush(); err != nil {
		return
	}

	// The hijacked reader may already hold bytes the client sent after
	// the CONNECT line, so it stays in front of the raw connection.
	client := &bufferedConn{Conn: certgen.WithConnectHost(conn, r.Host), r: rw.Reader}

	first, err := client.r.Peek(1)
	if err != nil {
		return
	}

	served := net.Conn(client)
	scheme := "http"
	opened := time.Now()

	if first[0] == 0x16 {
		tlsConn := tls.Server(client, p.tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			p.log.Debug("proxy: tls handshake failed", "host", r.Host, "err", err)

			return
		}

		served = tlsConn
		scheme = "https"

		state := tlsConn.ConnectionState()
		p.log.Debug("proxy: tunnel opened", "host", r.Host, "client", conn.RemoteAddr().String(),
			"tls", tls.VersionName(state.Version), "sni", state.ServerName, "alpn", state.NegotiatedProtocol)
	} else {
		p.log.Debug("proxy: tunnel opened", "host", r.Host, "client", conn.RemoteAddr().String(), "tls", "none")
	}

	// Requests read off the tunnel arrive in origin form, over HTTP/1 or,
	// when the client negotiated it, HTTP/2, whose :authority lands in
	// req.Host the same way. The tunnel knows the host, so they are made
	// absolute here and handed to the same handler a plain proxied
	// request goes through.
	inner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Host == "" {
			req.URL.Scheme = scheme
			req.URL.Host = req.Host
			if req.URL.Host == "" {
				req.URL.Host = r.Host
			}
		}

		p.ServeHTTP(w, req)
	})

	// No TLSConfig on purpose: the handshake is done. The server reads
	// the TLS state off the conn and serves h2 when it was negotiated.
	srv := &http.Server{
		Handler:   inner,
		ErrorLog:  p.rp.ErrorLog,
		Protocols: p.protocols,
		HTTP2:     &http.HTTP2Config{MaxConcurrentStreams: maxTunnelStreams},
	}
	l := newOneShotListener(served)

	err = srv.Serve(l)
	if err != nil && !errors.Is(err, errListenerDone) {
		p.log.Debug("proxy: tunnel ended", "host", r.Host, "err", err)
	}

	p.log.Debug("proxy: tunnel closed", "host", r.Host, "open_for", time.Since(opened).Round(time.Millisecond))
}

// bufferedConn is a net.Conn whose reads come from a bufio.Reader that
// may already hold bytes.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.r.Read(b)
}

var errListenerDone = errors.New("proxy: tunnel closed")

// oneShotListener hands http.Server exactly one connection, then blocks
// Accept until that connection closes and reports it as the listener
// being done. Serve returns when the client hangs up, and not before.
type oneShotListener struct {
	conn   net.Conn
	closed chan struct{}
	done   func()
	handed atomic.Bool
}

// maxTunnelStreams bounds the h2 streams one tunnel may hold open. Held
// streams each own a buffered body, so this bounds memory per tunnel.
const maxTunnelStreams = 100

func newOneShotListener(conn net.Conn) *oneShotListener {
	l := &oneShotListener{closed: make(chan struct{})}
	l.done = sync.OnceFunc(func() { close(l.closed) })

	// A TLS conn keeps its own type, so http.Server can read the ALPN
	// result off it. Behind a plain net.Conn it would serve HTTP/1 only.
	if tc, ok := conn.(*tls.Conn); ok {
		l.conn = &closeNotifyTLSConn{Conn: tc, onClose: l.done}
	} else {
		l.conn = &closeNotifyConn{Conn: conn, onClose: l.done}
	}

	return l
}

// Accept implements net.Listener.
func (l *oneShotListener) Accept() (net.Conn, error) {
	if l.handed.CompareAndSwap(false, true) {
		return l.conn, nil
	}

	<-l.closed

	return nil, errListenerDone
}

// Close implements net.Listener.
func (l *oneShotListener) Close() error {
	l.done()

	return nil
}

// Addr implements net.Listener.
func (l *oneShotListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

type closeNotifyConn struct {
	net.Conn
	onClose func()
}

func (c *closeNotifyConn) Close() error {
	err := c.Conn.Close()
	c.onClose()

	return err
}

// closeNotifyTLSConn is closeNotifyConn for a *tls.Conn. Embedding the
// concrete type keeps ConnectionState and HandshakeContext visible.
type closeNotifyTLSConn struct {
	*tls.Conn
	onClose func()
}

func (c *closeNotifyTLSConn) Close() error {
	err := c.Conn.Close()
	c.onClose()

	return err
}
