// Package response is the JSON edge of the API: how a body is rendered,
// how a request body is read, and the one error envelope every refusal
// uses. Handlers never call the json package directly, so what a shape
// looks like on the wire is decided here and nowhere else.
package response

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// marshalOptions is the wire policy, stated once.
//
// An empty list is [] (json/v2 formats a nil slice that way by default),
// a nil map is {} and invalid UTF-8 is coerced to U+FFFD rather than
// refused. The last one matters here more than anywhere: response bodies
// carry header values and body bytes taken off the network, and a latin-1
// byte in a proxied response must not turn a 200 into a 500.
var marshalOptions = jsontext.AllowInvalidUTF8(true)

// unmarshalOptions is the request policy. A member the struct does not
// know is refused rather than ignored, so a misspelled key in a settings
// PUT is an error the console sees instead of a value that silently
// never lands.
var unmarshalOptions = json.JoinOptions(
	json.RejectUnknownMembers(true),
)

// Marshal renders v under the product's wire policy.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v, marshalOptions)
}

// NewStreamEncoder is an encoder over w under the wire policy, for a
// body too large to build in memory: an export writes its envelope as
// tokens and each record through MarshalEncode.
func NewStreamEncoder(w io.Writer) *jsontext.Encoder {
	return jsontext.NewEncoder(w, marshalOptions)
}

// MarshalEncode renders v onto enc under the wire policy.
func MarshalEncode(enc *jsontext.Encoder, v any) error {
	return json.MarshalEncode(enc, v, marshalOptions)
}

// MaxBodyBytes bounds a request body read by Decode. Generous, because
// an intercepted request being forwarded carries the body the client
// sent, and a sender request may carry a file upload.
const MaxBodyBytes = 64 << 20

// Decode reads the JSON body of r into v. A body that is missing, too
// large, malformed or names an unknown member is a *DecodeError, which
// Handle turns into a 400. A handler returns the error and nothing else.
func Decode(r *http.Request, v any) error {
	body := http.MaxBytesReader(nil, r.Body, MaxBodyBytes)

	if err := json.UnmarshalRead(body, v, unmarshalOptions); err != nil {
		return &DecodeError{Err: err}
	}

	return nil
}

// DecodeError is a request body the API could not read as the type the
// handler asked for.
type DecodeError struct {
	Err error
}

// Error implements error.
func (e *DecodeError) Error() string {
	return "invalid request body: " + e.Err.Error()
}

// Unwrap exposes the json error for errors.Is and errors.As.
func (e *DecodeError) Unwrap() error {
	return e.Err
}

// JSON writes v with the given status. A marshal failure is a bug in
// the type being rendered and is reported as a 500 rather than half a
// body: the status line has to be written before the bytes are, so the
// bytes are rendered first.
func JSON(w http.ResponseWriter, status int, v any) {
	out, err := Marshal(v)
	if err != nil {
		slog.Error("response: marshal failed", "err", err, "type", fmt.Sprintf("%T", v))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal error"}`))

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

// Envelope is the one error shape. Every refusal and every failure the
// API reports looks like this, so a client parses one thing.
type Envelope struct {
	Error string `json:"error"`
}

// Fail writes the error envelope with the given status.
func Fail(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, Envelope{Error: msg})
}

// Handler is a handler that reports failure by returning an error
// rather than writing a status itself. Handle maps the error to a
// status, so the mapping from domain errors to HTTP lives in one place.
type Handler func(w http.ResponseWriter, r *http.Request) error

// StatusError carries an explicit status for a refusal the domain has
// already classified - a 404 for a missing row, a 409 for a conflict.
type StatusError struct {
	Status int
	Msg    string
}

// Error implements error.
func (e *StatusError) Error() string {
	return e.Msg
}

// NotFound is a 404 with msg.
func NotFound(msg string) error {
	return &StatusError{Status: http.StatusNotFound, Msg: msg}
}

// BadRequest is a 400 with msg.
func BadRequest(msg string) error {
	return &StatusError{Status: http.StatusBadRequest, Msg: msg}
}

// Conflict is a 409 with msg.
func Conflict(msg string) error {
	return &StatusError{Status: http.StatusConflict, Msg: msg}
}

// PayloadTooLarge is a 413 with msg.
func PayloadTooLarge(msg string) error {
	return &StatusError{Status: http.StatusRequestEntityTooLarge, Msg: msg}
}

// BadGateway is a 502 with msg, for a sender request the upstream
// refused or a proxy that could not reach its target.
func BadGateway(msg string) error {
	return &StatusError{Status: http.StatusBadGateway, Msg: msg}
}

// Handle adapts a Handler to http.Handler, answering an error with the
// envelope. A *StatusError keeps its status, a *DecodeError and a
// too-large body are 400, and anything else is logged and answered as a
// generic 500 - the raw message is not echoed, because it commonly
// names files and SQL.
func Handle(h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}

		if se, ok := errors.AsType[*StatusError](err); ok {
			Fail(w, se.Status, se.Msg)

			return
		}

		_, decode := errors.AsType[*DecodeError](err)
		_, tooBig := errors.AsType[*http.MaxBytesError](err)

		switch {
		case tooBig:
			Fail(w, http.StatusRequestEntityTooLarge, err.Error())
		case decode, errors.Is(err, io.ErrUnexpectedEOF):
			Fail(w, http.StatusBadRequest, err.Error())
		default:
			slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
			Fail(w, http.StatusInternalServerError, "internal error")
		}
	})
}
