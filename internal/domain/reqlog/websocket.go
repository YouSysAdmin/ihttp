package reqlog

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/internal/proxy"
)

// Bounds on what one connection records.
const (
	// MaxWSMessages is how many messages of one connection are kept.
	// Past it the count goes on and the messages are dropped.
	MaxWSMessages = 10000

	// wsQueue is how many messages may wait for the writer before a
	// connection faster than the disk starts dropping records.
	wsQueue = 4096
)

// wsRecorder writes one connection's messages off the relay's path. The
// relay hands a message over without waiting, one goroutine drains the
// queue into the store, and Close waits for it to finish.
type wsRecorder struct {
	projectID string
	entryID   string
	queue     chan reqlog.Message
	done      chan struct{}

	mu      sync.Mutex
	count   int
	dropped int

	// stored is what drain wrote, set before done closes.
	stored int
}

// wsPending marks an exchange whose response was a 101 that the log took,
// so WSOpened knows which project the connection belongs to.
type wsPending struct {
	projectID string
	at        time.Time
}

// wsPendingFor is how long a logged 101 waits for the relay to open the
// connection before the mark is dropped.
const wsPendingFor = time.Minute

// WSOpened implements proxy.WebSocketHook.
func (s *Service) WSOpened(ex *proxy.Exchange, subprotocol string) {
	v, ok := s.ws.LoadAndDelete(ex.ID)
	if !ok {
		return
	}

	pending, ok := v.(wsPending)
	if !ok {
		return
	}

	rec := &wsRecorder{
		projectID: pending.projectID,
		entryID:   ex.ID,
		queue:     make(chan reqlog.Message, wsQueue),
		done:      make(chan struct{}),
	}

	s.ws.Store(ex.ID, rec)

	go s.drain(rec)
}

