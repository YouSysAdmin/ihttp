package reqlog

import (
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/ids"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// ImportResult is what an import wrote, on the wire and on the stream.
type ImportResult struct {
	Entries int `json:"entries"`
}

// MaxHARImportBytes bounds an import body. A HAR is JSON with base64
// bodies inside it, so it is several times the traffic it describes.
const MaxHARImportBytes = 2 << 30

// ErrBadHAR is a file that is not a HAR the importer can read.
var ErrBadHAR = errors.New("reqlog: not a HAR file")

// ImportedTag marks every entry an import created, so the entries a
// capture actually saw are one filter away: NOT req.tag = imported.
const ImportedTag = "imported"

// harInEntry is one entry as it is read back. It is deliberately not the
// same type the writer uses: a file from another tool fills in members
// the writer never writes, and leaves out ones it always does.
type harInEntry struct {
	StartedDateTime string  `json:"startedDateTime"`
	Time            float64 `json:"time"`
	Request         struct {
		Method      string   `json:"method"`
		URL         string   `json:"url"`
		HTTPVersion string   `json:"httpVersion"`
		Headers     []harNV  `json:"headers"`
		PostData    *harPost `json:"postData"`
	} `json:"request"`
	Response struct {
		Status      int        `json:"status"`
		StatusText  string     `json:"statusText"`
		HTTPVersion string     `json:"httpVersion"`
		Headers     []harNV    `json:"headers"`
		Content     harContent `json:"content"`
	} `json:"response"`
	Timings         *harInTimings `json:"timings"`
	ServerIPAddress string        `json:"serverIPAddress"`
	IHTTP           *harMeta      `json:"_ihttp"`
}

// harInTimings is the timings object with every member optional, since
// only send, wait and receive are required by the spec and a phase that
// does not apply is -1.
type harInTimings struct {
	Blocked *float64 `json:"blocked"`
	DNS     *float64 `json:"dns"`
	Connect *float64 `json:"connect"`
	SSL     *float64 `json:"ssl"`
	Send    *float64 `json:"send"`
	Wait    *float64 `json:"wait"`
	Receive *float64 `json:"receive"`
}

// ImportHAR reads a HAR document into the open project's log and returns
// how many entries it wrote.
//
// Every entry is a new row: ids are minted from the entry's own
// startedDateTime with ids.NewAt, so an imported log sorts by when it
// happened rather than by when it was read, and an id from a file can
// never overwrite a row already in the project. That means a round trip
// through the format does not preserve ids, which is the right trade -
// the entries are new rows in whatever project is open.
func (s *Service) ImportHAR(ctx context.Context, r io.Reader) (int, error) {
	active := s.projects.Active()
	if active == nil {
		return 0, project.ErrNoActiveProject
	}

	dec := jsontext.NewDecoder(r, jsontext.AllowInvalidUTF8(true))

	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return 0, fmt.Errorf("%w: not a JSON object", ErrBadHAR)
	}

	n := 0

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return n, fmt.Errorf("%w: %v", ErrBadHAR, err)
		}

		if tok.Kind() == '}' {
			break
		}

		if tok.String() != "log" {
			if err := dec.SkipValue(); err != nil {
				return n, fmt.Errorf("%w: %v", ErrBadHAR, err)
			}

			continue
		}

		n, err = s.readHARLog(ctx, dec, active.Project.ID)
		if err != nil {
			return n, err
		}
	}

	if n == 0 {
		return 0, fmt.Errorf("%w: it carries no entries", ErrBadHAR)
	}

	s.log.Info("reqlog: imported a HAR file", "entries", n, "project", active.Project.ID)
	s.bus.Emit("reqlog.imported", ImportResult{Entries: n})

	return n, nil
}

// readHARLog walks the log object and writes each entry as it comes.
func (s *Service) readHARLog(ctx context.Context, dec *jsontext.Decoder, projectID string) (int, error) {
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return 0, fmt.Errorf("%w: log is not an object", ErrBadHAR)
	}

	n := 0

	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return n, fmt.Errorf("%w: %v", ErrBadHAR, err)
		}

		if tok.Kind() == '}' {
			return n, nil
		}

		if tok.String() != "entries" {
			if err := dec.SkipValue(); err != nil {
				return n, fmt.Errorf("%w: %v", ErrBadHAR, err)
			}

			continue
		}

		if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '[' {
			return n, fmt.Errorf("%w: entries is not an array", ErrBadHAR)
		}

		for dec.PeekKind() != ']' {
			if err := ctx.Err(); err != nil {
				return n, err
			}

			var in harInEntry
			if err := json.UnmarshalDecode(dec, &in); err != nil {
				return n, fmt.Errorf("%w: entry %d: %v", ErrBadHAR, n+1, err)
			}

			entry := s.entryFromHAR(in, projectID)
			if err := s.store.Put(ctx, entry); err != nil {
				return n, err
			}

			n++
		}

		if _, err := dec.ReadToken(); err != nil {
			return n, fmt.Errorf("%w: %v", ErrBadHAR, err)
		}
	}
}

