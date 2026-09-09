package proxy

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

// Timings is where one exchange spent its time, measured with
// net/http/httptrace around the upstream round trip. Every phase is
// milliseconds.
//
// A phase that did not happen is zero, and zero is not the same as
// fast: on a reused connection DNS, Connect and TLS never run at all,
// which is what Reused says. On an HTTP/2 connection the same is true
// of every stream after the first - the connection phases belong to the
// connection, not to this request. Receive is zero for a streamed
// response, whose body the proxy never reads.
//
// A response a hook answered locally - a mock, a local file, a CORS
// preflight - never reaches the transport and carries no timings at all.
type Timings struct {
	// Blocked is the wait for a connection from the pool before any of
	// the phases below start.
	Blocked float64 `json:"blocked_ms"`

	// DNS, Connect and TLS are the cost of a new connection.
	DNS     float64 `json:"dns_ms"`
	Connect float64 `json:"connect_ms"`
	TLS     float64 `json:"tls_ms"`

	// Send is writing the request, Wait is the server thinking about it,
	// Receive is reading the body back.
	Send    float64 `json:"send_ms"`
	Wait    float64 `json:"wait_ms"`
	Receive float64 `json:"receive_ms"`

	// Reused says the connection was already open, so the three
	// connection phases are absent rather than instant.
	Reused bool `json:"reused,omitzero"`

	// ServerAddr is the address the upstream was actually reached at,
	// which is the answer to "which of the DNS answers did it use".
	ServerAddr string `json:"server_addr,omitempty"`

	// TLSVersion and ALPN are what was negotiated with the upstream,
	// facts the log had nowhere to put before. Both come from the
	// handshake, so a request on a reused connection carries neither -
	// absent rather than assumed from the connection it shares.
	TLSVersion string `json:"tls_version,omitempty"`
	ALPN       string `json:"alpn,omitempty"`
}

// tracer collects the phases as the transport reports them. The
// callbacks come from the transport's own goroutines, so every field is
// written under the lock.
type tracer struct {
	mu sync.Mutex

	t Timings

	// The clock each phase started on, zero when it has not started.
	getConn, dns, connect, tls, gotConn, wroteRequest, firstByte time.Time
}

// trace returns the ClientTrace to hang on a request's context.
func (c *tracer) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn: func(string) {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.getConn = time.Now()
		},

		DNSStart: func(httptrace.DNSStartInfo) {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.dns = time.Now()
			c.blockedUntil(c.dns)
		},

		DNSDone: func(httptrace.DNSDoneInfo) {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.t.DNS = since(c.dns)
		},

		// Fires once per address tried, so the last attempt - the one
		// that connected - is the one kept.
		ConnectStart: func(string, string) {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.connect = time.Now()
			c.blockedUntil(c.connect)
		},

		ConnectDone: func(_, _ string, err error) {
			c.mu.Lock()
			defer c.mu.Unlock()

			if err == nil {
				c.t.Connect = since(c.connect)
			}
		},

		TLSHandshakeStart: func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.tls = time.Now()
		},

		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			c.mu.Lock()
			defer c.mu.Unlock()

			if err != nil {
				return
			}

			c.t.TLS = since(c.tls)
			c.t.TLSVersion = tls.VersionName(state.Version)
			c.t.ALPN = state.NegotiatedProtocol
		},

		GotConn: func(info httptrace.GotConnInfo) {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.gotConn = time.Now()
			c.t.Reused = info.Reused
			c.blockedUntil(c.gotConn)

			if info.Conn != nil && info.Conn.RemoteAddr() != nil {
				c.t.ServerAddr = info.Conn.RemoteAddr().String()
			}
		},

		WroteRequest: func(httptrace.WroteRequestInfo) {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.wroteRequest = time.Now()

			if !c.gotConn.IsZero() {
				c.t.Send = ms(c.wroteRequest.Sub(c.gotConn))
			}
		},

		GotFirstResponseByte: func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			c.firstByte = time.Now()

			// The wait is the server's own: from the request being on
			// the wire to the first byte coming back. Without a
			// WroteRequest to measure from - an h2 stream can report
			// them out of order - the connection is the next best mark.
			from := c.wroteRequest
			if from.IsZero() {
				from = c.gotConn
			}

			if !from.IsZero() {
				c.t.Wait = ms(c.firstByte.Sub(from))
			}
		},
	}
}

// blockedUntil records the pool wait once, the first time a phase
// actually starts after GetConn.
func (c *tracer) blockedUntil(at time.Time) {
	if c.getConn.IsZero() || c.t.Blocked != 0 {
		return
	}

	c.t.Blocked = ms(at.Sub(c.getConn))
}

// bodyRead closes the receive phase, called when the body has been read.
// A streamed body is never read, so this never runs for one and Receive
// stays zero.
func (c *tracer) bodyRead() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.firstByte.IsZero() {
		return
	}

	c.t.Receive = since(c.firstByte)
}

// timings is a copy of what has been collected so far.
func (c *tracer) timings() Timings {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

// ms is a duration in milliseconds, to the microsecond, so a local
// server's sub-millisecond phase is not rounded away to zero.
func ms(d time.Duration) float64 {
	if d < 0 {
		return 0
	}

	return float64(d.Round(time.Microsecond)) / float64(time.Millisecond)
}

func since(start time.Time) float64 {
	if start.IsZero() {
		return 0
	}

	return ms(time.Since(start))
}
