package wsframe

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

// The frames of RFC 6455 section 5.7.
var (
	helloUnmasked = []byte{0x81, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}
	helloMasked   = []byte{0x81, 0x85, 0x37, 0xfa, 0x21, 0x3d, 0x7f, 0x9f, 0x4d, 0x51, 0x58}
	fragmentOne   = []byte{0x01, 0x03, 0x48, 0x65, 0x6c}
	fragmentTwo   = []byte{0x80, 0x02, 0x6c, 0x6f}
	pingHello     = []byte{0x89, 0x05, 0x48, 0x65, 0x6c, 0x6c, 0x6f}
)

func read(t *testing.T, frame []byte) (Header, []byte) {
	t.Helper()

	r := bufio.NewReader(bytes.NewReader(frame))

	h, err := ReadHeader(r)
	if err != nil {
		t.Fatalf("ReadHeader(%x): %v", frame, err)
	}

	payload := make([]byte, h.Length)
	if _, err := io.ReadFull(r, payload); err != nil {
		t.Fatal(err)
	}

	if h.Masked {
		Unmask(h.Mask, 0, payload)
	}

	return h, payload
}

func TestReadHeaderRFCExamples(t *testing.T) {
	h, p := read(t, helloUnmasked)
	if !h.Fin || h.Opcode != Text || h.Masked || h.Length != 5 || string(p) != "Hello" || !bytes.Equal(h.Raw, helloUnmasked[:2]) {
		t.Fatalf("unmasked: %+v %q", h, p)
	}

	h, p = read(t, helloMasked)
	if !h.Masked || h.Mask != [4]byte{0x37, 0xfa, 0x21, 0x3d} || string(p) != "Hello" || !bytes.Equal(h.Raw, helloMasked[:6]) {
		t.Fatalf("masked: %+v %q", h, p)
	}

	h, p = read(t, fragmentOne)
	if h.Fin || h.Opcode != Text || string(p) != "Hel" {
		t.Fatalf("fragment one: %+v %q", h, p)
	}

	h, p = read(t, fragmentTwo)
	if !h.Fin || h.Opcode != Continuation || string(p) != "lo" {
		t.Fatalf("fragment two: %+v %q", h, p)
	}

	h, p = read(t, pingHello)
	if h.Opcode != Ping || !h.Opcode.IsControl() || string(p) != "Hello" {
		t.Fatalf("ping: %+v %q", h, p)
	}
}

func TestLongLengths(t *testing.T) {
	medium := append([]byte{0x82, 0x7e, 0x01, 0x00}, bytes.Repeat([]byte{0xab}, 256)...)
	if h, p := read(t, medium); h.Length != 256 || len(p) != 256 || len(h.Raw) != 4 {
		t.Fatalf("16-bit length: %+v", h)
	}

	large := append([]byte{0x82, 0x7f, 0, 0, 0, 0, 0, 1, 0, 0}, bytes.Repeat([]byte{0xcd}, 65536)...)
	if h, p := read(t, large); h.Length != 65536 || len(p) != 65536 || len(h.Raw) != 10 {
		t.Fatalf("64-bit length: %+v", h)
	}
}

func TestProtocolErrors(t *testing.T) {
	bad := map[string][]byte{
		"reserved opcode":    {0x83, 0x00},
		"fragmented control": {0x09, 0x00},
		"oversized control":  {0x89, 0x7e, 0x00, 0x7e},
		"length top bit":     {0x82, 0x7f, 0x80, 0, 0, 0, 0, 0, 0, 0},
	}

	for name, frame := range bad {
		if _, err := ReadHeader(bufio.NewReader(bytes.NewReader(frame))); !errors.Is(err, ErrProtocol) {
			t.Errorf("%s: %v", name, err)
		}
	}

	if _, err := ReadHeader(bufio.NewReader(bytes.NewReader([]byte{0x81}))); err == nil {
		t.Error("a cut header was read")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	for _, frame := range [][]byte{helloUnmasked, helloMasked, fragmentOne, pingHello} {
		h, _ := read(t, frame)
		if got := h.Encode(); !bytes.Equal(got, h.Raw) {
			t.Errorf("Encode(%x) = %x, want %x", frame, got, h.Raw)
		}
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, Header{Fin: true, Opcode: Text, Masked: true}, []byte("Hello")); err != nil {
		t.Fatal(err)
	}

	h, p := read(t, buf.Bytes())
	if !h.Masked || h.Mask == [4]byte{} || string(p) != "Hello" {
		t.Fatalf("WriteFrame masked: %+v %q", h, p)
	}

	buf.Reset()
	_ = WriteFrame(&buf, Header{Fin: true, Opcode: Binary}, bytes.Repeat([]byte{1}, 70000))

	if h, p := read(t, buf.Bytes()); h.Length != 70000 || len(p) != 70000 {
		t.Fatalf("WriteFrame long: %+v", h)
	}
}

func TestUnmaskWithOffset(t *testing.T) {
	mask := [4]byte{0x37, 0xfa, 0x21, 0x3d}
	whole := []byte{0x7f, 0x9f, 0x4d, 0x51, 0x58}
	Unmask(mask, 0, whole)

	parts := []byte{0x7f, 0x9f, 0x4d, 0x51, 0x58}
	Unmask(mask, 0, parts[:2])
	Unmask(mask, 2, parts[2:])

	if !bytes.Equal(whole, parts) || string(whole) != "Hello" {
		t.Fatalf("offset unmask: %q vs %q", whole, parts)
	}
}

func TestParseCloseAndAcceptKey(t *testing.T) {
	if code, reason := ParseClose([]byte{0x03, 0xe8, 'b', 'y', 'e'}); code != 1000 || reason != "bye" {
		t.Fatalf("close: %d %q", code, reason)
	}

	if code, _ := ParseClose(nil); code != NoStatus {
		t.Fatalf("empty close: %d", code)
	}

	if got := AcceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("AcceptKey: %s", got)
	}
}

func TestAssembler(t *testing.T) {
	a := NewAssembler(4)

	if err := a.Begin(Header{Opcode: Continuation}); !errors.Is(err, ErrProtocol) {
		t.Fatal("a continuation opened a message")
	}

	if err := a.Begin(Header{Opcode: Text, RSV1: true}); err != nil {
		t.Fatal(err)
	}

	a.Append([]byte("Hel"), 3)

	if err := a.Begin(Header{Opcode: Binary}); !errors.Is(err, ErrProtocol) {
		t.Fatal("a second message opened over an open one")
	}

	if err := a.Begin(Header{Opcode: Continuation}); err != nil {
		t.Fatal(err)
	}

	a.Append([]byte("lo"), 2)

	m := a.Finish()
	if m.Opcode != Text || string(m.Payload) != "Hell" || !m.Truncated || m.Size != 5 || m.Frames != 2 || !m.Compressed {
		t.Fatalf("assembled %+v", m)
	}

	if err := a.Begin(Header{Opcode: Binary}); err != nil {
		t.Fatal(err)
	}

	a.Append([]byte{1, 2}, 2)

	if m := a.Finish(); m.Truncated || m.Size != 2 || m.Frames != 1 {
		t.Fatalf("whole message: %+v", m)
	}
}

func TestOpcodeNames(t *testing.T) {
	for op, want := range map[Opcode]string{Text: "text", Binary: "binary", Close: "close", Ping: "ping", Pong: "pong", Continuation: "continuation", 0x3: "opcode-3"} {
		if op.String() != want {
			t.Errorf("%d: %s", op, op.String())
		}
	}
}
