package proxy_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// grpcFrame prefixes a message the gRPC way.
func grpcFrame(msg []byte) []byte {
	out := []byte{0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:5], uint32(len(msg)))

	return append(out, msg...)
}

func TestGRPCUnaryCallIsLoggedWithTrailersAndDecoded(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "grpc")

	// field 1 = varint 150, field 2 = "hello"
	reply := []byte{0x08, 0x96, 0x01, 0x12, 0x05, 'h', 'e', 'l', 'l', 'o'}

	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/grpc+proto")
		w.Header().Set("Trailer", "Grpc-Status, Grpc-Message")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(grpcFrame(reply))
		w.Header().Set("Grpc-Status", "0")
		w.Header().Set("Grpc-Message", "")
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	t.Cleanup(target.Close)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, target.URL+"/hello.HelloService/SayHello", bytes.NewReader(grpcFrame([]byte{0x0a, 0x01, 'x'})))
	req.Header.Set("Content-Type", "application/grpc+proto")
	req.Header.Set("Te", "trailers")

	res, err := stack.ClientH2().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if res.Proto != "HTTP/2.0" || len(body) != 5+len(reply) || res.Trailer.Get("Grpc-Status") != "0" {
		t.Fatalf("client saw %s, %d bytes, trailers %v", res.Proto, len(body), res.Trailer)
	}

	e := waitForResponse(t, stack)
	if e.Response.Trailers.Get("grpc-status") != "0" || len(e.Response.Body) != 5+len(reply) {
		t.Fatalf("logged %+v", e.Response)
	}

	ok, err := filter.Match(mustParseFilter(t, `res.type = grpc AND res.grpcStatus = 0 AND req.grpcService = hello.HelloService AND req.grpcMethod = SayHello AND res.trailer.grpc-status exists`), reqlog.Subject(e))
	if err != nil || !ok {
		t.Fatalf("gRPC filter keys: %v %v", ok, err)
	}

	dec, err := http.Get(stack.Console.URL + "/api/request-logs/" + e.ID + "/grpc/response")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dec.Body.Close() }()

	var out struct {
		Kind    string `json:"kind"`
		Service string `json:"service"`
		Method  string `json:"method"`
		Status  string `json:"status"`
		Frames  []struct {
			Fields []struct {
				Number int    `json:"number"`
				Value  string `json:"value"`
			} `json:"fields"`
		} `json:"frames"`
	}

	if err := json.UnmarshalRead(dec.Body, &out); err != nil {
		t.Fatal(err)
	}

	if out.Kind != "grpc" || out.Service != "hello.HelloService" || out.Method != "SayHello" || out.Status != "0" {
		t.Fatalf("decoded head: %+v", out)
	}

	if len(out.Frames) != 1 || len(out.Frames[0].Fields) != 2 || out.Frames[0].Fields[0].Value != "150" || out.Frames[0].Fields[1].Value != `"hello"` {
		t.Fatalf("decoded frames: %+v", out.Frames)
	}

	plain, err := http.Get(stack.Console.URL + "/api/request-logs/" + e.ID + "/grpc/nowhere")
	if err != nil {
		t.Fatal(err)
	}

	_ = plain.Body.Close()

	if plain.StatusCode != http.StatusNotFound {
		t.Fatalf("bad side answered %d", plain.StatusCode)
	}
}

func mustParseFilter(t *testing.T, q string) filter.Expr {
	t.Helper()

	e, err := filter.Parse(q)
	if err != nil {
		t.Fatal(err)
	}

	return e
}
