// Package wsframe reads and writes WebSocket frames as RFC 6455 lays them
// out. It is what the proxy uses to see a message go by without owning
// either end of the connection: a header is read, its raw bytes are
// forwarded as they were, and the payload is copied through. Nothing is
// re-encoded unless the proxy itself speaks.
package wsframe

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // The handshake is defined on SHA-1.
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Opcode is the frame type.
type Opcode uint8

// The opcodes RFC 6455 defines. The rest are reserved.
const (
	Continuation Opcode = 0x0
	Text         Opcode = 0x1
	Binary       Opcode = 0x2
	Close        Opcode = 0x8
	Ping         Opcode = 0x9
	Pong         Opcode = 0xA
)

// String names the opcode the way the console shows it.
func (o Opcode) String() string {
	switch o {
	case Continuation:
		return "continuation"
	case Text:
		return "text"
	case Binary:
		return "binary"
	case Close:
		return "close"
	case Ping:
		return "ping"
	case Pong:
		return "pong"
	default:
		return fmt.Sprintf("opcode-%d", uint8(o))
	}
}

// IsControl reports a close, ping or pong: a frame that may sit between
// the fragments of a message and never fragments itself.
func (o Opcode) IsControl() bool {
	return o >= Close
}

func (o Opcode) known() bool {
	switch o {
	case Continuation, Text, Binary, Close, Ping, Pong:
		return true
	default:
		return false
	}
}

// ErrProtocol is a frame the RFC does not allow.
var ErrProtocol = errors.New("wsframe: protocol error")

// MaxControlPayload bounds a control frame's payload.
const MaxControlPayload = 125

// Header is one frame's framing. Raw holds the header bytes as they were
// read, so a relay forwards them without encoding them again.
type Header struct {
	Fin              bool
	RSV1, RSV2, RSV3 bool
	Opcode           Opcode
	Masked           bool
	Length           int64
	Mask             [4]byte
	Raw              []byte
}

// ReadHeader reads one frame header, leaving the payload in r. A reserved
// opcode, a fragmented or oversized control frame, or a 64-bit length with
// its top bit set is ErrProtocol.
func ReadHeader(r *bufio.Reader) (Header, error) {
	var h Header

	raw := make([]byte, 2, 14)
	if _, err := io.ReadFull(r, raw); err != nil {
		return h, err
	}

	h.Fin = raw[0]&0x80 != 0
	h.RSV1 = raw[0]&0x40 != 0
	h.RSV2 = raw[0]&0x20 != 0
	h.RSV3 = raw[0]&0x10 != 0
	h.Opcode = Opcode(raw[0] & 0x0f)
	h.Masked = raw[1]&0x80 != 0

	if !h.Opcode.known() {
		return h, fmt.Errorf("%w: reserved opcode %d", ErrProtocol, uint8(h.Opcode))
	}

	switch n := raw[1] & 0x7f; n {
	case 126:
		ext := raw[2:4]
		raw = raw[:4]

		if _, err := io.ReadFull(r, ext); err != nil {
			return h, err
		}

		h.Length = int64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := raw[2:10]
		raw = raw[:10]

		if _, err := io.ReadFull(r, ext); err != nil {
			return h, err
		}

		v := binary.BigEndian.Uint64(ext)
		if v>>63 != 0 {
			return h, fmt.Errorf("%w: length with the top bit set", ErrProtocol)
		}

		h.Length = int64(v)
	default:
		h.Length = int64(n)
	}

	if h.Masked {
		start := len(raw)
		raw = raw[:start+4]

		if _, err := io.ReadFull(r, raw[start:]); err != nil {
			return h, err
		}

		copy(h.Mask[:], raw[start:])
	}

	if h.Opcode.IsControl() && (!h.Fin || h.Length > MaxControlPayload) {
		return h, fmt.Errorf("%w: control frame fragmented or over %d bytes", ErrProtocol, MaxControlPayload)
	}

	h.Raw = raw

	return h, nil
}