// WSMessage implements proxy.WebSocketHook. It never blocks the relay: a
// full queue or a connection over the cap counts a drop instead.
func (s *Service) WSMessage(ex *proxy.Exchange, m *proxy.WSMessage) (*proxy.WSMessage, error) {
	rec := s.recorder(ex.ID)
	if rec == nil {
		return nil, nil
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.count >= MaxWSMessages {
		rec.dropped++

		return nil, nil
	}

	stored := reqlog.Message{
		ID:               ids.New(),
		EntryID:          ex.ID,
		Seq:              m.Seq,
		Direction:        string(m.Direction),
		Opcode:           m.Opcode.String(),
		Timestamp:        m.At,
		Size:             m.Size,
		Payload:          m.Payload,
		PayloadTruncated: m.Truncated,
		Frames:           m.Frames,
		Compressed:       m.Compressed,
		CloseCode:        m.CloseCode,
		CloseReason:      m.CloseReason,
	}

	select {
	case rec.queue <- stored:
		rec.count++
	default:
		rec.dropped++
	}

	return nil, nil
}

// WSClosed implements proxy.WebSocketHook. The queue is drained, the
// entry learns how the connection ended, and the list row is refreshed.
func (s *Service) WSClosed(ex *proxy.Exchange, code int, reason string, err error) {
	v, ok := s.ws.LoadAndDelete(ex.ID)
	if !ok {
		return
	}

	rec, ok := v.(*wsRecorder)
	if !ok {
		return
	}

	close(rec.queue)
	<-rec.done

	rec.mu.Lock()
	info := reqlog.WebSocketInfo{
		Messages:    rec.stored,
		Dropped:     rec.dropped,
		ClosedAt:    time.Now(),
		CloseCode:   code,
		CloseReason: reason,
	}
	rec.mu.Unlock()

	if err != nil {
		info.Error = err.Error()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var updated reqlog.Entry

	uerr := s.store.Update(ctx, rec.projectID, rec.entryID, func(e *reqlog.Entry) error {
		if e.WebSocket != nil && e.WebSocket.Subprotocol != "" {
			info.Subprotocol = e.WebSocket.Subprotocol
		}

		e.WebSocket = &info
		updated = *e

		return nil
	})
	if uerr != nil {
		s.log.Error("reqlog: store websocket close", "id", rec.entryID, "err", uerr)

		return
	}

	s.bus.Emit("reqlog.response", updated.Summarize())
}

// The running count is written onto the entry every wsProgressEvery
// messages, or when wsProgressAfter has passed since the last write, so
// a page opened mid-connection is not far behind on a quiet socket and a
// chatty one does not rewrite the entry per frame.
const (
	wsProgressEvery = 20
	wsProgressAfter = 2 * time.Second
	wsBatch         = 256
)

// drain writes the queue until it closes, and every so often the count:
// after a burst, and on a tick when a quiet socket left a count unwritten.
func (s *Service) drain(rec *wsRecorder) {
	defer close(rec.done)

	stored := 0
	written := 0

	tick := time.NewTicker(wsProgressAfter)
	defer tick.Stop()

	persist := func() {
		if stored == written {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s.setWSCount(ctx, rec, stored)
		cancel()

		written = stored
	}

	defer func() { rec.stored = stored }()

	for {
		select {
		case m, ok := <-rec.queue:
			if !ok {
				return
			}

			// Whatever else is already queued goes in the same write.
			batch := append(make([]reqlog.Message, 0, 64), m)
			for len(batch) < wsBatch {
				select {
				case next, ok := <-rec.queue:
					if !ok {
						break
					}

					batch = append(batch, next)

					continue
				default:
				}

				break
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

			if err := s.store.PutMessages(ctx, rec.projectID, batch); err != nil {
				s.log.Error("reqlog: store websocket messages", "id", rec.entryID, "count", len(batch), "err", err)
			} else {
				stored += len(batch)
				for _, b := range batch {
					s.bus.Emit("reqlog.message", b.Summarize())
				}
			}

			cancel()

			if stored-written >= wsProgressEvery {
				persist()
			}
		case <-tick.C:
			persist()
		}
	}
}

// setWSCount writes the running message count onto the entry and refreshes
// its list row. The close writes the final figures over it.
func (s *Service) setWSCount(ctx context.Context, rec *wsRecorder, count int) {
	var updated reqlog.Entry

	err := s.store.Update(ctx, rec.projectID, rec.entryID, func(e *reqlog.Entry) error {
		if e.WebSocket == nil {
			e.WebSocket = &reqlog.WebSocketInfo{}
		}

		if e.WebSocket.ClosedAt.IsZero() {
			e.WebSocket.Messages = count
		}

		updated = *e

		return nil
	})
	if err != nil {
		return
	}

	s.bus.Emit("reqlog.response", updated.Summarize())
}

func (s *Service) recorder(id string) *wsRecorder {
	v, ok := s.ws.Load(id)
	if !ok {
		return nil
	}

	rec, _ := v.(*wsRecorder)

	return rec
}

// markUpgrade remembers a logged 101 so the relay's WSOpened finds its
// project, and stamps the entry as a connection at once, with the
// subprotocol the server chose, so the console offers its messages
// while it is still open.
func (s *Service) markUpgrade(ctx context.Context, projectID string, ex *proxy.Exchange, res *http.Response) {
	now := time.Now()

	// A 101 the relay never picked up would stay forever, so the stale
	// marks go each time a new one is made.
	s.ws.Range(func(k, v any) bool {
		if p, ok := v.(wsPending); ok && now.Sub(p.at) > wsPendingFor {
			s.ws.Delete(k)
		}

		return true
	})

	s.ws.Store(ex.ID, wsPending{projectID: projectID, at: now})

	sub := res.Header.Get("Sec-WebSocket-Protocol")

	go func() {
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		var updated reqlog.Entry

		err := s.store.Update(wctx, projectID, ex.ID, func(e *reqlog.Entry) error {
			if e.WebSocket == nil {
				e.WebSocket = &reqlog.WebSocketInfo{}
			}

			e.WebSocket.Subprotocol = sub
			updated = *e

			return nil
		})
		if err == nil {
			s.bus.Emit("reqlog.response", updated.Summarize())
		}
	}()
}

// Messages returns a page of an entry's WebSocket messages after seq
// afterSeq. limit is clamped to [1, 1000], zero means 500.
func (s *Service) Messages(ctx context.Context, entryID string, afterSeq, limit int) ([]reqlog.MessageSummary, bool, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, false, project.ErrNoActiveProject
	}

	limit = clampLimit(limit, 500, 1000)

	return s.store.ListMessages(ctx, active.Project.ID, entryID, afterSeq, limit)
}

// MessagesBefore returns a page of an entry's messages newest first,
// below seq beforeSeq, or from the newest when it is 0. limit as Messages.
func (s *Service) MessagesBefore(ctx context.Context, entryID string, beforeSeq, limit int) ([]reqlog.MessageSummary, bool, error) {
	active := s.projects.Active()
	if active == nil {
		return nil, false, project.ErrNoActiveProject
	}

	limit = clampLimit(limit, 500, 1000)

	return s.store.ListMessagesBefore(ctx, active.Project.ID, entryID, beforeSeq, limit)
}

// Message returns one message of an entry, with its payload.
func (s *Service) Message(ctx context.Context, entryID string, seq int) (reqlog.Message, error) {
	active := s.projects.Active()
	if active == nil {
		return reqlog.Message{}, project.ErrNoActiveProject
	}

	m, err := s.store.GetMessage(ctx, active.Project.ID, entryID, seq)
	if err != nil {
		return reqlog.Message{}, err
	}

	if m == nil {
		return reqlog.Message{}, ErrMessageNotFound
	}

	m.Annotate()

	return *m, nil
}

var _ proxy.WebSocketHook = (*Service)(nil)
