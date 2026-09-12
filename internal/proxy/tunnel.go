package proxy

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/ids"
)

// Tunnel is one connection the proxy relayed WITHOUT decrypting it,
// because the open project said not to.
type Tunnel struct {
	// ID is minted like an exchange's, so a tunnel sorts among them.
	ID string

	// Host is the CONNECT target, host and port as the client named it.
	Host string

	Started time.Time
	Closed  time.Time

	// BytesOut is client to upstream, BytesIn is upstream to client.
	BytesOut int64
	BytesIn  int64

	// Err is what ended it, when something other than either side
	// closing did.
	Err error
}

// TunnelHook is a Hook that also wants the connections the proxy did not
// decrypt, so a passthrough host still leaves a trace in the log.
//
// TunnelOpened runs as soon as the tunnel stands and TunnelClosed when
// it is over: a tunnel can be held open for minutes, and a log told only
// at the end would be blank for all of them. On the second call the byte
// counts, Closed and Err are set and nothing else has changed.
type TunnelHook interface {
	TunnelOpened(t *Tunnel)
	TunnelClosed(t *Tunnel)
}

// relayTunnel copies bytes both ways without looking at them and
// reports what passed. It returns when either side closes.
//
// The Tunnel is written once, after both directions are done, so
// nothing reads a half-updated one.
func relayTunnel(client, upstream net.Conn, t *Tunnel) {
	var (
		wg            sync.WaitGroup
		once          sync.Once
		out, in       int64
		errOut, errIn error
	)

	// Closing both ends is what unblocks the other direction, and it is
	// also why that direction then reports a closed connection.
	stop := func() {
		once.Do(func() {
			_ = client.Close()
			_ = upstream.Close()
		})
	}

	wg.Go(func() {
		defer stop()

		out, errOut = io.Copy(upstream, client)
	})

	wg.Go(func() {
		defer stop()

		in, errIn = io.Copy(client, upstream)
	})

	wg.Wait()

	t.BytesOut, t.BytesIn = out, in
	t.Closed = time.Now()

	// A tunnel ENDS by one side closing, which is not a failure. The
	// direction still copying then reports a closed connection because
	// stop closed it, so that is not recorded as the reason either.
	for _, err := range []error{errOut, errIn} {
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) {
			t.Err = err

			break
		}
	}
}

// passthroughHost decides whether the CONNECT target is one the project
// asked not to decrypt. The port is stripped, since a list names hosts.
func (p *Proxy) passthroughHost(target string) bool {
	if p.passthrough == nil {
		return false
	}

	host := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}

	return p.passthrough(host)
}

// tunnelDialTimeout bounds reaching the upstream of a tunnel. The client
// is already waiting on the CONNECT by this point.
const tunnelDialTimeout = 30 * time.Second

// dialTunnelUpstream reaches the CONNECT target, through the upstream
// proxy when one is configured - a host we do not decrypt still has to
// leave the machine the way everything else does, or passthrough would
// quietly stop working on the networks that need an upstream proxy at
// all.
func (p *Proxy) dialTunnelUpstream(r *http.Request) (net.Conn, error) {
	d := &net.Dialer{Timeout: tunnelDialTimeout}

	via, err := p.tunnelProxyFor(r)
	if err != nil {
		return nil, err
	}

	if via == nil {
		// Overridden here too: a host we do not decrypt still has to be
		// reachable where the operator said it lives, or an override
		// would work everywhere except on the hosts left alone.
		return d.DialContext(r.Context(), "tcp", p.dialAddr(r.Host))
	}

	// A SOCKS upstream would need its own handshake. Rather than
	// silently going direct - which leaks past the proxy the operator
	// configured - or silently decrypting, which is the one thing they
	// asked not to happen, this is refused and says why.
	if scheme := strings.ToLower(via.Scheme); scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("not decrypting %s needs a tunnel through the upstream proxy, which %s cannot do - use an http or https upstream proxy, or remove the host from the do-not-decrypt list",
			r.Host, via.Scheme)
	}

	addr := via.Host
	if via.Port() == "" {
		addr = net.JoinHostPort(via.Hostname(), map[string]string{"http": "80", "https": "443"}[strings.ToLower(via.Scheme)])
	}

	conn, err := d.DialContext(r.Context(), "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("upstream proxy %s: %w", addr, err)
	}

	if strings.EqualFold(via.Scheme, "https") {
		// The hop to the proxy itself is TLS, and its certificate is
		// checked against its own name like any other.
		tlsConn := tls.Client(conn, &tls.Config{ServerName: via.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(r.Context()); err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("upstream proxy %s: %w", addr, err)
		}

		conn = tlsConn
	}

	if err := connectThrough(conn, r.Host, via); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return conn, nil
}

