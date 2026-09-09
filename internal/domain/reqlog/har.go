package reqlog

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
	"github.com/yousysadmin/ihttp/pkg"
)

// The HAR 1.2 shapes. Only what the log knows is filled in. Sizes the
// format wants and the log never measured are -1, as the spec allows.
// The _ihttp member carries the entry id and what happened to the body,
// which HAR has no word for.
type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPost struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`

	// Encoding is not in HAR 1.2 for postData, which has no way to carry
	// bytes, so it is a custom member. "base64" when set.
	Encoding string `json:"_encoding,omitempty"`
}

type harRequest struct {
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	HTTPVersion string   `json:"httpVersion"`
	Cookies     []harNV  `json:"cookies"`
	Headers     []harNV  `json:"headers"`
	QueryString []harNV  `json:"queryString"`
	PostData    *harPost `json:"postData,omitzero"`
	HeadersSize int      `json:"headersSize"`
	BodySize    int      `json:"bodySize"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

type harResponse struct {
	Status      int        `json:"status"`
	StatusText  string     `json:"statusText"`
	HTTPVersion string     `json:"httpVersion"`
	Cookies     []harNV    `json:"cookies"`
	Headers     []harNV    `json:"headers"`
	Content     harContent `json:"content"`
	RedirectURL string     `json:"redirectURL"`
	HeadersSize int        `json:"headersSize"`
	BodySize    int        `json:"bodySize"`
}

// harTimings is the HAR timings object. The spec's rules, which other
// tools rely on: send, wait and receive are required, the rest are -1
// when they do not apply, and connect INCLUDES ssl.
type harTimings struct {
	Blocked float64 `json:"blocked"`
	DNS     float64 `json:"dns"`
	Connect float64 `json:"connect"`
	SSL     float64 `json:"ssl"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

type harMeta struct {
	ID                string `json:"id"`
	RequestTruncated  bool   `json:"request_truncated,omitzero"`
	ResponseTruncated bool   `json:"response_truncated,omitzero"`
	ResponseStreamed  bool   `json:"response_streamed,omitzero"`
	Pending           bool   `json:"pending,omitzero"`
}

type harEntry struct {
	StartedDateTime time.Time   `json:"startedDateTime"`
	Time            int64       `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitzero"`
	IHTTP           harMeta     `json:"_ihttp"`
}

// WriteHAR streams a HAR 1.2 document of the entries each yields, in the
// order they come. An entry without a response is written as pending
// with status 0, the way a browser exports an unanswered request.
func WriteHAR(w io.Writer, each func(func(reqlog.Entry) error) error) error {
	enc := response.NewStreamEncoder(w)

	for _, tok := range []jsontext.Token{
		jsontext.BeginObject, jsontext.String("log"), jsontext.BeginObject,
		jsontext.String("version"), jsontext.String("1.2"),
		jsontext.String("creator"),
	} {
		if err := enc.WriteToken(tok); err != nil {
			return err
		}
	}

	if err := response.MarshalEncode(enc, harCreator{Name: pkg.AppName, Version: pkg.Version}); err != nil {
		return err
	}

	for _, tok := range []jsontext.Token{jsontext.String("entries"), jsontext.BeginArray} {
		if err := enc.WriteToken(tok); err != nil {
			return err
		}
	}

	if err := each(func(e reqlog.Entry) error {
		return response.MarshalEncode(enc, harEntryOf(e))
	}); err != nil {
		return err
	}

	for _, tok := range []jsontext.Token{jsontext.EndArray, jsontext.EndObject, jsontext.EndObject} {
		if err := enc.WriteToken(tok); err != nil {
			return err
		}
	}

	return nil
}

