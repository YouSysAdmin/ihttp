package proxy_test

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/wsframe"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

// protoMessage is a protobuf message by hand: field 1 the string s,
// field 2 the varint n, field 3 a nested message holding field 1 as the
// string inner.
func protoMessage(s string, n byte, inner string) []byte {
	nested := append([]byte{1<<3 | 2, byte(len(inner))}, inner...)

	out := append([]byte{1<<3 | 2, byte(len(s))}, s...)
	out = append(out, 2<<3, n) // Wire type 0, the varint.
	out = append(out, 3<<3|2, byte(len(nested)))

	return append(out, nested...)
}

// decoded is the shape both protobuf endpoints answer with.
type decoded struct {
	Kind   string `json:"kind"`
	Frames []struct {
		Length int    `json:"length"`
		Err    string `json:"error"`
		Fields []struct {
			Number int    `json:"number"`
			Wire   string `json:"wire"`
			Value  string `json:"value"`
			Nested []struct {
				Number int    `json:"number"`
				Value  string `json:"value"`
			} `json:"nested"`
		} `json:"fields"`
	} `json:"frames"`
}

func decodeAt(t *testing.T, url string) decoded {
	t.Helper()

	res, err := http.Get(url) //nolint:noctx // Test only.
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("%s answered %d: %s", url, res.StatusCode, body)
	}

	var out decoded
	if err := json.UnmarshalRead(res.Body, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

// A protobuf body over plain HTTP is the same wire format gRPC carries,
// without the framing - so it reads field by field like a gRPC frame
// rather than as a hex dump.
func TestBareProtobufBodyIsDecoded(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "protobuf")

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(protoMessage("reply", 9, "two"))
	}))
	t.Cleanup(target.Close)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, target.URL+"/api",
		bytes.NewReader(protoMessage("Ada", 7, "one")))
	req.Header.Set("Content-Type", "application/x-protobuf")

	res, err := stack.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	entry := waitForResponse(t, stack)

	for _, side := range []struct{ name, want string }{
		{name: "request", want: `"Ada"`},
		{name: "response", want: `"reply"`},
	} {
		out := decodeAt(t, stack.Console.URL+"/api/request-logs/"+entry.ID+"/grpc/"+side.name)

		// Not "grpc": there is no service, no method and no framing, and
		// saying grpc would invite a schema lookup that cannot work.
		if out.Kind != "protobuf" {
			t.Errorf("%s kind %q, want protobuf", side.name, out.Kind)
		}

		if len(out.Frames) != 1 {
			t.Fatalf("%s: %d frames, want the one message", side.name, len(out.Frames))
		}

		f := out.Frames[0]
		if len(f.Fields) != 3 || f.Fields[0].Value != side.want || f.Fields[1].Value != "7" && f.Fields[1].Value != "9" {
			t.Fatalf("%s fields: %+v", side.name, f.Fields)
		}

		if len(f.Fields[2].Nested) != 1 {
			t.Errorf("%s: the nested message was not read: %+v", side.name, f.Fields[2])
		}
	}
}

// A binary WebSocket frame carries no content type, so nothing can
// decide it is protobuf - it is asked for, and the answer is honest
// either way.
func TestWebSocketFrameReadAsProtobuf(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "ws-protobuf")

	target := echoUpstream(t, nil)
	conn, br := wsClient(t, stack, target)

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Binary}, protoMessage("Ada", 7, "one"))

	if op, _ := recv(t, br); op != wsframe.Binary {
		t.Fatalf("echo came back as %s", op)
	}

	send(t, conn, wsframe.Header{Fin: true, Opcode: wsframe.Binary}, []byte{0xff, 0xff, 0xff})

	if op, _ := recv(t, br); op != wsframe.Binary {
		t.Fatalf("second echo came back as %s", op)
	}

	_ = conn.Close()

	var id string

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(t.Context(), reqlogList())
		if len(entries) != 1 || entries[0].Messages < 4 {
			return false
		}

		id = entries[0].ID

		return true
	}, "the four frames are recorded")

	out := decodeAt(t, stack.Console.URL+"/api/request-logs/"+id+"/messages/1/protobuf")
	if out.Kind != "protobuf" || len(out.Frames) != 1 || len(out.Frames[0].Fields) != 3 {
		t.Fatalf("frame 1: %+v", out)
	}

	if out.Frames[0].Fields[0].Value != `"Ada"` {
		t.Errorf("fields: %+v", out.Frames[0].Fields)
	}

	// Bytes that are not protobuf say so rather than inventing fields.
	// 0xff 0xff 0xff is field number 536870911 with wire type 7, which
	// is not a wire type.
	junk := decodeAt(t, stack.Console.URL+"/api/request-logs/"+id+"/messages/3/protobuf")
	if junk.Frames[0].Err == "" && len(junk.Frames[0].Fields) > 0 {
		t.Errorf("junk decoded as %+v", junk.Frames[0])
	}
}