// entryFromHAR maps one HAR entry onto a stored entry.
func (s *Service) entryFromHAR(in harInEntry, projectID string) reqlog.Entry {
	started := harTime(in.StartedDateTime)

	entry := reqlog.Entry{
		ID:        ids.NewAt(started),
		ProjectID: projectID,
		CreatedAt: started,
		Method:    firstNonEmptyStr(in.Request.Method, http.MethodGet),
		URL:       in.Request.URL,
		Proto:     firstNonEmptyStr(in.Request.HTTPVersion, "HTTP/1.1"),
		Headers:   harHeadersIn(in.Request.Headers),
		Tags:      []string{ImportedTag},
	}

	if in.Request.PostData != nil {
		entry.Body = harBodyIn(in.Request.PostData.Text, in.Request.PostData.Encoding)
	}

	if in.IHTTP != nil {
		entry.BodyTruncated = in.IHTTP.RequestTruncated
	}

	// A HAR writes an unanswered request with status 0, which is how a
	// browser exports a request that never came back.
	pending := in.Response.Status == 0
	if in.IHTTP != nil && in.IHTTP.Pending {
		pending = true
	}

	if pending {
		return entry
	}

	res := &reqlog.Response{
		Proto:      firstNonEmptyStr(in.Response.HTTPVersion, entry.Proto),
		StatusCode: in.Response.Status,
		Status:     firstNonEmptyStr(in.Response.StatusText, http.StatusText(in.Response.Status)),
		Headers:    harHeadersIn(in.Response.Headers),
		Body:       harBodyIn(in.Response.Content.Text, in.Response.Content.Encoding),
		ReceivedAt: started.Add(time.Duration(in.Time) * time.Millisecond),
		DurationMS: int64(in.Time),
		Timings:    harTimingsIn(in.Timings, in.ServerIPAddress),
	}

	if in.IHTTP != nil {
		res.BodyTruncated = in.IHTTP.ResponseTruncated
		res.BodyStreamed = in.IHTTP.ResponseStreamed
	}

	entry.Response = res

	return entry
}

// harTimingsIn maps the HAR timings back. A negative phase is the
// spec's "does not apply", and connect INCLUDES ssl there, so the ssl
// share is taken back out. Nothing measurable at all is stored as not
// measured rather than as a row of zeros.
func harTimingsIn(in *harInTimings, serverIP string) *reqlog.Timings {
	if in == nil {
		return nil
	}

	// The three connection phases marked as not applying is exactly what
	// a tool writes for a request on a connection that was already open,
	// so that is read back as reused rather than dropped.
	reused := negative(in.DNS) && negative(in.Connect) && negative(in.SSL)

	out := reqlog.Timings{
		Blocked:    atLeastZero(in.Blocked),
		DNS:        atLeastZero(in.DNS),
		Connect:    atLeastZero(in.Connect) - atLeastZero(in.SSL),
		TLS:        atLeastZero(in.SSL),
		Send:       atLeastZero(in.Send),
		Wait:       atLeastZero(in.Wait),
		Receive:    atLeastZero(in.Receive),
		Reused:     reused,
		ServerAddr: serverIP,
	}

	if out.Connect < 0 {
		out.Connect = 0
	}

	if out == (reqlog.Timings{}) {
		return nil
	}

	return &out
}

func negative(v *float64) bool {
	return v != nil && *v < 0
}

func atLeastZero(v *float64) float64 {
	if v == nil || *v < 0 {
		return 0
	}

	return *v
}

// harBodyIn decodes a body the way the file said it was encoded.
// Undecodable base64 is kept as the text it was, since losing it would
// be worse than reading it wrong.
func harBodyIn(text, encoding string) httpmsg.Body {
	if text == "" {
		return nil
	}

	if strings.EqualFold(encoding, "base64") {
		if raw, err := base64.StdEncoding.DecodeString(text); err == nil {
			return raw
		}
	}

	return httpmsg.Body(text)
}

// harHeadersIn keeps the file's order and its names as written.
func harHeadersIn(in []harNV) httpmsg.Headers {
	if len(in) == 0 {
		return nil
	}

	out := make(httpmsg.Headers, 0, len(in))
	for _, h := range in {
		if h.Name == "" {
			continue
		}

		out = append(out, httpmsg.Header{Name: h.Name, Value: h.Value})
	}

	return out
}

// harTime reads the entry's start, falling back to now so an entry with
// an unreadable date is still imported rather than dropped.
func harTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999-0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}

	return time.Now().UTC()
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