// tunnelProxyFor asks the configured proxy function about this target,
// with a request shaped the way an outbound one would be so a bypass
// list decides the same way it does for everything else.
func (p *Proxy) tunnelProxyFor(r *http.Request) (*url.URL, error) {
	if p.upstreamProxy == nil {
		return nil, nil
	}

	host := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		host = h
	}

	probe := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Scheme: "https", Host: host},
		Header: http.Header{},
	}

	return p.upstreamProxy(probe.WithContext(r.Context()))
}

// connectThrough asks an upstream HTTP proxy to open a tunnel to target.
func connectThrough(conn net.Conn, target string, via *url.URL) error {
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: http.Header{},
	}

	if via.User != nil {
		password, _ := via.User.Password()
		req.Header.Set("Proxy-Authorization",
			"Basic "+base64.StdEncoding.EncodeToString([]byte(via.User.Username()+":"+password)))
	}

	if err := req.Write(conn); err != nil {
		return fmt.Errorf("upstream proxy: write CONNECT: %w", err)
	}

	// The body of a 2xx answer IS the tunnel, so it is never closed -
	// see the check below.
	res, err := http.ReadResponse(bufio.NewReader(conn), req) //nolint:bodyclose // See above.
	if err != nil {
		return fmt.Errorf("upstream proxy: read CONNECT answer: %w", err)
	}

	// The body of a 2xx CONNECT answer is the tunnel, so it is never
	// read or closed here.
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream proxy refused the tunnel to %s: %s", target, res.Status)
	}

	return nil
}

// tunnel relays a CONNECT the project asked not to decrypt. The
// upstream is reached FIRST, so a failure is a 502 the client can read
// rather than a tunnel that opens and dies.
func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request, hj http.Hijacker) {
	up, err := p.dialTunnelUpstream(r)
	if err != nil {
		p.log.Debug("proxy: tunnel upstream failed", "host", r.Host, "err", err)
		http.Error(w, "ihttp could not open a tunnel to "+r.Host+": "+err.Error(), http.StatusBadGateway)

		return
	}

	conn, rw, err := hj.Hijack()
	if err != nil {
		_ = up.Close()
		p.log.Error("proxy: hijack failed", "err", err)

		return
	}

	// By hand, for the same reason as the decrypting path: a 2xx to
	// CONNECT carries no framing headers.
	if _, err := rw.WriteString("HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
		_ = conn.Close()
		_ = up.Close()

		return
	}

	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		_ = up.Close()

		return
	}

	t := &Tunnel{ID: ids.New(), Host: r.Host, Started: time.Now()}
	hooks := p.tunnelHooks()

	p.log.Debug("proxy: tunnel opened without decrypting", "id", t.ID, "host", t.Host,
		"client", conn.RemoteAddr().String())

	for _, h := range hooks {
		h.TunnelOpened(t)
	}

	// Whatever the client already sent after the CONNECT line goes first.
	relayTunnel(&bufferedConn{Conn: conn, r: rw.Reader}, up, t)

	p.log.Debug("proxy: tunnel closed", "id", t.ID, "host", t.Host,
		"open_for", t.Closed.Sub(t.Started).Round(time.Millisecond),
		"bytes_out", t.BytesOut, "bytes_in", t.BytesIn, "err", t.Err)

	for _, h := range hooks {
		h.TunnelClosed(t)
	}
}

// tunnelHooks are the hooks that also want undecrypted connections,
// picked out of the registered ones the way wsHooks does.
func (p *Proxy) tunnelHooks() []TunnelHook {
	var out []TunnelHook

	for _, h := range p.hooks {
		if th, ok := h.(TunnelHook); ok {
			out = append(out, th)
		}
	}

	return out
}
