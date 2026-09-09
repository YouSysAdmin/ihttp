// Package grpcmsg reads gRPC bodies without a schema: the length-prefixed
// frames a body is made of, and the protobuf wire format inside each,
// shown as field numbers, wire types and values. With no .proto to hand
// that is what there is to see, and it is enough to tell one call from
// another and spot a value.
package grpcmsg

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// Kind is which framing a body uses.
type Kind string

// The kinds. None is not gRPC at all.
//
// KindProtobuf is NOT returned by KindOf, on purpose: a bare protobuf
// body over plain HTTP is not gRPC, has no service or method, and must
// not start answering req.grpcService or be buffered as a gRPC call
// would be. It is what a caller sets when it has decided to read a body
// as one message rather than as a stream of frames.
const (
	KindNone     Kind = ""
	KindGRPC     Kind = "grpc"
	KindWeb      Kind = "grpc-web"
	KindWebText  Kind = "grpc-web-text"
	KindProtobuf Kind = "protobuf"
)

// KindOf reads the kind off a Content-Type.
func KindOf(contentType string) Kind {
	mt := httpmsg.MediaType(contentType)

	switch {
	case strings.HasPrefix(mt, "application/grpc-web-text"):
		return KindWebText
	case strings.HasPrefix(mt, "application/grpc-web"):
		return KindWeb
	case strings.HasPrefix(mt, "application/grpc"):
		return KindGRPC
	default:
		return KindNone
	}
}

// IsProtobuf says a Content-Type names a bare protobuf message: the
// same wire format gRPC carries, without the framing, which is how a
// plain HTTP API that speaks protobuf sends one.
//
// Deliberately not part of KindOf - see KindProtobuf.
func IsProtobuf(contentType string) bool {
	mt := httpmsg.MediaType(contentType)

	switch mt {
	case "application/x-protobuf", "application/protobuf", "application/vnd.google.protobuf",
		"application/octet-stream+protobuf", "application/x-google-protobuf":
		return true
	}

	// A vendor type may name its own schema and end in +protobuf, the
	// way a JSON one ends in +json.
	return strings.HasSuffix(mt, "+protobuf")
}

// Message reads a whole body as ONE protobuf message, for a body that
// carries a message rather than a stream of framed ones. The result is
// a Frame so that everything which already shows a gRPC frame shows
// this too.
func Message(body []byte) Frame {
	f := Frame{Length: len(body), Raw: body}

	fields, err := Decode(body)
	if err != nil {
		f.Err = err.Error()

		return f
	}

	f.Fields = fields

	return f
}

// Method splits a gRPC path, /package.Service/Method, into its two names.
func Method(path string) (service, method string) {
	path = strings.TrimPrefix(path, "/")
	service, method, _ = strings.Cut(path, "/")

	return service, method
}

// Limits on what one body is decoded into.
const (
	// MaxFrames bounds how many frames Split returns.
	MaxFrames = 200

	// MaxDepth bounds how deep nested messages are guessed.
	MaxDepth = 8

	// MaxValueBytes bounds how much of a bytes field is rendered.
	MaxValueBytes = 64
)

// Frame is one length-prefixed message. Raw is the message bytes after
// any gzip, Fields the wire-format dump of them, and Trailers the header
// block of a grpc-web trailer frame. Err says why a frame could not be
// read further.
type Frame struct {
	Index      int             `json:"index"`
	Compressed bool            `json:"compressed"`
	Trailer    bool            `json:"trailer"`
	Length     int             `json:"length"`
	Raw        []byte          `json:"raw"`
	Fields     []Field         `json:"fields,omitempty"`
	Trailers   httpmsg.Headers `json:"trailers,omitempty"`
	Err        string          `json:"error,omitempty"`

	// JSON is the message read with its schema, when one is known.
	JSON string `json:"json,omitempty"`
}

// Field is one protobuf field as the wire shows it. Nested is set when
// the bytes of a length-delimited field parse cleanly as a message.
type Field struct {
	Number int     `json:"number"`
	Wire   string  `json:"wire"`
	Value  string  `json:"value"`
	Nested []Field `json:"nested,omitempty"`
}

// ErrFraming is a body that is not a sequence of gRPC frames.
var ErrFraming = errors.New("grpcmsg: bad framing")

