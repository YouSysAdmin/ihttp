package proxy_test

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/wsframe"
	models "github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// echoUpstream completes the handshake and echoes every data message
// unmasked, answers a ping with a pong, and answers a close with a close.
// It records whether the extension offer reached it.
func echoUpstream(t *testing.T, sawExtensions *atomic.Bool) *httptest.Server {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-WebSocket-Extensions") != "" {
			sawExtensions.Store(true)
		}

		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)

			return
		}
		defer func() { _ = conn.Close() }()

		_, _ = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Protocol: echo\r\nSec-WebSocket-Accept: " + wsframe.AcceptKey(r.Header.Get("Sec-WebSocket-Key")) + "\r\n\r\n")
		_ = brw.Flush()

		asm := wsframe.NewAssembler(1 << 30)

		for {
			h, err := wsframe.ReadHeader(brw.Reader)
			if err != nil {
				return
			}

			payload := make([]byte, h.Length)
			if _, err := io.ReadFull(brw.Reader, payload); err != nil {
				return
			}

			if h.Masked {
				wsframe.Unmask(h.Mask, 0, payload)
			}

			switch h.Opcode {
			case wsframe.Close:
				_ = wsframe.WriteFrame(brw, wsframe.Header{Fin: true, Opcode: wsframe.Close}, payload)
				_ = brw.Flush()

				return
			case wsframe.Ping:
				_ = wsframe.WriteFrame(brw, wsframe.Header{Fin: true, Opcode: wsframe.Pong}, payload)
				_ = brw.Flush()
			default:
				if err := asm.Begin(h); err != nil {
					t.Errorf("upstream assembler: %v", err)

					return
				}

				asm.Append(payload, h.Length)

				if h.Fin {
					m := asm.Finish()
					_ = wsframe.WriteFrame(brw, wsframe.Header{Fin: true, Opcode: m.Opcode}, m.Payload)
					_ = brw.Flush()
				}
			}
		}
	}))

	t.Cleanup(srv.Close)

	return srv
}

// wsClient opens a WebSocket through the proxy tunnel to target.
func wsClient(t *testing.T, stack *testutil.Stack, target *httptest.Server) (net.Conn, *bufio.Reader) {
	t.Helper()

	host := strings.TrimPrefix(target.URL, "https://")
	conn := stack.DialTunnel(t, host, true)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	_, _ = io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: "+host+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Extensions: permessage-deflate\r\nSec-WebSocket-Protocol: echo\r\n\r\n")

	br := bufio.NewReader(conn)

	res, err := http.ReadResponse(br, nil) //nolint:bodyclose // A 101 has no body.
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != http.StatusSwitchingProtocols || res.Header.Get("Sec-WebSocket-Protocol") != "echo" {
		t.Fatalf("handshake answered %s %v", res.Status, res.Header)
	}

	return conn, br
}

func send(t *testing.T, conn net.Conn, h wsframe.Header, payload []byte) {
	t.Helper()

	h.Masked = true
	if err := wsframe.WriteFrame(conn, h, payload); err != nil {
		t.Fatal(err)
	}
}

func recv(t *testing.T, br *bufio.Reader) (wsframe.Opcode, []byte) {
	t.Helper()

	h, err := wsframe.ReadHeader(br)
	if err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, h.Length)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatal(err)
	}

	return h.Opcode, payload
}