// Encode renders the header bytes for a frame the proxy writes itself,
// with the shortest length encoding.
func (h Header) Encode() []byte {
	out := make([]byte, 2, 14)

	if h.Fin {
		out[0] |= 0x80
	}

	if h.RSV1 {
		out[0] |= 0x40
	}

	if h.RSV2 {
		out[0] |= 0x20
	}

	if h.RSV3 {
		out[0] |= 0x10
	}

	out[0] |= byte(h.Opcode) & 0x0f

	switch {
	case h.Length <= 125:
		out[1] = byte(h.Length)
	case h.Length <= 0xffff:
		out[1] = 126
		out = binary.BigEndian.AppendUint16(out, uint16(h.Length))
	default:
		out[1] = 127
		out = binary.BigEndian.AppendUint64(out, uint64(h.Length))
	}

	if h.Masked {
		out[1] |= 0x80
		out = append(out, h.Mask[:]...)
	}

	return out
}

// Unmask XORs b in place with the mask, where b starts offset bytes into
// the payload. A relay that keeps only a prefix of a long payload unmasks
// that prefix and leaves the rest to the peer.
func Unmask(mask [4]byte, offset int, b []byte) {
	var m [4]byte
	for i := range m {
		m[i] = mask[(offset+i)&3]
	}

	for i := range b {
		b[i] ^= m[i&3]
	}
}

// WriteFrame writes one whole frame. Length is taken from the payload.
// When h.Masked is set and the mask is zero a fresh one is drawn, since a
// frame the proxy sends toward a server must be masked and unpredictable.
func WriteFrame(w io.Writer, h Header, payload []byte) error {
	h.Length = int64(len(payload))

	if h.Masked && h.Mask == [4]byte{} {
		h.Mask = NewMask()
	}

	buf := h.Encode()
	buf = append(buf, payload...)

	if h.Masked {
		Unmask(h.Mask, 0, buf[len(buf)-len(payload):])
	}

	_, err := w.Write(buf)

	return err
}

// NewMask draws a masking key.
func NewMask() [4]byte {
	var m [4]byte
	_, _ = rand.Read(m[:])

	return m
}

// NoStatus is the code a close frame without a body stands for.
const NoStatus = 1005

// ParseClose reads the status code and reason out of a close payload.
func ParseClose(payload []byte) (code int, reason string) {
	if len(payload) < 2 {
		return NoStatus, ""
	}

	return int(binary.BigEndian.Uint16(payload)), string(payload[2:])
}

// AcceptKey answers a Sec-WebSocket-Key the way a server must.
func AcceptKey(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec // Protocol constant.

	return base64.StdEncoding.EncodeToString(sum[:])
}

// Message is one WebSocket message: a text or binary payload put back
// together from its frames, or a control frame on its own. Payload holds
// at most the limit the Assembler was given, Size the whole length.
type Message struct {
	Opcode     Opcode
	Payload    []byte
	Truncated  bool
	Size       int64
	Frames     int
	Compressed bool
}

// Assembler puts fragments back together, keeping a bounded prefix of
// the payload. Control frames never pass through it.
type Assembler struct {
	limit int64
	open  bool
	msg   Message
}

// NewAssembler builds an Assembler that keeps limit bytes per message.
func NewAssembler(limit int64) *Assembler {
	return &Assembler{limit: limit}
}

// Begin starts or continues a message with h. A continuation with nothing
// open, or a new message while one is open, is ErrProtocol.
func (a *Assembler) Begin(h Header) error {
	if h.Opcode == Continuation {
		if !a.open {
			return fmt.Errorf("%w: continuation with no message open", ErrProtocol)
		}

		return nil
	}

	if a.open {
		return fmt.Errorf("%w: a new message while one is open", ErrProtocol)
	}

	a.open = true
	a.msg = Message{Opcode: h.Opcode, Compressed: h.RSV1}

	return nil
}

// Append adds one fragment: prefix is what was kept of its payload, n the
// payload's whole length.
func (a *Assembler) Append(prefix []byte, n int64) {
	room := a.limit - int64(len(a.msg.Payload))
	take := min(room, int64(len(prefix)))

	if take > 0 {
		a.msg.Payload = append(a.msg.Payload, prefix[:take]...)
	}

	if take < n {
		a.msg.Truncated = true
	}

	a.msg.Size += n
	a.msg.Frames++
}

// Finish closes the message and returns it.
func (a *Assembler) Finish() Message {
	m := a.msg
	a.open = false
	a.msg = Message{}

	return m
}
