package protoschema_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/grpcmsg"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/testutil"
)

const helloProto = `syntax = "proto3";
package hello;

import "common.proto";

message HelloRequest { string greeting = 1; }

message HelloReply {
  int32 code = 1;
  string reply = 2;
  common.Meta meta = 3;
}

service HelloService {
  rpc SayHello(HelloRequest) returns (HelloReply);
}
`

const commonProto = `syntax = "proto3";
package common;

message Meta { int32 n = 1; }
`

func TestUploadCompileAndResolve(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "schemas")
	ctx := t.Context()

	// The import must be there first.
	if _, err := stack.Schemas.Upload(ctx, "hello.proto", []byte(helloProto)); err == nil {
		t.Fatal("a file with a missing import compiled")
	}

	common, err := stack.Schemas.Upload(ctx, "common.proto", []byte(commonProto))
	if err != nil {
		t.Fatal(err)
	}

	hello, err := stack.Schemas.Upload(ctx, "hello.proto", []byte(helloProto))
	if err != nil {
		t.Fatal(err)
	}

	if hello.Kind != "proto" || len(hello.Services) != 1 || hello.Services[0] != "hello.HelloService" || len(hello.Files) != 2 {
		t.Fatalf("hello summary: %+v", hello)
	}

	if len(common.Services) != 0 {
		t.Fatalf("common summary: %+v", common)
	}

	in, out, ok := stack.Schemas.Method(ctx, "/hello.HelloService/SayHello")
	if !ok || in.FullName() != "hello.HelloRequest" || out.FullName() != "hello.HelloReply" {
		t.Fatalf("resolve: %v %v %v", ok, in, out)
	}

	if _, _, ok := stack.Schemas.Method(ctx, "/hello.HelloService/Nope"); ok {
		t.Fatal("an unknown method resolved")
	}

	// A reply read with the schema.
	reply := []byte{0x08, 0x96, 0x01, 0x12, 0x05, 'h', 'e', 'l', 'l', 'o', 0x1a, 0x02, 0x08, 0x07}

	text, err := grpcmsg.DecodeWith(reply, out)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"code": 150`, `"reply": "hello"`, `"n": 7`} {
		if !strings.Contains(text, want) {
			t.Fatalf("decoded %s, missing %s", text, want)
		}
	}

	// Uploading the same name again replaces it, deleting drops it.
	list, _ := stack.Schemas.List(ctx)
	if len(list) != 2 {
		t.Fatalf("list: %d", len(list))
	}

	if _, err := stack.Schemas.Upload(ctx, "hello.proto", []byte(helloProto)); err != nil {
		t.Fatal(err)
	}

	list, _ = stack.Schemas.List(ctx)
	if len(list) != 2 {
		t.Fatalf("list after re-upload: %d", len(list))
	}

	// The re-upload replaced hello under a new id, the fresh list has it.
	var helloID string
	for _, sc := range list {
		if sc.Name == "hello.proto" {
			helloID = sc.ID
		}
	}

	if err := stack.Schemas.Delete(ctx, helloID); err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Schemas.Upload(ctx, "junk.pb", []byte("not a descriptor set")); err == nil {
		t.Fatal("junk was accepted as a descriptor set")
	}
}

func TestLoggedCallReadsWithTheSchema(t *testing.T) {
	stack := testutil.New(t)
	stack.OpenProject(t, "grpc-schema")
	ctx := t.Context()

	if _, err := stack.Schemas.Upload(ctx, "common.proto", []byte(commonProto)); err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Schemas.Upload(ctx, "hello.proto", []byte(helloProto)); err != nil {
		t.Fatal(err)
	}

	reply := []byte{0x08, 0x96, 0x01, 0x12, 0x05, 'h', 'e', 'l', 'l', 'o'}

	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status")
		_, _ = w.Write(frame(reply))
		w.Header().Set("Grpc-Status", "0")
	}))
	target.EnableHTTP2 = true
	target.StartTLS()
	t.Cleanup(target.Close)

	req, _ := http.NewRequest(http.MethodPost, target.URL+"/hello.HelloService/SayHello", bytes.NewReader(frame([]byte{0x0a, 0x02, 'h', 'i'})))
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("Te", "trailers")

	res, err := stack.ClientH2().Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()

	var id string

	testutil.Eventually(t, 3*time.Second, func() bool {
		entries, _, _ := stack.ReqLogs.List(ctx, reqlog.ListParams{Limit: 5})
		if len(entries) == 1 && entries[0].StatusCode != 0 {
			id = entries[0].ID
		}

		return id != ""
	}, "call logged")

	for side, want := range map[string]string{"request": `"greeting": "hi"`, "response": `"reply": "hello"`} {
		dec, err := http.Get(stack.Console.URL + "/api/request-logs/" + id + "/grpc/" + side)
		if err != nil {
			t.Fatal(err)
		}

		var out struct {
			HasSchema   bool   `json:"has_schema"`
			MessageType string `json:"message_type"`
			Frames      []struct {
				JSON string `json:"json"`
			} `json:"frames"`
		}

		err = json.UnmarshalRead(dec.Body, &out)
		_ = dec.Body.Close()

		if err != nil || !out.HasSchema || len(out.Frames) != 1 || !strings.Contains(out.Frames[0].JSON, want) {
			t.Fatalf("%s: %+v %v", side, out, err)
		}
	}

	// The route takes a file and refuses junk.
	up, err := http.Post(stack.Console.URL+"/api/project/schemas?name=extra.proto", "application/octet-stream", strings.NewReader(`syntax = "proto3"; package x; message X {}`))
	if err != nil {
		t.Fatal(err)
	}

	_ = up.Body.Close()

	if up.StatusCode != http.StatusCreated {
		t.Fatalf("upload answered %d", up.StatusCode)
	}

	bad, err := http.Post(stack.Console.URL+"/api/project/schemas?name=bad.proto", "application/octet-stream", strings.NewReader(`message {`))
	if err != nil {
		t.Fatal(err)
	}

	_ = bad.Body.Close()

	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad file answered %d", bad.StatusCode)
	}
}

func frame(msg []byte) []byte {
	out := []byte{0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:5], uint32(len(msg)))

	return append(out, msg...)
}
