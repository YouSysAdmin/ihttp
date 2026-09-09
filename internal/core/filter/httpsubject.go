package filter

import (
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/gqlmsg"
	"github.com/yousysadmin/ihttp/internal/core/grpcmsg"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// Request is the request half an HTTPSubject is built from. Every list
// in the product - the log, the sender history, the intercept queue -
// projects onto this, so one set of field names serves all three.
type Request struct {
	ID        string
	Method    string
	URL       *url.URL
	Proto     string
	Header    http.Header
	Body      []byte
	Timestamp time.Time

	// The marks a logged entry carries. Empty on a live request.
	Tags  []string
	Note  string
	Color string
}

// Response is the response half, nil when there is none yet. Streamed
// says the body went to the client unseen.
type Response struct {
	StatusCode int
	Status     string
	Proto      string
	Header     http.Header
	Trailer    http.Header
	Body       []byte
	DurationMS int64
	Streamed   bool

	// Timings is the phase breakdown, nil when the exchange was not
	// measured - an old entry, or a response a rule answered locally.
	// Every res.* timing key then answers "" rather than zero, so a
	// query for a slow phase never matches what was never timed.
	Timings *Timings
}

// Timings is the phase breakdown a res.* key reads, in milliseconds.
type Timings struct {
	Blocked    float64
	DNS        float64
	Connect    float64
	TLS        float64
	Send       float64
	Wait       float64
	Receive    float64
	Reused     bool
	ServerAddr string
	TLSVersion string
	ALPN       string
}

// Tunnel is what a logged entry knows about a connection that was
// relayed WITHOUT being decrypted, nil for everything else.
type Tunnel struct {
	BytesOut int64
	BytesIn  int64
}

// WebSocket is what a logged entry knows about the connection it became,
// nil for an entry that never upgraded.
type WebSocket struct {
	Subprotocol string
	Messages    int
	CloseCode   int
}

// HTTPSubject is the one Subject over an HTTP exchange.
//
// Static keys: req.id, req.method, req.url, req.scheme, req.host,
// req.port, req.path, req.query, req.ext, req.proto, req.body, req.size,
// req.timestamp, req.headers, req.tag (a list, req.tags is the same),
// req.note, req.color, req.websocket, res.statusCode, res.statusReason,
// res.proto, res.body, res.size, res.duration, res.type, res.mime,
// res.headers, res.streamed, ws.messages, ws.subprotocol, ws.closeCode,
// req.grpcService, req.grpcMethod, res.grpcStatus, req.gqlType,
// req.gqlName, res.gqlErrors, req.tunnel,
// tunnel.bytesOut, tunnel.bytesIn, and from the phase
// breakdown res.ttfb, res.blocked, res.dns, res.connect, res.tls,
// res.send, res.wait, res.receive, res.reused, res.serverIP,
// res.tlsVersion, res.alpn.
//
// Dynamic keys: req.header.<name>, res.header.<name>, res.trailer.<name>,
// req.cookie.<name>, res.cookie.<name> (Set-Cookie), req.query.<name>,
// req.form.<name> (a urlencoded body). Absent answers "" and `exists`
// says so.
type HTTPSubject struct {
	Req Request
	Res *Response
	WS  *WebSocket

	// Tun is set on a connection the proxy did not decrypt.
	Tun *Tunnel
}

// FromRequest builds the request half from a live http.Request whose
// body has already been read out. The id and the start time come from
// the caller, which knows the exchange - this package does not, and
// must not, know about the proxy.
//
// One place, so the proxy-side callers - the rules hook and the
// intercept hook - cannot drift about which fields a live request
// contributes.
func FromRequest(req *http.Request, body []byte, id string, startedAt time.Time) Request {
	return Request{
		ID:        id,
		Method:    req.Method,
		URL:       req.URL,
		Proto:     req.Proto,
		Header:    req.Header,
		Body:      body,
		Timestamp: startedAt,
	}
}

// NewHTTPSubject builds a subject. A nil res is a request alone, and
// every res.* key then answers "" rather than failing, so one query runs
// over a log where half the rows are still waiting.
func NewHTTPSubject(req Request, res *Response) HTTPSubject {
	return HTTPSubject{Req: req, Res: res}
}

// WithWebSocket adds what the log knows about the connection. The ws.*
// keys answer "" without it.
func (s HTTPSubject) WithWebSocket(ws *WebSocket) HTTPSubject {
	s.WS = ws

	return s
}

// Field implements Subject.
func (s HTTPSubject) Field(key string) (string, bool) {
	if v, ok := s.dynamic(key); ok {
		return v, true
	}

	switch key {
	case "req.id":
		return s.Req.ID, true
	case "req.method":
		return s.Req.Method, true
	case "req.url":
		return s.urlString(), true
	case "req.scheme":
		return s.urlPart(func(u *url.URL) string { return u.Scheme }), true
	case "req.host":
		return s.urlPart(func(u *url.URL) string { return u.Hostname() }), true
	case "req.port":
		return s.urlPart(func(u *url.URL) string { return u.Port() }), true
	case "req.path":
		return s.urlPart(func(u *url.URL) string { return u.Path }), true
	case "req.query":
		return s.urlPart(func(u *url.URL) string { return u.RawQuery }), true
	case "req.ext":
		return strings.TrimPrefix(path.Ext(s.urlPart(func(u *url.URL) string { return u.Path })), "."), true
	case "req.proto":
		return s.Req.Proto, true
	case "req.body":
		return string(s.Req.Body), true
	case "req.size":
		return strconv.Itoa(len(s.Req.Body)), true
	case "req.timestamp":
		if s.Req.Timestamp.IsZero() {
			return "", true
		}

		return s.Req.Timestamp.Format(time.RFC3339), true
	case "req.note":
		return s.Req.Note, true
	case "req.color":
		return s.Req.Color, true
	case "req.websocket":
		return flag(s.isUpgrade()), true
	case "req.grpcservice":
		service, _ := s.grpcMethod()

		return service, true
	case "req.grpcmethod":
		_, method := s.grpcMethod()

		return method, true
	case "req.gqltype":
		op, _ := s.graphql()

		return op.Type, true
	case "req.gqlname":
		op, _ := s.graphql()

		return op.Name, true
	case "req.tunnel":
		return flag(s.Tun != nil), true
	case "tunnel.bytesout":
		return s.tunnel(func(t *Tunnel) int64 { return t.BytesOut }), true
	case "tunnel.bytesin":
		return s.tunnel(func(t *Tunnel) int64 { return t.BytesIn }), true
	case "ws.messages":
		return s.ws(func(w *WebSocket) string { return strconv.Itoa(w.Messages) }), true
	case "ws.subprotocol":
		return s.ws(func(w *WebSocket) string { return w.Subprotocol }), true
	case "ws.closecode":
		return s.ws(func(w *WebSocket) string {
			if w.CloseCode == 0 {
				return ""
			}

			return strconv.Itoa(w.CloseCode)
		}), true
	}

	if !strings.HasPrefix(key, "res.") {
		return "", false
	}

	switch key {
	case "res.statuscode":
		return s.res(func(r *Response) string { return strconv.Itoa(r.StatusCode) }), true
	case "res.statusreason":
		return s.res(func(r *Response) string { return r.Status }), true
	case "res.proto":
		return s.res(func(r *Response) string { return r.Proto }), true
	case "res.body":
		return s.res(func(r *Response) string { return string(r.Body) }), true
	case "res.size":
		return s.res(func(r *Response) string { return strconv.Itoa(len(r.Body)) }), true
	case "res.duration":
		return s.res(func(r *Response) string { return strconv.FormatInt(r.DurationMS, 10) }), true
	case "res.mime":
		return s.res(func(r *Response) string { return httpmsg.MediaType(r.Header.Get("Content-Type")) }), true
	case "res.type":
		return s.res(func(r *Response) string { return BodyType(r.Header.Get("Content-Type"), r.Body) }), true
	case "res.streamed":
		return s.res(func(r *Response) string { return flag(r.Streamed) }), true
	case "res.gqlerrors":
		return s.res(func(r *Response) string {
			// Absent rather than zero when the body is not a GraphQL
			// result: "no errors" and "not GraphQL" are different
			// answers, and a query for one must not match the other.
			n, ok := gqlmsg.Errors(r.Header.Get("Content-Type"), r.Body)
			if !ok {
				return ""
			}

			return strconv.Itoa(n)
		}), true
	case "res.grpcstatus":
		return s.res(func(r *Response) string {
			code, _ := grpcmsg.Status(r.Header, r.Trailer, s.grpcFrames(r))

			return code
		}), true
	case "res.ttfb":
		return s.timing(func(t *Timings) float64 {
			return t.Blocked + t.DNS + t.Connect + t.TLS + t.Send + t.Wait
		}), true
	case "res.blocked":
		return s.timing(func(t *Timings) float64 { return t.Blocked }), true
	case "res.dns":
		return s.timing(func(t *Timings) float64 { return t.DNS }), true
	case "res.connect":
		return s.timing(func(t *Timings) float64 { return t.Connect }), true
	case "res.tls":
		return s.timing(func(t *Timings) float64 { return t.TLS }), true
	case "res.send":
		return s.timing(func(t *Timings) float64 { return t.Send }), true
	case "res.wait":
		return s.timing(func(t *Timings) float64 { return t.Wait }), true
	case "res.receive":
		return s.timing(func(t *Timings) float64 { return t.Receive }), true
	case "res.reused":
		return s.res(func(r *Response) string {
			if r.Timings == nil {
				return ""
			}

			return flag(r.Timings.Reused)
		}), true
	case "res.serverip":
		return s.res(func(r *Response) string {
			if r.Timings == nil {
				return ""
			}

			return r.Timings.ServerAddr
		}), true
	case "res.tlsversion":
		return s.res(func(r *Response) string {
			if r.Timings == nil {
				return ""
			}

			return r.Timings.TLSVersion
		}), true
	case "res.alpn":
		return s.res(func(r *Response) string {
			if r.Timings == nil {
				return ""
			}

			return r.Timings.ALPN
		}), true
	}

	return "", false
}

// tunnel answers a byte count, and "" for an exchange that was
// decrypted - which is not the same as one that carried no bytes.
func (s HTTPSubject) tunnel(pick func(*Tunnel) int64) string {
	if s.Tun == nil {
		return ""
	}

	return strconv.FormatInt(pick(s.Tun), 10)
}

// timing answers a phase in milliseconds, and "" when the exchange was
// never measured - which is not the same as a phase of zero.
func (s HTTPSubject) timing(pick func(*Timings) float64) string {
	return s.res(func(r *Response) string {
		if r.Timings == nil {
			return ""
		}

		return strconv.FormatFloat(pick(r.Timings), 'f', -1, 64)
	})
}

// graphql reads the request body as a GraphQL operation. Recomputed per
// access, like grpcMethod: it costs a JSON scan and only a query that
// asks for a gql field pays it.
func (s HTTPSubject) graphql() (gqlmsg.Operation, bool) {
	return gqlmsg.Detect(s.Req.Header.Get("Content-Type"), s.Req.Body)
}

// grpcMethod names the service and method of a gRPC request, and nothing
// for any other request.
func (s HTTPSubject) grpcMethod() (string, string) {
	if grpcmsg.KindOf(s.Req.Header.Get("Content-Type")) == grpcmsg.KindNone || s.Req.URL == nil {
		return "", ""
	}

	return grpcmsg.Method(s.Req.URL.Path)
}

// grpcFrames splits a grpc-web body for its trailer frame, where the
// status of such a call lives. Other bodies yield nothing.
func (s HTTPSubject) grpcFrames(r *Response) []grpcmsg.Frame {
	kind := grpcmsg.KindOf(r.Header.Get("Content-Type"))
	if kind != grpcmsg.KindWeb && kind != grpcmsg.KindWebText {
		return nil
	}

	frames, _ := grpcmsg.Split(r.Body, kind, r.Header.Get("Grpc-Encoding"))

	return frames
}

// isUpgrade reads the request's own headers, so the key is known before
// any response and legal in the ignore filter.
func (s HTTPSubject) isUpgrade() bool {
	return httpmsg.IsWebSocketUpgrade(s.Req.Header)
}

func (s HTTPSubject) ws(fn func(*WebSocket) string) string {
	if s.WS == nil {
		return ""
	}

	return fn(s.WS)
}

// flag is how a yes-or-no field reads: "true", or "" so exists says no.
func flag(b bool) string {
	if b {
		return "true"
	}

	return ""
}

// Exists implements Subject.
func (s HTTPSubject) Exists(key string) (bool, bool) {
	kind, name, ok := splitDynamic(key)
	if !ok {
		if vals, known := s.Values(key); known {
			return len(vals) > 0, true
		}

		if v, known := s.Field(key); known {
			return v != "", true
		}

		if h, known := s.Headers(key); known {
			return len(h) > 0, true
		}

		return false, false
	}

	switch kind {
	case "req.header":
		return s.Req.Header.Values(name) != nil, true
	case "res.header":
		return s.Res != nil && s.Res.Header.Values(name) != nil, true
	case "res.trailer":
		return s.Res != nil && s.Res.Trailer.Values(name) != nil, true
	case "req.cookie":
		_, found := s.cookie(name)

		return found, true
	case "res.cookie":
		_, found := s.setCookie(name)

		return found, true
	case "req.query":
		if s.Req.URL == nil {
			return false, true
		}

		return s.Req.URL.Query().Has(name), true
	case "req.form":
		return s.form().Has(name), true
	}

	return false, false
}

// Headers implements Subject.
func (s HTTPSubject) Headers(key string) (http.Header, bool) {
	switch key {
	case "req.headers":
		return s.Req.Header, true
	case "res.headers":
		if s.Res == nil {
			return nil, true
		}

		return s.Res.Header, true
	}

	return nil, false
}

// Values implements Subject.
func (s HTTPSubject) Values(key string) ([]string, bool) {
	switch key {
	case "req.tag", "req.tags":
		return s.Req.Tags, true
	}

	return nil, false
}

// Text implements Subject.
func (s HTTPSubject) Text() []string {
	out := []string{s.Req.Method, s.urlString(), string(s.Req.Body), s.Req.Note}
	out = append(out, s.Req.Tags...)
	out = append(out, HeaderLines(s.Req.Header)...)

	if s.Res != nil {
		out = append(out, s.Res.Status, string(s.Res.Body))
		out = append(out, HeaderLines(s.Res.Header)...)
	}

	return out
}

// dynamic answers the <prefix>.<name> keys.
func (s HTTPSubject) dynamic(key string) (string, bool) {
	kind, name, ok := splitDynamic(key)
	if !ok {
		return "", false
	}

	switch kind {
	case "req.header":
		return s.Req.Header.Get(name), true
	case "res.header":
		if s.Res == nil {
			return "", true
		}

		return s.Res.Header.Get(name), true
	case "res.trailer":
		if s.Res == nil {
			return "", true
		}

		return s.Res.Trailer.Get(name), true
	case "req.cookie":
		v, _ := s.cookie(name)

		return v, true
	case "res.cookie":
		v, _ := s.setCookie(name)

		return v, true
	case "req.query":
		if s.Req.URL == nil {
			return "", true
		}

		return s.Req.URL.Query().Get(name), true
	case "req.form":
		return s.form().Get(name), true
	}

	return "", false
}

var dynamicPrefixes = []string{"req.header.", "res.header.", "res.trailer.", "req.cookie.", "res.cookie.", "req.query.", "req.form."}

func splitDynamic(key string) (kind, name string, ok bool) {
	for _, p := range dynamicPrefixes {
		if rest, found := strings.CutPrefix(key, p); found && rest != "" {
			return strings.TrimSuffix(p, "."), rest, true
		}
	}

	return "", "", false
}

func (s HTTPSubject) urlString() string {
	if s.Req.URL == nil {
		return ""
	}

	return s.Req.URL.String()
}

func (s HTTPSubject) urlPart(fn func(*url.URL) string) string {
	if s.Req.URL == nil {
		return ""
	}

	return fn(s.Req.URL)
}

func (s HTTPSubject) res(fn func(*Response) string) string {
	if s.Res == nil {
		return ""
	}

	return fn(s.Res)
}

// cookie finds a request cookie by name, case-insensitively on the name
// the way header names are, since a filter is typed by hand.
func (s HTTPSubject) cookie(name string) (string, bool) {
	for _, raw := range s.Req.Header.Values("Cookie") {
		for part := range strings.SplitSeq(raw, ";") {
			k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
			if strings.EqualFold(k, name) {
				return v, true
			}
		}
	}

	return "", false
}

// setCookie finds a Set-Cookie by cookie name and returns its value
// without the attributes.
func (s HTTPSubject) setCookie(name string) (string, bool) {
	if s.Res == nil {
		return "", false
	}

	for _, raw := range s.Res.Header.Values("Set-Cookie") {
		first, _, _ := strings.Cut(raw, ";")
		k, v, _ := strings.Cut(strings.TrimSpace(first), "=")
		if strings.EqualFold(k, name) {
			return v, true
		}
	}

	return "", false
}

// form parses a urlencoded request body. Anything else is empty.
func (s HTTPSubject) form() url.Values {
	if !strings.Contains(strings.ToLower(s.Req.Header.Get("Content-Type")), "x-www-form-urlencoded") {
		return url.Values{}
	}

	values, err := url.ParseQuery(string(s.Req.Body))
	if err != nil {
		return url.Values{}
	}

	return values
}

// BodyType names a body's kind for `res.type`: grpc, json, html, xml, js,
// css, text, image, font, audio, video, pdf, binary, or "" for none.
func BodyType(contentType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}

	mt := httpmsg.MediaType(contentType)

	switch {
	case strings.HasPrefix(mt, "application/grpc"):
		return "grpc"
	case strings.Contains(mt, "json"):
		return "json"
	case strings.Contains(mt, "html"):
		return "html"
	case strings.Contains(mt, "xml"):
		return "xml"
	case strings.Contains(mt, "javascript"), strings.Contains(mt, "ecmascript"):
		return "js"
	case strings.Contains(mt, "css"):
		return "css"
	case strings.HasPrefix(mt, "image/"):
		return "image"
	case strings.HasPrefix(mt, "font/"), strings.Contains(mt, "font"):
		return "font"
	case strings.HasPrefix(mt, "audio/"):
		return "audio"
	case strings.HasPrefix(mt, "video/"):
		return "video"
	case mt == "application/pdf":
		return "pdf"
	case httpmsg.IsBinary(contentType, body):
		return "binary"
	default:
		return "text"
	}
}
