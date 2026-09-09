// Package reqlog is the stored shape of one proxied exchange.
package reqlog

import (
	"slices"
	"time"
	"unicode/utf8"

	"github.com/yousysadmin/ihttp/internal/core/gqlmsg"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// Entry is a request the proxy forwarded and, once it arrived, the
// response. Response is nil while the upstream has not answered.
type Entry struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`

	Method        string          `json:"method"`
	URL           string          `json:"url"`
	Proto         string          `json:"proto"`
	Headers       httpmsg.Headers `json:"headers"`
	Body          httpmsg.Body    `json:"body"`
	BodyTruncated bool            `json:"body_truncated"`

	// BodyBinary says the body is bytes and not text, and BodySize is the
	// byte count, which the coerced string on the wire cannot tell. Both
	// are decided when the entry is read - see Annotate. Not stored.
	BodyBinary bool `json:"body_binary"`
	BodySize   int  `json:"body_size"`

	// Marks the operator put on the entry: tags to find it by, a note,
	// and a color from Colors for the list row.
	Tags  []string `json:"tags,omitempty"`
	Note  string   `json:"note,omitempty"`
	Color string   `json:"color,omitempty"`

	// Saved keeps the entry out of Clear and gives it a place in the
	// saved view, so a log can be emptied and what mattered stays.
	Saved   bool      `json:"saved,omitzero"`
	SavedAt time.Time `json:"saved_at,omitzero"`

	Response *Response `json:"response"`

	// WebSocket is set once an upgraded connection has been relayed: how
	// many messages went by and how it ended. The messages themselves are
	// stored apart, see Message.
	WebSocket *WebSocketInfo `json:"websocket,omitempty"`

	// Tunnel is set on an entry that was relayed WITHOUT being
	// decrypted, because the project said not to. There is no request
	// or response to speak of - only that it happened, to whom, for how
	// long and how much went each way. Present is the whole signal.
	Tunnel *TunnelInfo `json:"tunnel,omitempty"`

	// GraphQL is set when the request body is a GraphQL operation.
	// Derived on read like BodyBinary and BodySize, never stored.
	GraphQL *GraphQL `json:"graphql,omitempty"`

	// BodyRef names the file the request body is kept in, for a body
	// too big to sit in the database. STORAGE ONLY: the store puts the
	// bytes back into Body and clears this on every read, so nothing
	// above the store - the console, the filter, an export - ever sees
	// an entry whose body is elsewhere.
	BodyRef string `json:"body_ref,omitempty"`
}

// WebSocketInfo is what an entry knows about the connection it became.
type WebSocketInfo struct {
	Subprotocol string    `json:"subprotocol,omitempty"`
	Messages    int       `json:"messages"`
	Dropped     int       `json:"dropped,omitzero"`
	ClosedAt    time.Time `json:"closed_at,omitzero"`
	CloseCode   int       `json:"close_code,omitzero"`
	CloseReason string    `json:"close_reason,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Message is one WebSocket message of an entry's connection. Payload is
// capped at MaxBody, Size is the whole length. Direction is "out" for
// what the client sent and "in" for what it received.
type Message struct {
	ID               string       `json:"id"`
	EntryID          string       `json:"entry_id"`
	Seq              int          `json:"seq"`
	Direction        string       `json:"direction"`
	Opcode           string       `json:"opcode"`
	Timestamp        time.Time    `json:"timestamp"`
	Size             int64        `json:"size"`
	Payload          httpmsg.Body `json:"payload"`
	PayloadTruncated bool         `json:"payload_truncated"`
	Frames           int          `json:"frames"`
	Compressed       bool         `json:"compressed,omitzero"`
	CloseCode        int          `json:"close_code,omitzero"`
	CloseReason      string       `json:"close_reason,omitempty"`

	// Derived on read, see Annotate. Not stored.
	PayloadBinary bool `json:"payload_binary"`
	PayloadSize   int  `json:"payload_size"`
}

// Annotate fills the fields derived rather than stored. A text message is
// text whatever its bytes say, a binary one is bytes.
func (m *Message) Annotate() {
	m.PayloadBinary = m.Opcode == "binary" || (m.Opcode != "text" && httpmsg.IsBinary("", m.Payload))
	m.PayloadSize = len(m.Payload)
}

// MessageSummary is a Message without its payload, for the list.
type MessageSummary struct {
	ID          string    `json:"id"`
	EntryID     string    `json:"entry_id"`
	Seq         int       `json:"seq"`
	Direction   string    `json:"direction"`
	Opcode      string    `json:"opcode"`
	Timestamp   time.Time `json:"timestamp"`
	Size        int64     `json:"size"`
	Preview     string    `json:"preview,omitempty"`
	Truncated   bool      `json:"truncated,omitzero"`
	Compressed  bool      `json:"compressed,omitzero"`
	CloseCode   int       `json:"close_code,omitzero"`
	CloseReason string    `json:"close_reason,omitempty"`
}

// PreviewLen bounds the preview of a text message in the list.
const PreviewLen = 200

// Summarize projects a Message onto its list row.
func (m Message) Summarize() MessageSummary {
	s := MessageSummary{
		ID:          m.ID,
		EntryID:     m.EntryID,
		Seq:         m.Seq,
		Direction:   m.Direction,
		Opcode:      m.Opcode,
		Timestamp:   m.Timestamp,
		Size:        m.Size,
		Truncated:   m.PayloadTruncated,
		Compressed:  m.Compressed,
		CloseCode:   m.CloseCode,
		CloseReason: m.CloseReason,
	}

	// A text, ping or pong payload reads as text. A close carries a code
	// the summary already has, and a binary message has nothing to show.
	switch m.Opcode {
	case "text", "ping", "pong":
		if !m.Compressed {
			s.Preview = preview(string(m.Payload))
		}
	}

	return s
}

func preview(s string) string {
	if utf8.RuneCountInString(s) <= PreviewLen {
		return s
	}

	runes := []rune(s)

	return string(runes[:PreviewLen])
}

// Colors is the palette an entry can be marked with.
var Colors = []string{"red", "orange", "yellow", "green", "blue", "purple", "gray"}

// ValidColor reports whether c is empty or one of Colors.
func ValidColor(c string) bool {
	return c == "" || slices.Contains(Colors, c)
}

// Annotate fills the fields that are derived rather than stored.
func (e *Entry) Annotate() {
	e.BodyBinary = httpmsg.IsBinary(e.Headers.Get("Content-Type"), e.Body)
	e.BodySize = len(e.Body)
	e.GraphQL = graphqlOf(e)

	if e.Response != nil {
		e.Response.Annotate()
	}
}

// gqlScanLimit bounds the body a GraphQL scan will read. A query
// document is never this big. Variables can be, and a page of a hundred
// rows must not turn into a hundred megabyte-sized JSON scans. Past it
// the exchange reads as ordinary JSON, which is what it did before.
const gqlScanLimit = 256 << 10

// graphqlOf reads an entry as a GraphQL call: what the request asked
// for, and how many errors came back with the answer. Nil for
// everything that is not one.
func graphqlOf(e *Entry) *GraphQL {
	if len(e.Body) == 0 || len(e.Body) > gqlScanLimit {
		return nil
	}

	op, ok := gqlmsg.Detect(e.Headers.Lookup().Get("Content-Type"), e.Body)
	if !ok {
		return nil
	}

	out := &GraphQL{Type: op.Type, Name: op.Name}
	if op.Batch > 1 {
		out.Batch = op.Batch
	}

	if r := e.Response; r != nil && len(r.Body) > 0 && len(r.Body) <= gqlScanLimit {
		if n, ok := gqlmsg.Errors(r.Headers.Lookup().Get("Content-Type"), r.Body); ok {
			out.Errors = n
		}
	}

	return out
}

// Annotate fills the derived fields of a response.
func (r *Response) Annotate() {
	r.BodyBinary = httpmsg.IsBinary(r.Headers.Get("Content-Type"), r.Body)
	r.BodySize = len(r.Body)
}

// GraphQL is what a GraphQL exchange is doing, derived on read and
// never stored. Present is the signal that this exchange IS GraphQL:
// one POST to one path is all the request line says, and a log of rows
// that all read POST /graphql is no log at all.
type GraphQL struct {
	// Type is query, mutation or subscription.
	Type string `json:"type,omitempty"`

	// Name is the operation name, empty when it is anonymous.
	Name string `json:"name,omitempty"`

	// Batch is how many operations the request carried, set only when
	// more than one came at once.
	Batch int `json:"batch,omitzero"`

	// Errors is how many errors the result carried. A failed GraphQL
	// call is usually a 200, so this is the field that says so.
	Errors int `json:"errors,omitzero"`
}

// Response is the answer half of an Entry. DurationMS is measured from
// the request leaving the proxy to the response body being read.
// BodyStreamed says the body went to the client as it arrived and was
// never captured: a protocol switch or a feed such as an event stream.
// Trailers are read after a buffered body, so a streamed or truncated
// one has none.
type Response struct {
	Proto         string          `json:"proto"`
	StatusCode    int             `json:"status_code"`
	Status        string          `json:"status"`
	Headers       httpmsg.Headers `json:"headers"`
	Trailers      httpmsg.Headers `json:"trailers,omitempty"`
	Body          httpmsg.Body    `json:"body"`
	BodyTruncated bool            `json:"body_truncated"`
	BodyStreamed  bool            `json:"body_streamed"`
	BodyBinary    bool            `json:"body_binary"`
	BodySize      int             `json:"body_size"`
	ReceivedAt    time.Time       `json:"received_at"`
	DurationMS    int64           `json:"duration_ms"`

	// Timings is the phase breakdown, absent on an entry logged before
	// the proxy measured them and on a response a rule answered
	// locally. Absent is not zero: the console has to say "not
	// measured" rather than draw a bar of nothing.
	Timings *Timings `json:"timings,omitempty"`

	// BodyRef names the file this body is kept in. Storage only, see
	// Entry.BodyRef.
	BodyRef string `json:"body_ref,omitempty"`
}

// TunnelInfo is what is known about a connection the proxy relayed
// without looking inside it.
type TunnelInfo struct {
	// BytesOut is client to upstream, BytesIn is upstream to client.
	BytesOut int64 `json:"bytes_out"`
	BytesIn  int64 `json:"bytes_in"`

	ClosedAt time.Time `json:"closed_at,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// Timings is where one exchange spent its time, in milliseconds, as the
// proxy measured it. A phase that did not happen is zero, and zero is
// not the same as fast: on a reused connection - Reused - the three
// connection phases never ran, and Receive is zero for a streamed body
// the proxy never read.
type Timings struct {
	Blocked float64 `json:"blocked_ms"`
	DNS     float64 `json:"dns_ms"`
	Connect float64 `json:"connect_ms"`
	TLS     float64 `json:"tls_ms"`
	Send    float64 `json:"send_ms"`
	Wait    float64 `json:"wait_ms"`
	Receive float64 `json:"receive_ms"`

	Reused     bool   `json:"reused,omitzero"`
	ServerAddr string `json:"server_addr,omitempty"`
	TLSVersion string `json:"tls_version,omitempty"`
	ALPN       string `json:"alpn,omitempty"`
}

// TTFB is the time to the first response byte: everything the exchange
// did before the body started arriving. Derived, never stored, so it
// cannot disagree with the phases it is made of.
func (t Timings) TTFB() float64 {
	return t.Blocked + t.DNS + t.Connect + t.TLS + t.Send + t.Wait
}

// Summary is an Entry without its bodies, which is what a list shows and
// what a table of a thousand rows can afford to send.
type Summary struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	Method     string    `json:"method"`
	URL        string    `json:"url"`
	Proto      string    `json:"proto"`
	StatusCode int       `json:"status_code,omitzero"`
	Status     string    `json:"status,omitempty"`
	DurationMS int64     `json:"duration_ms,omitzero"`
	ResSize    int       `json:"response_size,omitzero"`
	ReqSize    int       `json:"request_size,omitzero"`
	Streamed   bool      `json:"streamed,omitzero"`

	// Tunnel says the row is a connection relayed without decryption,
	// so a reader does not expect a body to open.
	Tunnel bool `json:"tunnel,omitzero"`

	// GraphQL names the operation, when the exchange is one. Without it
	// every row of a GraphQL log reads POST /graphql.
	GraphQL *GraphQL `json:"graphql,omitempty"`

	// Omitted when empty: the summary the response hook emits is built
	// from a partial entry, and the console merges it over the row it
	// has, so an absent member must not wipe a mark.
	Tags  []string `json:"tags,omitempty"`
	Color string   `json:"color,omitempty"`

	// WebSocket says the entry became a connection, Messages how many
	// went by.
	WebSocket bool `json:"websocket,omitzero"`
	Messages  int  `json:"messages,omitzero"`

	Saved bool `json:"saved,omitzero"`
}

// Summarize projects an Entry onto its list row.
func (e Entry) Summarize() Summary {
	s := Summary{
		ID:        e.ID,
		CreatedAt: e.CreatedAt,
		Method:    e.Method,
		URL:       e.URL,
		Proto:     e.Proto,
		ReqSize:   len(e.Body),
		Tags:      e.Tags,
		Color:     e.Color,
		Saved:     e.Saved,
	}

	if e.Response != nil {
		s.StatusCode = e.Response.StatusCode
		s.Status = e.Response.Status
		s.DurationMS = e.Response.DurationMS
		s.ResSize = len(e.Response.Body)
		s.Streamed = e.Response.BodyStreamed
	}

	if e.WebSocket != nil {
		s.WebSocket = true
		s.Messages = e.WebSocket.Messages
	}

	s.GraphQL = graphqlOf(&e)

	if e.Tunnel != nil {
		s.Tunnel = true
		s.ResSize = int(e.Tunnel.BytesIn)
		s.ReqSize = int(e.Tunnel.BytesOut)
	}

	return s
}
