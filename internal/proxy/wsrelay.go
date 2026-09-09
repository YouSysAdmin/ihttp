package proxy

import (
	"bufio"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/wsframe"
)

// WSDirection says which way a message went.
type WSDirection string

// The two directions. Out is what the client sent, in is what it got.
const (
	WSOut WSDirection = "out"
	WSIn  WSDirection = "in"
)

// WSMessage is one WebSocket message as it passed: a text or binary
// message put back together from its frames, or a control frame. Payload
// holds at most MaxBody bytes, Size the whole length.
type WSMessage struct {
	Seq         int
	Direction   WSDirection
	Opcode      wsframe.Opcode
	At          time.Time
	Size        int64
	Payload     []byte
	Truncated   bool
	Frames      int
	Compressed  bool
	CloseCode   int
	CloseReason string
}

// WebSocketHook is a Hook that also wants the messages of a connection
// the proxy upgraded. The calls come on the relay's own goroutine, so a
// hook must not block: by the time WSMessage runs the bytes have already
// gone to the peer. The returned message is what a holding relay would
// forward, this relay forwards frames as they are and ignores it.
type WebSocketHook interface {
	WSOpened(ex *Exchange, subprotocol string)
	WSMessage(ex *Exchange, m *WSMessage) (*WSMessage, error)
	WSClosed(ex *Exchange, code int, reason string, err error)
}

// wsRelay carries frames both ways between a client and a server,
// reading each header, forwarding it as it was, copying the payload
// through, and telling the hooks about each message.
type wsRelay struct {
	p     *Proxy
	ex    *Exchange
	hooks []WebSocketHook
	limit int64

	seq atomic.Int64

	mu        sync.Mutex
	closeCode int
	closeText string
	closeSeen bool
}

func newWSRelay(p *Proxy, ex *Exchange, limit int64) *wsRelay {
	return &wsRelay{p: p, ex: ex, hooks: p.wsHooks(), limit: limit}
}

// run pumps until either side ends, then closes both. It answers with
// the close code and reason the first close frame carried, and the error
// that ended the relay when it was not a clean end.
func (r *wsRelay) run(client, server io.ReadWriteCloser) (code int, reason string, err error) {
	var (
		wg   sync.WaitGroup
		errs [2]error
	)

	closeBoth := sync.OnceFunc(func() {
		_ = client.Close()
		_ = server.Close()
	})

	wg.Go(func() {
		errs[0] = r.pump(WSOut, client, server)
		closeBoth()
	})
	wg.Go(func() {
		errs[1] = r.pump(WSIn, server, client)
		closeBoth()
	})
	wg.Wait()

	r.mu.Lock()
	code, reason = r.closeCode, r.closeText
	if !r.closeSeen {
		code = wsframe.NoStatus
	}
	r.mu.Unlock()

	for _, e := range errs {
		if e != nil && !isClosed(e) {
			return code, reason, e
		}
	}

	return code, reason, nil
}

// isClosed reports an error that is a connection ending, not a fault.
func isClosed(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, io.ErrUnexpectedEOF)
}

// pump relays one direction. A frame the parser refuses is not the
// proxy's to judge: the direction degrades to a plain copy so the peers
// keep talking, and the error is what run reports.
func (r *wsRelay) pump(dir WSDirection, src io.Reader, dst io.Writer) error {
	br := bufio.NewReaderSize(src, 32<<10)
	// One write to the peer per frame: the header and the payload are
	// gathered here and flushed together.
	bw := bufio.NewWriterSize(dst, 32<<10)
	asm := wsframe.NewAssembler(r.limit)

	for {
		h, err := wsframe.ReadHeader(br)
		if err != nil {
			if errors.Is(err, wsframe.ErrProtocol) {
				r.p.log.Debug("proxy: websocket frame not understood, relaying bytes", "err", err)
				_, _ = io.Copy(dst, br)
			}

			return err
		}

		if _, err := bw.Write(h.Raw); err != nil {
			return err
		}

		keep := &prefixWriter{limit: int(min(r.limit, h.Length))}
		if _, err := io.CopyN(bw, io.TeeReader(br, keep), h.Length); err != nil {
			return err
		}

		if err := bw.Flush(); err != nil {
			return err
		}

		if h.Masked {
			wsframe.Unmask(h.Mask, 0, keep.buf)
		}

		if h.Opcode.IsControl() {
			m := WSMessage{Direction: dir, Opcode: h.Opcode, Size: h.Length, Payload: keep.buf, Frames: 1}
			if h.Opcode == wsframe.Close {
				m.CloseCode, m.CloseReason = wsframe.ParseClose(keep.buf)
				r.noteClose(m.CloseCode, m.CloseReason)
			}

			r.emit(&m)

			continue
		}

		if err := asm.Begin(h); err != nil {
			r.p.log.Debug("proxy: websocket fragments out of order, relaying bytes", "err", err)
			_, _ = io.Copy(dst, br)

			return err
		}

		asm.Append(keep.buf, h.Length)

		if h.Fin {
			m := asm.Finish()
			r.emit(&WSMessage{
				Direction: dir, Opcode: m.Opcode, Size: m.Size, Payload: m.Payload,
				Truncated: m.Truncated, Frames: m.Frames, Compressed: m.Compressed,
			})
		}
	}
}

func (r *wsRelay) noteClose(code int, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.closeSeen {
		r.closeSeen = true
		r.closeCode = code
		r.closeText = reason
	}
}

func (r *wsRelay) emit(m *WSMessage) {
	m.Seq = int(r.seq.Add(1))
	m.At = time.Now()

	for _, h := range r.hooks {
		if _, err := h.WSMessage(r.ex, m); err != nil {
			r.p.log.Debug("proxy: websocket hook", "err", err)
		}
	}
}

// prefixWriter keeps the first limit bytes written to it and lets the rest go by.
type prefixWriter struct {
	limit int
	buf   []byte
}

func (w *prefixWriter) Write(b []byte) (int, error) {
	if room := w.limit - len(w.buf); room > 0 {
		w.buf = append(w.buf, b[:min(room, len(b))]...)
	}

	return len(b), nil
}
