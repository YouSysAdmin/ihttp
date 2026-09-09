package proxy_test

import (
	"bufio"
	"bytes"
	"crypto/sha1" //nolint:gosec // The WebSocket handshake is defined on SHA-1.
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	models "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// wsAccept is the Sec-WebSocket-Accept a server answers a key with.
func wsAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec // Protocol constant.

	return base64.StdEncoding.EncodeToString(sum[:])
}

// The RFC 6455 example: a masked text frame carrying "Hello", and the
// unmasked "hi" a server might answer with.
var (
	wsHello = []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	wsHi    = []byte{0x81, 0x02, 'h', 'i'}
)

// wsUpstream completes a WebSocket handshake by hand, reads one frame
// and answers with wsHi.
func wsUpstream(t *testing.T, tlsOn bool) *httptest.Server {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "not an upgrade", http.StatusBadRequest)

			return
		}

		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)

			return
		}
		defer func() { _ = conn.Close() }()

		_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + wsAccept(r.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n")
		_ = brw.Flush()

		frame := make([]byte, len(wsHello))
		if _, err := io.ReadFull(brw, frame); err != nil {
			t.Errorf("read frame: %v", err)

			return
		}

		if !bytes.Equal(frame, wsHello) {
			t.Errorf("frame %x, want %x", frame, wsHello)
		}

		_, _ = brw.Write(wsHi)
		_ = brw.Flush()
	})

	var srv *httptest.Server
	if tlsOn {
		srv = httptest.NewTLSServer(h)
	} else {
		srv = httptest.NewServer(h)
	}

	t.Cleanup(srv.Close)

	return srv
}

func TestWebSocketUpgradeIsRelayed(t *testing.T) {
	for _, tlsOn := range []bool{false, true} {
		stack := testutil.New(t)
		stack.OpenProject(t, "ws")
		target := wsUpstream(t, tlsOn)

		host := strings.TrimPrefix(strings.TrimPrefix(target.URL, "https://"), "http://")

		var conn net.Conn
		if tlsOn {
			conn = stack.DialTunnel(t, host, true)
		} else {
			var err error

			conn, err = net.Dial("tcp", strings.TrimPrefix(stack.Proxy.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = conn.Close() })
		}

		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

		// Absolute form on the plain path, origin form inside the tunnel.
		path := "/socket"
		if !tlsOn {
			path = target.URL + "/socket"
		}

		_, _ = io.WriteString(conn, "GET "+path+" HTTP/1.1\r\nHost: "+host+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")

		br := bufio.NewReader(conn)

		// A 101 has no body to close: what follows is the socket itself.
		res, err := http.ReadResponse(br, nil) //nolint:bodyclose // See above.
		if err != nil {
			t.Fatalf("tls=%v: %v", tlsOn, err)
		}

		if res.StatusCode != http.StatusSwitchingProtocols || res.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
			t.Fatalf("tls=%v: handshake answered %s %v", tlsOn, res.Status, res.Header)
		}

		if _, err := conn.Write(wsHello); err != nil {
			t.Fatal(err)
		}

		got := make([]byte, len(wsHi))
		if _, err := io.ReadFull(br, got); err != nil {
			t.Fatalf("tls=%v: reading the answer frame: %v", tlsOn, err)
		}

		if !bytes.Equal(got, wsHi) {
			t.Fatalf("tls=%v: frame %x, want %x", tlsOn, got, wsHi)
		}

		e := waitForResponse(t, stack)
		if e.Response.StatusCode != 101 || !e.Response.BodyStreamed || e.Response.Headers.Get("Upgrade") != "websocket" || len(e.Response.Body) != 0 {
			t.Fatalf("tls=%v: logged %+v", tlsOn, e.Response)
		}
	}
}

func TestEventStreamArrivesBeforeItEnds(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "sse")

	release := make(chan struct{})

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: one\n\n")
		_ = http.NewResponseController(w).Flush()

		<-release

		_, _ = io.WriteString(w, "data: two\n\n")
	}))
	t.Cleanup(target.Close)

	res, err := stack.Client().Get(target.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	// The first event must arrive while the upstream is still blocked.
	br := bufio.NewReader(res.Body)
	first := readEvent(t, br)

	if first != "data: one" {
		t.Fatalf("first event %q", first)
	}

	close(release)

	if second := readEvent(t, br); second != "data: two" {
		t.Fatalf("second event %q", second)
	}

	e := waitForResponse(t, stack)
	if !e.Response.BodyStreamed || e.Response.Headers.Get("Content-Type") != "text/event-stream" || len(e.Response.Body) != 0 {
		t.Fatalf("logged %+v", e.Response)
	}
}

// readEvent reads one SSE event, up to its blank line, within 2 seconds.
func readEvent(t *testing.T, br *bufio.Reader) string {
	t.Helper()

	type result struct {
		line string
		err  error
	}

	ch := make(chan result, 1)

	go func() {
		var lines []string

		for {
			line, err := br.ReadString('\n')
			if err != nil {
				ch <- result{err: err}

				return
			}

			line = strings.TrimRight(line, "\n")
			if line == "" {
				ch <- result{line: strings.Join(lines, "\n")}

				return
			}

			lines = append(lines, line)
		}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}

		return r.line
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s: the stream is being buffered")

		return ""
	}
}

// waitForResponse polls the log until its newest entry has a response.
func waitForResponse(t *testing.T, stack *testutil.Stack) models.Entry {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) > 0 && entries[0].StatusCode != 0 {
			e, err := stack.ReqLogs.Get(t.Context(), entries[0].ID)
			if err != nil {
				t.Fatal(err)
			}

			return e
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("the log has no response")

	return models.Entry{}
}
