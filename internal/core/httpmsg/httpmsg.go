// Package httpmsg holds the shared vocabulary for an HTTP message as the
// product stores and renders it: an ordered header list, a body that
// crosses the wire as a string, and the readers that bound how much of a
// network body is kept.
package httpmsg

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Header is one header line. A list rather than a map because order is
// meaningful to a person reading a request, and a map loses it.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Headers is an ordered header list.
type Headers []Header

// FromHTTP flattens an http.Header into a Headers, sorted by name so
// the order is stable across renders. The map has no order to keep.
func FromHTTP(h http.Header) Headers {
	out := make(Headers, 0, len(h))

	for _, name := range slices.Sorted(maps.Keys(h)) {
		for _, v := range h[name] {
			out = append(out, Header{Name: name, Value: v})
		}
	}

	return out
}

// MediaType is the type and subtype of a Content-Type, lowercased, with
// the parameters cut off.
func MediaType(contentType string) string {
	mt, _, _ := strings.Cut(contentType, ";")

	return strings.ToLower(strings.TrimSpace(mt))
}

// IsWebSocketUpgrade reports headers asking to become a WebSocket.
func IsWebSocketUpgrade(h http.Header) bool {
	if !strings.EqualFold(h.Get("Upgrade"), "websocket") {
		return false
	}

	for _, v := range h.Values("Connection") {
		for tok := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
				return true
			}
		}
	}

	return false
}

// ToHTTP expands the list into an http.Header. Names are added as
// written, not canonicalized, so a header a person typed in the console
// reaches the wire with the casing they chose.
func (hs Headers) ToHTTP() http.Header {
	out := make(http.Header, len(hs))

	for _, h := range hs {
		out[h.Name] = append(out[h.Name], h.Value)
	}

	return out
}

// Lookup is the same headers as an http.Header with CANONICAL names,
// for reading a value back out. ToHTTP keeps the names as written,
// because that is what goes on the wire, but http.Header.Get
// canonicalises the key it is given - so a header stored as
// "content-type", which is how HTTP/2 writes it and how a HAR carries
// it, is unreachable through a map built by ToHTTP.
//
// Use ToHTTP for a message being sent, and Lookup for a value being
// read: a scope match, a filter subject, a content type, a cookie.
func (hs Headers) Lookup() http.Header {
	out := make(http.Header, len(hs))

	for _, h := range hs {
		out.Add(h.Name, h.Value)
	}

	return out
}

// Get returns the first value carried under name, matched without
// regard to case, or "".
func (hs Headers) Get(name string) string {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}

	return ""
}

// Body is raw message bytes. On the wire it is a JSON STRING and never
// base64: the console shows and edits bodies as text, and the bytes
// that are not valid UTF-8 are coerced to U+FFFD by the encoder rather
// than refused. Binary bodies are therefore lossy through the console,
// which is the trade the product makes on purpose.
type Body []byte

// MarshalJSONTo implements json.MarshalerTo.
func (b Body) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(string(b)))
}

// UnmarshalJSONFrom implements json.UnmarshalerFrom. null is an empty
// body, so a client may omit or null the field interchangeably.
func (b *Body) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}

	switch tok.Kind() {
	case 'n':
		*b = nil
	case '"':
		*b = Body(tok.String())
	default:
		return fmt.Errorf("httpmsg: body must be a string, got %v", tok.Kind())
	}

	return nil
}

var _ json.MarshalerTo = Body(nil)
var _ json.UnmarshalerFrom = (*Body)(nil)

// ReadBody drains r up to limit bytes and returns them. A body longer
// than the limit is truncated and reported through the bool so the
// caller can say so, rather than holding an unbounded upload in memory
// because a client sent one.
func ReadBody(r io.Reader, limit int64) (Body, bool, error) {
	if r == nil {
		return nil, false, nil
	}

	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}

	if int64(len(data)) > limit {
		return Body(data[:limit]), true, nil
	}

	return Body(data), false, nil
}

// Replace puts body back on the message reader after it has been read,
// so the next reader sees exactly what the first one did.
func Replace(body Body) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(body))
}

// MaxDecompressed bounds what a compressed body may expand to. A body
// past it is left encoded.
const MaxDecompressed = 64 << 20

