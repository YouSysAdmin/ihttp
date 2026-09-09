package httpmsg

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBodyIsAStringOnTheWire(t *testing.T) {
	type msg struct {
		B Body `json:"b"`
	}

	out, err := json.Marshal(msg{B: Body("hi")})
	if err != nil {
		t.Fatal(err)
	}

	if string(out) != `{"b":"hi"}` {
		t.Fatalf("got %s", out)
	}

	var back msg
	if err := json.Unmarshal([]byte(`{"b":null}`), &back); err != nil {
		t.Fatal(err)
	}

	if back.B != nil {
		t.Fatalf("null must decode to an empty body, got %q", back.B)
	}

	if err := json.Unmarshal([]byte(`{"b":"x"}`), &back); err != nil || string(back.B) != "x" {
		t.Fatalf("got %q, %v", back.B, err)
	}
}

func TestHeadersRoundTrip(t *testing.T) {
	h := http.Header{"X-B": {"2"}, "X-A": {"1", "3"}}
	hs := FromHTTP(h)

	if hs[0].Name != "X-A" || hs[1].Value != "3" || hs[2].Name != "X-B" {
		t.Fatalf("unexpected order: %+v", hs)
	}

	if hs.Get("x-b") != "2" {
		t.Fatal("Get must ignore case")
	}

	if got := hs.ToHTTP(); len(got["X-A"]) != 2 {
		t.Fatalf("ToHTTP lost values: %v", got)
	}
}

func TestReadBodyBounds(t *testing.T) {
	b, truncated, err := ReadBody(strings.NewReader("abcdef"), 4)
	if err != nil || !truncated || string(b) != "abcd" {
		t.Fatalf("got %q %v %v", b, truncated, err)
	}
}

func TestDecompressGzip(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte("hello"))
	_ = gz.Close()

	res := &http.Response{
		Header: http.Header{"Content-Encoding": {"gzip"}},
		Body:   io.NopCloser(&buf),
	}

	if err := Decompress(res); err != nil {
		t.Fatal(err)
	}

	got, _ := io.ReadAll(res.Body)
	if string(got) != "hello" || res.Header.Get("Content-Encoding") != "" || res.ContentLength != 5 {
		t.Fatalf("got %q, headers %v", got, res.Header)
	}
}

// A body that does not decode is handed on as it was, whole.
func TestDecompressLeavesABrokenBodyAlone(t *testing.T) {
	var good bytes.Buffer
	gz := gzip.NewWriter(&good)
	_, _ = gz.Write([]byte("hello, this is long enough to be cut"))
	_ = gz.Close()

	cases := map[string][]byte{
		"bad header": []byte("not gzip at all"),
		"truncated":  good.Bytes()[:good.Len()/2],
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			res := &http.Response{
				Header: http.Header{"Content-Encoding": {"gzip"}},
				Body:   io.NopCloser(bytes.NewReader(raw)),
			}

			if err := Decompress(res); err == nil {
				t.Fatal("expected an error")
			}

			got, _ := io.ReadAll(res.Body)
			if !bytes.Equal(got, raw) || res.Header.Get("Content-Encoding") != "gzip" {
				t.Fatalf("body changed: %d of %d bytes, encoding %q", len(got), len(raw), res.Header.Get("Content-Encoding"))
			}
		})
	}
}

// deflate as most servers send it, zlib-wrapped, and as raw DEFLATE.
func TestDecompressDeflate(t *testing.T) {
	var wrapped bytes.Buffer
	zw := zlib.NewWriter(&wrapped)
	_, _ = zw.Write([]byte("hello"))
	_ = zw.Close()

	var raw bytes.Buffer
	fw, _ := flate.NewWriter(&raw, flate.DefaultCompression)
	_, _ = fw.Write([]byte("hello"))
	_ = fw.Close()

	for name, body := range map[string][]byte{"zlib": wrapped.Bytes(), "raw": raw.Bytes()} {
		t.Run(name, func(t *testing.T) {
			res := &http.Response{
				Header: http.Header{"Content-Encoding": {"deflate"}},
				Body:   io.NopCloser(bytes.NewReader(body)),
			}

			if err := Decompress(res); err != nil {
				t.Fatal(err)
			}

			got, _ := io.ReadAll(res.Body)
			if string(got) != "hello" {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	cases := []struct {
		ct   string
		body string
		want bool
	}{
		{"application/json", "{}", false},
		{"text/html; charset=utf-8", "<p>", false},
		{"image/png", "\x89PNG", true},
		{"", "\x89PNG\x00\x00", true},
		{"", "plain words", false},
		{"application/octet-stream", "abc", true},
		{"", "", false},
		{"", "\xff\xfe\xfd\xfc\xfb\xfa\xf9\xf8 x", true},
	}

	for _, c := range cases {
		if got := IsBinary(c.ct, []byte(c.body)); got != c.want {
			t.Errorf("IsBinary(%q, %q) = %v, want %v", c.ct, c.body, got, c.want)
		}
	}
}

// ToHTTP keeps the names as written, because they go on the wire.
// Lookup canonicalises them, because http.Header.Get canonicalises the
// key it is given - a header stored the way HTTP/2 and a HAR write it
// would otherwise be unreachable.
func TestLookupCanonicalisesWhatToHTTPKeepsVerbatim(t *testing.T) {
	hs := Headers{
		{Name: "content-type", Value: "image/png"},
		{Name: "x-Custom-CASE", Value: "kept"},
	}

	// A non-canonical key in an http.Header is what ToHTTP deliberately
	// produces, so the wire carries the casing the operator typed. The
	// key is built rather than written as a constant because staticcheck
	// rightly objects to the literal - which is the whole hazard this
	// test is about.
	wire := hs.ToHTTP()

	verbatim := strings.ToLower("Content-Type")
	if _, ok := wire[verbatim]; !ok {
		t.Error("ToHTTP did not keep the name as written")
	}

	if wire.Get("Content-Type") != "" {
		t.Error("http.Header.Get reached a non-canonical key, so this test is no longer about anything")
	}

	read := hs.Lookup()
	if got := read.Get("Content-Type"); got != "image/png" {
		t.Errorf("Lookup().Get(Content-Type) = %q, want image/png", got)
	}

	if got := read.Get("x-custom-case"); got != "kept" {
		t.Errorf("Lookup().Get(x-custom-case) = %q, want kept", got)
	}

	// Headers.Get has always folded case and still must.
	if got := hs.Get("CONTENT-TYPE"); got != "image/png" {
		t.Errorf("Headers.Get = %q", got)
	}
}