// Split cuts a body into its frames. encoding is the grpc-encoding
// header, gzip being the one this reads. A grpc-web-text body is base64,
// sometimes in one piece and sometimes frame by frame, so it is decoded
// chunk by chunk. More is reported through a frame count of MaxFrames.
func Split(body []byte, kind Kind, encoding string) ([]Frame, error) {
	if kind == KindWebText {
		var err error
		if body, err = decodeChunks(body); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFraming, err)
		}
	}

	var out []Frame

	for len(body) > 0 && len(out) < MaxFrames {
		if len(body) < 5 {
			return out, fmt.Errorf("%w: %d trailing bytes", ErrFraming, len(body))
		}

		flags := body[0]
		n := int(binary.BigEndian.Uint32(body[1:5]))

		if n > len(body)-5 {
			return out, fmt.Errorf("%w: frame %d says %d bytes, %d remain", ErrFraming, len(out), n, len(body)-5)
		}

		f := Frame{
			Index:      len(out),
			Compressed: flags&0x01 != 0,
			Trailer:    kind != KindGRPC && flags&0x80 != 0,
			Length:     n,
			Raw:        bytes.Clone(body[5 : 5+n]),
		}
		body = body[5+n:]

		if f.Compressed {
			switch strings.ToLower(encoding) {
			case "gzip":
				plain, err := gunzip(f.Raw)
				if err != nil {
					f.Err = "gzip: " + err.Error()
				} else {
					f.Raw = plain
				}
			case "", "identity":
				f.Err = "compressed, but no grpc-encoding was declared"
			default:
				f.Err = "compressed with " + encoding + ", which is not decoded"
			}
		}

		switch {
		case f.Err != "":
		case f.Trailer:
			f.Trailers = parseTrailers(f.Raw)
		default:
			fields, err := Decode(f.Raw)
			if err != nil {
				f.Err = err.Error()
			}

			f.Fields = fields
		}

		out = append(out, f)
	}

	return out, nil
}

// decodeChunks base64-decodes a grpc-web-text body written as one blob or
// as several padded chunks back to back.
func decodeChunks(b []byte) ([]byte, error) {
	b = bytes.TrimSpace(b)

	var out []byte

	for len(b) > 0 {
		end := bytes.IndexByte(b, '=')
		if end < 0 {
			end = len(b)
		} else {
			for end < len(b) && b[end] == '=' {
				end++
			}
		}

		chunk, err := base64.StdEncoding.DecodeString(string(b[:end]))
		if err != nil {
			return nil, err
		}

		out = append(out, chunk...)
		b = b[end:]
	}

	return out, nil
}

// MaxMessage bounds what a compressed message may expand to.
const MaxMessage = 64 << 20

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	out, err := io.ReadAll(io.LimitReader(r, MaxMessage+1))
	if err != nil {
		return nil, err
	}

	if len(out) > MaxMessage {
		return nil, errors.New("grpcmsg: message is over the limit")
	}

	return out, nil
}

// parseTrailers reads the "name: value" lines of a grpc-web trailer frame.
func parseTrailers(b []byte) httpmsg.Headers {
	out := httpmsg.Headers{}

	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}

		name, value, _ := strings.Cut(line, ":")
		out = append(out, httpmsg.Header{Name: strings.TrimSpace(name), Value: strings.TrimSpace(value)})
	}

	return out
}

// Decode dumps a protobuf message without its schema.
func Decode(msg []byte) ([]Field, error) {
	return decode(msg, 0)
}