// Decompress replaces a gzip or deflate response body with its decoded
// bytes and fixes the framing headers to match. Other encodings are left
// alone - the proxy strips them from Accept-Encoding so a server should
// never send one. A body that fails to decode, or expands past
// MaxDecompressed, is left as it was with the error reported, because a
// broken stream is still worth logging.
func Decompress(res *http.Response) error {
	enc := strings.ToLower(strings.TrimSpace(res.Header.Get("Content-Encoding")))

	switch enc {
	case "gzip", "x-gzip", "deflate":
	default:
		return nil
	}

	raw, _, err := ReadBody(res.Body, 1<<62)
	if err != nil {
		return fmt.Errorf("httpmsg: read %s body: %w", enc, err)
	}

	res.Body = Replace(raw)

	data, err := inflate(enc, raw)
	if err != nil {
		return fmt.Errorf("httpmsg: decompress %s body: %w", enc, err)
	}

	res.Body = Replace(Body(data))
	res.Header.Del("Content-Encoding")
	res.Header.Set("Content-Length", strconv.Itoa(len(data)))
	res.ContentLength = int64(len(data))

	return nil
}

// inflate decodes raw as enc. deflate is tried as zlib first, which is
// what most servers send under that name, then as raw DEFLATE.
func inflate(enc string, raw []byte) ([]byte, error) {
	var reader io.ReadCloser

	switch enc {
	case "gzip", "x-gzip":
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}

		reader = gz
	default:
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			reader = flate.NewReader(bytes.NewReader(raw))
		} else {
			reader = zr
		}
	}

	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(io.LimitReader(reader, MaxDecompressed+1))
	if err != nil {
		return nil, err
	}

	if len(data) > MaxDecompressed {
		return nil, errors.New("decoded body is over the limit")
	}

	return data, nil
}

// StatusReason is the reason phrase alone: "OK" out of "200 OK". A
// status a server wrote without a phrase yields "".
func StatusReason(status string) string {
	_, reason, _ := strings.Cut(status, " ")

	return reason
}

// Protocols the sender can be asked to speak. HTTP/2 is attempted and
// the transport falls back to HTTP/1.1 when the server will not.
const (
	ProtoHTTP10 = "HTTP/1.0"
	ProtoHTTP11 = "HTTP/1.1"
	ProtoHTTP20 = "HTTP/2.0"
)

// ValidProto reports whether proto is one the sender knows how to speak.
func ValidProto(proto string) bool {
	return proto == ProtoHTTP10 || proto == ProtoHTTP11 || proto == ProtoHTTP20
}

// Methods lists the request methods the console offers. Any token is
// accepted on the wire, so this is the menu, not a gate.
var Methods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
	http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace,
}

// textTypes are the content types that are text whatever their bytes
// look like, matched as substrings of the media type.
var textTypes = []string{
	"text/", "json", "xml", "javascript", "ecmascript", "x-www-form-urlencoded",
	"graphql", "yaml", "csv", "x-ndjson", "svg", "html", "css",
}

// IsBinary reports whether a body should be shown as bytes rather than
// text. A text content type wins. Without one, the bytes decide: a NUL
// or a run of invalid UTF-8 in the first kilobytes is not text.
func IsBinary(contentType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}

	media := MediaType(contentType)

	// A gRPC body is protobuf frames unless it says json or text.
	if strings.HasPrefix(media, "application/grpc") {
		return !strings.Contains(media, "json") && !strings.Contains(media, "-text")
	}

	for _, t := range textTypes {
		if strings.Contains(media, t) {
			return false
		}
	}

	if strings.HasPrefix(media, "image/") || strings.HasPrefix(media, "audio/") ||
		strings.HasPrefix(media, "video/") || strings.HasPrefix(media, "font/") ||
		media == "application/octet-stream" || media == "application/pdf" ||
		media == "application/zip" || media == "application/gzip" ||
		strings.HasPrefix(media, "application/x-protobuf") || media == "application/wasm" {
		return true
	}

	n := min(len(body), 8192)
	sample := body[:n]
	if bytes.IndexByte(sample, 0) >= 0 {
		return true
	}

	invalid := 0
	for len(sample) > 0 {
		r, size := utf8.DecodeRune(sample)
		if r == utf8.RuneError && size == 1 {
			invalid++
		}

		sample = sample[size:]
	}

	return invalid > n/32
}