func TestWebSocketMessagesAreRelayedAndRecorded(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "ws")

	var sawExtensions atomic.Bool
	target := echoUpstream(t, &sawExtensions)
	conn, br := wsClient(t, stack, target)

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Text}, []byte("hi"))
	if op, p := recv(t, br); op != wsframe.Text || string(p) != "hi" {
		t.Fatalf("text echo: %s %q", op, p)
	}

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Binary}, []byte{1, 2, 3})
	if op, p := recv(t, br); op != wsframe.Binary || !bytes.Equal(p, []byte{1, 2, 3}) {
		t.Fatalf("binary echo: %s %x", op, p)
	}

	// Three fragments with a ping in between them.
	send(t, conn, wsframe.Header{Opcode: wsframe.Text}, []byte("Hel"))
	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Ping}, []byte("p"))
	send(t, conn, wsframe.Header{Opcode: wsframe.Continuation}, []byte("l"))
	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Continuation}, []byte("o"))

	if op, p := recv(t, br); op != wsframe.Pong || string(p) != "p" {
		t.Fatalf("pong: %s %q", op, p)
	}

	if op, p := recv(t, br); op != wsframe.Text || string(p) != "Hello" {
		t.Fatalf("fragmented echo: %s %q", op, p)
	}

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Close}, []byte{0x03, 0xe8, 'b', 'y', 'e'})
	if op, p := recv(t, br); op != wsframe.Close || !bytes.Equal(p, []byte{0x03, 0xe8, 'b', 'y', 'e'}) {
		t.Fatalf("close echo: %s %x", op, p)
	}

	_ = conn.Close()

	if sawExtensions.Load() {
		t.Fatal("the extension offer reached the upstream")
	}

	// The connection's end lands in the log asynchronously.
	var entry models.Entry

	deadline := time.Now().Add(3 * time.Second)
	for {
		entries, _, err := stack.ReqLogs.List(t.Context(), reqlogList())
		if err != nil {
			t.Fatal(err)
		}

		if len(entries) == 1 && entries[0].WebSocket && entries[0].Messages == 10 {
			entry, err = stack.ReqLogs.Get(t.Context(), entries[0].ID)
			if err != nil {
				t.Fatal(err)
			}

			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("connection not recorded: %+v", entries)
		}

		time.Sleep(20 * time.Millisecond)
	}

	if entry.Response.StatusCode != 101 || !entry.Response.BodyStreamed || entry.WebSocket.CloseCode != 1000 || entry.WebSocket.CloseReason != "bye" || entry.WebSocket.Subprotocol != "echo" {
		t.Fatalf("entry: %+v %+v", entry.Response, entry.WebSocket)
	}

	msgs, more, err := stack.ReqLogs.Messages(t.Context(), entry.ID, 0, 100)
	if err != nil || more || len(msgs) != 10 {
		t.Fatalf("messages: %d more=%v %v", len(msgs), more, err)
	}

	// The two directions are pumped by two goroutines, so their messages
	// interleave as they happen to. Each direction alone is in order.
	want := map[string][]string{
		"out": {"text hi", "binary", "ping p", "text Hello", "close"},
		"in":  {"text hi", "binary", "pong p", "text Hello", "close"},
	}

	got := map[string][]string{}
	seqs := map[string]int{}

	for i, m := range msgs {
		if m.Seq != i+1 {
			t.Fatalf("message %d has seq %d", i, m.Seq)
		}

		label := m.Opcode
		if m.Preview != "" {
			label += " " + m.Preview
		}

		got[m.Direction] = append(got[m.Direction], label)
		seqs[m.Direction+" "+label] = m.Seq
	}

	for dir, labels := range want {
		if !slices.Equal(got[dir], labels) {
			t.Fatalf("%s messages %q, want %q", dir, got[dir], labels)
		}
	}

	full, err := stack.ReqLogs.Message(t.Context(), entry.ID, seqs["out text Hello"])
	if err != nil || string(full.Payload) != "Hello" || full.Frames != 3 || full.PayloadBinary {
		t.Fatalf("fragmented message: %+v %v", full, err)
	}

	closing, _ := stack.ReqLogs.Message(t.Context(), entry.ID, seqs["out close"])
	if closing.CloseCode != 1000 || closing.CloseReason != "bye" {
		t.Fatalf("close message: %+v", closing)
	}

	// Newest first, in pages: the first page is the last three, the next
	// starts below them.
	newest, more, err := stack.ReqLogs.MessagesBefore(t.Context(), entry.ID, 0, 3)
	if err != nil || !more || len(newest) != 3 || newest[0].Seq != 10 || newest[2].Seq != 8 {
		t.Fatalf("newest page: %+v more=%v %v", newest, more, err)
	}

	older, more, err := stack.ReqLogs.MessagesBefore(t.Context(), entry.ID, 8, 100)
	if err != nil || more || len(older) != 7 || older[0].Seq != 7 || older[6].Seq != 1 {
		t.Fatalf("older page: %d more=%v %v", len(older), more, err)
	}

	// The page after the last message is empty, and a filter finds the entry.
	if rest, _, _ := stack.ReqLogs.Messages(t.Context(), entry.ID, 10, 100); len(rest) != 0 {
		t.Fatalf("messages after the end: %d", len(rest))
	}

	found, _, err := stack.ReqLogs.List(t.Context(), reqlogListSearch("req.websocket = true AND ws.messages = 10 AND ws.closeCode = 1000"))
	if err != nil || len(found) != 1 {
		t.Fatalf("filter over the connection: %v %v", found, err)
	}
}

func TestWebSocketLargeMessageIsRelayedWholeAndStoredCapped(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "ws-large")

	var sawExtensions atomic.Bool
	target := echoUpstream(t, &sawExtensions)
	conn, br := wsClient(t, stack, target)

	// The stack caps bodies at 1 MiB, the message is one byte over.
	big := bytes.Repeat([]byte{0xab}, 1<<20+1)
	big[len(big)-1] = 0xcd

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Binary}, big)

	op, p := recv(t, br)
	if op != wsframe.Binary || !bytes.Equal(p, big) {
		t.Fatalf("large echo: %s, %d bytes", op, len(p))
	}

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Close}, []byte{0x03, 0xe8})
	_, _ = recv(t, br)
	_ = conn.Close()

	deadline := time.Now().Add(3 * time.Second)
	for {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlogList())
		if len(entries) == 1 && entries[0].Messages == 4 {
			m, err := stack.ReqLogs.Message(t.Context(), entries[0].ID, 1)
			if err != nil {
				t.Fatal(err)
			}

			if !m.PayloadTruncated || m.Size != int64(len(big)) || len(m.Payload) != 1<<20 || m.Payload[0] != 0xab {
				t.Fatalf("large message stored: truncated=%v size=%d kept=%d", m.PayloadTruncated, m.Size, len(m.Payload))
			}

			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("connection not recorded: %+v", entries)
		}

		time.Sleep(20 * time.Millisecond)
	}
}