func harEntryOf(e reqlog.Entry) harEntry {
	out := harEntry{
		StartedDateTime: e.CreatedAt,
		Request: harRequest{
			Method:      e.Method,
			URL:         e.URL,
			HTTPVersion: e.Proto,
			Cookies:     harCookies((&http.Request{Header: e.Headers.Lookup()}).Cookies()),
			Headers:     harHeaders(e.Headers),
			QueryString: harQuery(e.URL),
			HeadersSize: -1,
			BodySize:    len(e.Body),
		},
		Response: harResponse{
			Cookies: []harNV{},
			Headers: []harNV{},
			Content: harContent{},
		},
		IHTTP: harMeta{ID: e.ID, RequestTruncated: e.BodyTruncated, Pending: e.Response == nil},
	}

	if len(e.Body) > 0 {
		out.Request.PostData = harBody(e.Headers, e.Body)
	}

	r := e.Response
	if r == nil {
		return out
	}

	out.Time = r.DurationMS
	out.Timings = harTimingsOf(r)

	if t := r.Timings; t != nil {
		out.ServerIPAddress = hostOf(t.ServerAddr)
	}
	out.Response = harResponse{
		Status:      r.StatusCode,
		StatusText:  r.Status,
		HTTPVersion: r.Proto,
		Cookies:     harCookies((&http.Response{Header: r.Headers.Lookup()}).Cookies()),
		Headers:     harHeaders(r.Headers),
		RedirectURL: r.Headers.Get("Location"),
		HeadersSize: -1,
		BodySize:    len(r.Body),
	}
	out.Response.Content = harContent{Size: len(r.Body), MimeType: r.Headers.Get("Content-Type")}
	out.IHTTP.ResponseTruncated = r.BodyTruncated
	out.IHTTP.ResponseStreamed = r.BodyStreamed

	if len(r.Body) > 0 {
		post := harBody(r.Headers, r.Body)
		out.Response.Content.Text = post.Text
		out.Response.Content.Encoding = post.Encoding
	}

	return out
}

// harBody renders a body as text, or as base64 when the bytes are not
// text, which the encoding member says.
func harBody(headers httpmsg.Headers, body httpmsg.Body) *harPost {
	ct := headers.Get("Content-Type")
	if httpmsg.IsBinary(ct, body) {
		return &harPost{MimeType: ct, Text: base64.StdEncoding.EncodeToString(body), Encoding: "base64"}
	}

	return &harPost{MimeType: ct, Text: string(body)}
}

func harHeaders(h httpmsg.Headers) []harNV {
	out := make([]harNV, 0, len(h))
	for _, x := range h {
		out = append(out, harNV{Name: x.Name, Value: x.Value})
	}

	return out
}

func harCookies(cookies []*http.Cookie) []harNV {
	out := make([]harNV, 0, len(cookies))
	for _, c := range cookies {
		out = append(out, harNV{Name: c.Name, Value: c.Value})
	}

	return out
}

func harQuery(rawURL string) []harNV {
	out := []harNV{}

	u, err := url.Parse(rawURL)
	if err != nil {
		return out
	}

	// url.Values loses the order the query was written in, so the raw
	// query is walked instead.
	for pair := range strings.SplitSeq(u.RawQuery, "&") {
		if pair == "" {
			continue
		}

		k, v, _ := strings.Cut(pair, "=")
		out = append(out, harNV{Name: unescape(k), Value: unescape(v)})
	}

	return out
}

func unescape(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}

	return s
}

// harTimingsOf renders the phase breakdown the way the HAR spec wants
// it. Without a measurement the whole duration is reported as wait and
// the optional phases as -1, which is the spec's "does not apply" and
// keeps an unmeasured entry honest instead of drawing it as instant.
func harTimingsOf(r *reqlog.Response) harTimings {
	t := r.Timings
	if t == nil {
		return harTimings{
			Blocked: -1,
			DNS:     -1,
			Connect: -1,
			SSL:     -1,
			Send:    0,
			Wait:    float64(r.DurationMS),
			Receive: 0,
		}
	}

	out := harTimings{
		Blocked: t.Blocked,
		DNS:     t.DNS,
		// The spec has connect include ssl.
		Connect: t.Connect + t.TLS,
		SSL:     t.TLS,
		Send:    t.Send,
		Wait:    t.Wait,
		Receive: t.Receive,
	}

	// A reused connection did not do these at all, which is -1 and not
	// zero. Some tools draw a zero as a real phase.
	if t.Reused {
		out.DNS, out.Connect, out.SSL = -1, -1, -1
	}

	return out
}

// hostOf is the address without its port, since HAR wants the IP alone.
func hostOf(addr string) string {
	if addr == "" {
		return ""
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	return host
}
