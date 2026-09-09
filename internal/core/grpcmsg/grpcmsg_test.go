package grpcmsg

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"testing"
)

// frame prefixes a message the gRPC way.
func frame(flags byte, msg []byte) []byte {
	out := []byte{flags, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(out[1:5], uint32(len(msg)))

	return append(out, msg...)
}

// A message with field 1 = varint 150, field 2 = "testing", field 3 = a
// nested message with field 1 = varint 1.
var sample = []byte{0x08, 0x96, 0x01, 0x12, 0x07, 't', 'e', 's', 't', 'i', 'n', 'g', 0x1a, 0x02, 0x08, 0x01}

func TestKindAndMethod(t *testing.T) {
	cases := map[string]Kind{
		"application/grpc":                KindGRPC,
		"application/grpc+proto":          KindGRPC,
		"application/grpc-web+proto":      KindWeb,
		"application/grpc-web-text+proto": KindWebText,
		"application/json":                KindNone,
	}

	for ct, want := range cases {
		if got := KindOf(ct); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", ct, got, want)
		}
	}

	if s, m := Method("/hello.HelloService/SayHello"); s != "hello.HelloService" || m != "SayHello" {
		t.Errorf("Method: %q %q", s, m)
	}
}

func TestSplitAndDecode(t *testing.T) {
	body := append(frame(0, sample), frame(0, []byte{0x08, 0x02})...)

	frames, err := Split(body, KindGRPC, "")
	if err != nil || len(frames) != 2 {
		t.Fatalf("Split: %d %v", len(frames), err)
	}

	f := frames[0]
	if f.Length != len(sample) || len(f.Fields) != 3 {
		t.Fatalf("frame 0: %+v", f)
	}

	if f.Fields[0].Wire != "varint" || f.Fields[0].Value != "150" {
		t.Errorf("field 1: %+v", f.Fields[0])
	}

	if f.Fields[1].Wire != "bytes" || f.Fields[1].Value != `"testing"` || f.Fields[1].Nested != nil {
		t.Errorf("field 2: %+v", f.Fields[1])
	}

	if len(f.Fields[2].Nested) != 1 || f.Fields[2].Nested[0].Number != 1 || f.Fields[2].Nested[0].Value != "1" {
		t.Errorf("field 3: %+v", f.Fields[2])
	}

	if frames[1].Index != 1 || frames[1].Fields[0].Value != "2" {
		t.Errorf("frame 1: %+v", frames[1])
	}
}

func TestSplitGzipAndTrailer(t *testing.T) {
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	_, _ = zw.Write(sample)
	_ = zw.Close()

	trailer := []byte("grpc-status: 0\r\ngrpc-message: ok\r\n")
	body := append(frame(0x01, zipped.Bytes()), frame(0x80, trailer)...)

	frames, err := Split(body, KindWeb, "gzip")
	if err != nil || len(frames) != 2 {
		t.Fatalf("Split: %d %v", len(frames), err)
	}

	if !frames[0].Compressed || len(frames[0].Fields) != 3 || frames[0].Err != "" {
		t.Fatalf("gzip frame: %+v", frames[0])
	}

	if !frames[1].Trailer || frames[1].Trailers.Get("grpc-status") != "0" {
		t.Fatalf("trailer frame: %+v", frames[1])
	}

	if code, msg := Status(nil, nil, frames); code != "0" || msg != "ok" {
		t.Fatalf("Status from the trailer frame: %q %q", code, msg)
	}

	if code, _ := Status(http.Header{"Grpc-Status": {"5"}}, http.Header{"Grpc-Status": {"13"}, "Grpc-Message": {"boom%20here"}}, nil); code != "13" {
		t.Fatalf("Status prefers the trailers: %q", code)
	}

	if _, msg := Status(nil, http.Header{"Grpc-Status": {"13"}, "Grpc-Message": {"boom%20here"}}, nil); msg != "boom here" {
		t.Fatalf("Status message: %q", msg)
	}

	undeclared, _ := Split(frame(0x01, zipped.Bytes()), KindGRPC, "")
	if undeclared[0].Err == "" {
		t.Fatal("a compressed frame without grpc-encoding was decoded")
	}
}

func TestSplitWebText(t *testing.T) {
	one := frame(0, sample)
	two := frame(0x80, []byte("grpc-status: 0\r\n"))

	// One blob, then the same as two padded chunks back to back.
	whole := base64.StdEncoding.EncodeToString(append(append([]byte{}, one...), two...))
	chunks := base64.StdEncoding.EncodeToString(one) + base64.StdEncoding.EncodeToString(two)

	for name, body := range map[string]string{"whole": whole, "chunks": chunks} {
		frames, err := Split([]byte(body), KindWebText, "")
		if err != nil || len(frames) != 2 || !frames[1].Trailer {
			t.Errorf("%s: %d frames, %v", name, len(frames), err)
		}
	}
}

func TestSplitRefusesCutFrames(t *testing.T) {
	if _, err := Split([]byte{0, 0, 0, 0, 9, 1}, KindGRPC, ""); !errors.Is(err, ErrFraming) {
		t.Fatalf("short frame: %v", err)
	}

	if _, err := Split([]byte{0, 0}, KindGRPC, ""); !errors.Is(err, ErrFraming) {
		t.Fatalf("short prefix: %v", err)
	}

	if _, err := Decode([]byte{0x08}); err == nil {
		t.Fatal("a cut varint decoded")
	}
}

func TestValuesRead(t *testing.T) {
	fields, err := Decode([]byte{
		0x08, 0x01, // 1: varint 1
		0x11, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, // 2: fixed64 1.0
		0x1d, 0, 0, 0x80, 0x3f, // 3: fixed32 1.0
		0x22, 0x02, 0xff, 0xfe, // 4: bytes not text
	})
	if err != nil || len(fields) != 4 {
		t.Fatalf("Decode: %v %v", fields, err)
	}

	if fields[0].Value != "1" || fields[1].Value != "4607182418800017408 (0x3ff0000000000000, 1)" ||
		fields[2].Value != "1065353216 (0x3f800000, 1)" || fields[3].Value != "fffe" {
		t.Fatalf("values: %+v", fields)
	}
}