func decode(msg []byte, depth int) ([]Field, error) {
	var out []Field

	total := len(msg)
	for len(msg) > 0 {
		tag, n := binary.Uvarint(msg)
		if n <= 0 {
			return out, fmt.Errorf("grpcmsg: bad tag at byte %d", total-len(msg))
		}

		msg = msg[n:]

		number := int(tag >> 3)
		wire := int(tag & 7)

		if number == 0 || number > 1<<29-1 {
			return out, fmt.Errorf("grpcmsg: field number %d out of range", number)
		}

		f := Field{Number: number}

		switch wire {
		case 0:
			v, n := binary.Uvarint(msg)
			if n <= 0 {
				return out, errors.New("grpcmsg: cut varint")
			}

			msg = msg[n:]
			f.Wire = "varint"
			f.Value = varintValue(v)
		case 1:
			if len(msg) < 8 {
				return out, errors.New("grpcmsg: cut fixed64")
			}

			v := binary.LittleEndian.Uint64(msg)
			msg = msg[8:]
			f.Wire = "fixed64"
			f.Value = fmt.Sprintf("%d (0x%016x, %g)", v, v, math.Float64frombits(v))
		case 2:
			l, n := binary.Uvarint(msg)
			if n <= 0 || l > uint64(len(msg)-n) {
				return out, errors.New("grpcmsg: cut length-delimited field")
			}

			data := msg[n : n+int(l)]
			msg = msg[n+int(l):]
			f.Wire = "bytes"
			f.Value = bytesValue(data)

			if depth < MaxDepth && len(data) > 0 {
				if nested, err := decode(data, depth+1); err == nil && len(nested) > 0 {
					f.Nested = nested
				}
			}
		case 5:
			if len(msg) < 4 {
				return out, errors.New("grpcmsg: cut fixed32")
			}

			v := binary.LittleEndian.Uint32(msg)
			msg = msg[4:]
			f.Wire = "fixed32"
			f.Value = fmt.Sprintf("%d (0x%08x, %g)", v, v, math.Float32frombits(v))
		default:
			return out, fmt.Errorf("grpcmsg: wire type %d is not read", wire)
		}

		out = append(out, f)
	}

	return out, nil
}

// varintValue shows a varint as the number it most likely is: unsigned,
// and when that reads as huge, the two signed readings beside it, since a
// negative int32 or int64 arrives as ten bytes of varint.
func varintValue(v uint64) string {
	if v > math.MaxInt32 {
		zigzag := int64(v>>1) ^ -int64(v&1)

		return fmt.Sprintf("%d (as sint %d, as int64 %d)", v, zigzag, int64(v))
	}

	return strconv.FormatUint(v, 10)
}

// bytesValue shows a length-delimited field as text when it reads as
// text, and as hex otherwise, capped.
func bytesValue(b []byte) string {
	if utf8.Valid(b) && isText(b) {
		s := string(b)
		if len(s) > MaxValueBytes*4 {
			cut := MaxValueBytes * 4
			for cut > 0 && !utf8.RuneStart(s[cut]) {
				cut--
			}

			s = s[:cut] + "..."
		}

		return strconv.Quote(s)
	}

	if len(b) > MaxValueBytes {
		return hex.EncodeToString(b[:MaxValueBytes]) + fmt.Sprintf("... (%d bytes)", len(b))
	}

	return hex.EncodeToString(b)
}

func isText(b []byte) bool {
	for _, r := range string(b) {
		if r == utf8.RuneError || (!unicode.IsPrint(r) && !unicode.IsSpace(r)) {
			return false
		}
	}

	return true
}

// DecodeWith reads a message with its descriptor and renders it as JSON,
// field names and all. protojson varies its whitespace on purpose, so the
// text is re-indented through jsontext to come out the same each time.
func DecodeWith(msg []byte, md protoreflect.MessageDescriptor) (string, error) {
	m := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(msg, m); err != nil {
		return "", err
	}

	out, err := protojson.Marshal(m)
	if err != nil {
		return "", err
	}

	pretty := jsontext.Value(out)
	if err := pretty.Indent(jsontext.WithIndent("  ")); err != nil {
		return "", err
	}

	return string(pretty), nil
}

// Status reads the grpc-status and grpc-message of a call: from the
// trailers, from the headers of a trailers-only answer, or from the
// trailer frame of a grpc-web body. code is "" when none says.
func Status(header, trailer http.Header, frames []Frame) (code, message string) {
	for _, h := range []http.Header{trailer, header} {
		if c := h.Get("Grpc-Status"); c != "" {
			return c, unescape(h.Get("Grpc-Message"))
		}
	}

	for _, f := range slices.Backward(frames) {
		if !f.Trailer {
			continue
		}

		if c := f.Trailers.Get("grpc-status"); c != "" {
			return c, unescape(f.Trailers.Get("grpc-message"))
		}
	}

	return "", ""
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}

	return s
}
