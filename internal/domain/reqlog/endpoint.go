package reqlog

import (
	"context"
	"errors"
	"github.com/yousysadmin/ihttp/internal/core/filter"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/yousysadmin/ihttp/internal/core/grpcmsg"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/core/response"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// Handler answers the request log routes.
type Handler struct {
	Svc *Service

	// Schemas resolves a gRPC method to its message types, when the
	// project has schemas. Nil reads gRPC without names.
	Schemas SchemaResolver
}

// SchemaResolver answers the input and output message of a gRPC path.
type SchemaResolver interface {
	Method(ctx context.Context, fullMethod string) (in, out protoreflect.MessageDescriptor, ok bool)
}

// List handles GET /api/request-logs.
func (h Handler) List(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	limit, err := queryInt(q, "limit")
	if err != nil {
		return err
	}

	entries, more, err := h.Svc.List(r.Context(), ListParams{
		Search:      q.Get("search"),
		OnlyInScope: q.Get("only_in_scope") == "true",
		Saved:       q.Get("saved") == "true",
		Before:      q.Get("before"),
		Limit:       limit,

		// A view of the log, so a muted host is left out of it - unless
		// the caller asks for them, which is what the console does when
		// you go looking at one on purpose. The export and the delete
		// below always take the filter as written.
		HonourMutes: q.Get("include_muted") != "true",
	})
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, ListResponse{Entries: entries, More: more})

	return nil
}

// Get handles GET /api/request-logs/{id}.
func (h Handler) Get(w http.ResponseWriter, r *http.Request) error {
	e, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, EntryResponse{Entry: e})

	return nil
}

// Body handles GET /api/request-logs/{id}/body/{side}: the raw bytes of
// one half of the exchange, served under their own content type, so the
// console can show an image as an image and offer a download of exactly
// what passed through.
func (h Handler) Body(w http.ResponseWriter, r *http.Request) error {
	e, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	switch r.PathValue("side") {
	case "request":
		ServeBody(w, e.Headers, e.Body)
	case "response":
		if e.Response == nil {
			return response.NotFound("no response yet")
		}

		ServeBody(w, e.Response.Headers, e.Response.Body)
	default:
		return response.NotFound("side must be request or response")
	}

	return nil
}

// ServeBody writes body under the content type its headers carried, or
// octet-stream. Inline, so an image renders in the tab, and never
// sniffed, so a body that claims to be text cannot become a script.
func ServeBody(w http.ResponseWriter, headers httpmsg.Headers, body httpmsg.Body) {
	ct := headers.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// GRPC handles GET /api/request-logs/{id}/grpc/{side}: the frames of a
// gRPC request or response, decoded as far as the wire format allows.
func (h Handler) GRPC(w http.ResponseWriter, r *http.Request) error {
	e, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	var (
		headers httpmsg.Headers
		body    httpmsg.Body
		out     GRPCResponse
	)

	switch r.PathValue("side") {
	case "request":
		headers, body = e.Headers, e.Body
	case "response":
		if e.Response == nil {
			return response.NotFound("no response yet")
		}

		headers, body = e.Response.Headers, e.Response.Body
		out.Status, out.Message = grpcmsg.Status(headers.Lookup(), e.Response.Trailers.Lookup(), nil)
	default:
		return response.NotFound("side must be request or response")
	}

	out.Kind = grpcmsg.KindOf(headers.Get("Content-Type"))

	// A bare protobuf body over plain HTTP: the same wire format with no
	// framing around it, so it is read as one message and shown by the
	// same view. There is no service, no method and no schema to look
	// up - a path is what names those, and this has none.
	if out.Kind == grpcmsg.KindNone && grpcmsg.IsProtobuf(headers.Get("Content-Type")) {
		out.Kind = grpcmsg.KindProtobuf
		out.Frames = []grpcmsg.Frame{grpcmsg.Message(body)}

		response.JSON(w, http.StatusOK, out)

		return nil
	}

	if out.Kind == grpcmsg.KindNone {
		return response.BadRequest("not a gRPC or protobuf body")
	}

	var md protoreflect.MessageDescriptor

	if u, err := url.Parse(e.URL); err == nil {
		out.Service, out.Method = grpcmsg.Method(u.Path)

		if h.Schemas != nil {
			if in, res, ok := h.Schemas.Method(r.Context(), u.Path); ok {
				md = in
				if r.PathValue("side") == "response" {
					md = res
				}
			}
		}
	}

	frames, err := grpcmsg.Split(body, out.Kind, headers.Get("Grpc-Encoding"))
	if err != nil {
		out.Error = err.Error()
	}

	if frames == nil {
		frames = []grpcmsg.Frame{}
	}

	if md != nil {
		out.HasSchema = true
		out.MessageType = string(md.FullName())

		for i := range frames {
			f := &frames[i]
			if f.Trailer || f.Err != "" {
				continue
			}

			if text, err := grpcmsg.DecodeWith(f.Raw, md); err != nil {
				f.Err = "schema: " + err.Error()
			} else {
				f.JSON = text
			}
		}
	}

	out.Frames = frames
	out.More = len(frames) == grpcmsg.MaxFrames

	if out.Status == "" {
		out.Status, out.Message = grpcmsg.Status(nil, nil, frames)
	}

	response.JSON(w, http.StatusOK, out)

	return nil
}

// MessageProtobuf handles
// GET /api/request-logs/{id}/messages/{seq}/protobuf: one WebSocket
// message read as a protobuf message.
//
// A frame carries no content type, so nothing can decide this for the
// reader - it is asked for, and the answer says plainly when the bytes
// do not read as protobuf.
func (h Handler) MessageProtobuf(w http.ResponseWriter, r *http.Request) error {
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil {
		return response.NotFound("no such message")
	}

	msg, err := h.Svc.Message(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, GRPCResponse{
		Kind:   grpcmsg.KindProtobuf,
		Frames: []grpcmsg.Frame{grpcmsg.Message(msg.Payload)},
	})

	return nil
}

// Messages handles GET /api/request-logs/{id}/messages. With order=desc
// the page is newest first and before= is the cursor, otherwise it is
// oldest first and after= is.
func (h Handler) Messages(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	limit, err := queryInt(q, "limit")
	if err != nil {
		return err
	}

	var (
		list []reqlog.MessageSummary
		more bool
	)

	if q.Get("order") == "desc" {
		before, perr := queryInt(q, "before")
		if perr != nil {
			return perr
		}

		list, more, err = h.Svc.MessagesBefore(r.Context(), r.PathValue("id"), before, limit)
	} else {
		after, perr := queryInt(q, "after")
		if perr != nil {
			return perr
		}

		list, more, err = h.Svc.Messages(r.Context(), r.PathValue("id"), after, limit)
	}

	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, MessagesResponse{Messages: list, More: more})

	return nil
}

// Message handles GET /api/request-logs/{id}/messages/{seq}.
func (h Handler) Message(w http.ResponseWriter, r *http.Request) error {
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil {
		return response.NotFound("message not found")
	}

	m, err := h.Svc.Message(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, MessageResponse{Message: m})

	return nil
}

// MessageBody handles GET /api/request-logs/{id}/messages/{seq}/body: the
// payload bytes, as text for a text message and as bytes otherwise.
func (h Handler) MessageBody(w http.ResponseWriter, r *http.Request) error {
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil {
		return response.NotFound("message not found")
	}

	m, err := h.Svc.Message(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		return mapErr(err)
	}

	ct := "application/octet-stream"
	if m.Opcode == "text" && !m.Compressed {
		ct = "text/plain; charset=utf-8"
	}

	ServeBody(w, httpmsg.Headers{{Name: "Content-Type", Value: ct}}, m.Payload)

	return nil
}

// Save handles POST /api/request-logs/{id}/save.
func (h Handler) Save(w http.ResponseWriter, r *http.Request) error {
	e, err := h.Svc.SetSaved(r.Context(), r.PathValue("id"), true)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, EntryResponse{Entry: e})

	return nil
}

// Unsave handles DELETE /api/request-logs/{id}/save.
func (h Handler) Unsave(w http.ResponseWriter, r *http.Request) error {
	e, err := h.Svc.SetSaved(r.Context(), r.PathValue("id"), false)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, EntryResponse{Entry: e})

	return nil
}

// Patch handles PATCH /api/request-logs/{id}: the marks on an entry.
func (h Handler) Patch(w http.ResponseWriter, r *http.Request) error {
	var in MarksRequest
	if err := response.Decode(r, &in); err != nil {
		return err
	}

	e, err := h.Svc.SetMarks(r.Context(), r.PathValue("id"), in)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, EntryResponse{Entry: e})

	return nil
}

// Top handles GET /api/request-logs/top: the slowest or largest
// entries of the current filter, which is what column sorting is for.
func (h Handler) Top(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	limit, err := queryInt(q, "limit")
	if err != nil {
		return err
	}

	entries, err := h.Svc.Top(r.Context(), ListParams{
		Search:      q.Get("search"),
		OnlyInScope: q.Get("only_in_scope") == "true",
		Saved:       q.Get("saved") == "true",

		// A muted host is muted in the LIST, and a ranking is a
		// question about the whole log: leaving it out would answer
		// "the slowest thing you are looking at", which nobody asked.
		HonourMutes: false,
	}, TopBy(q.Get("by")), limit)
	if err != nil {
		return mapErr(err)
	}

	if entries == nil {
		entries = []reqlog.Summary{}
	}

	response.JSON(w, http.StatusOK, ListResponse{Entries: entries})

	return nil
}

// Hosts handles GET /api/request-logs/hosts: the log grouped by host,
// busiest first, with the failures and whether the host is muted.
func (h Handler) Hosts(w http.ResponseWriter, r *http.Request) error {
	hosts, err := h.Svc.Hosts(r.Context())
	if err != nil {
		return mapErr(err)
	}

	if hosts == nil {
		hosts = []HostCount{}
	}

	response.JSON(w, http.StatusOK, HostsResponse{Hosts: hosts})

	return nil
}

// Tags handles GET /api/request-logs/tags.
func (h Handler) Tags(w http.ResponseWriter, r *http.Request) error {
	tags, err := h.Svc.Tags(r.Context())
	if err != nil {
		return mapErr(err)
	}

	if tags == nil {
		tags = []TagCount{}
	}

	response.JSON(w, http.StatusOK, TagsResponse{Tags: tags})

	return nil
}

// Snippets handles GET /api/request-logs/{id}/snippets.
func (h Handler) Snippets(w http.ResponseWriter, r *http.Request) error {
	e, err := h.Svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, Snippets(httpmsg.SnippetInput{
		Method: e.Method, URL: e.URL, Proto: e.Proto, Headers: e.Headers, Body: e.Body,
	}))

	return nil
}

// ExportHAR handles GET /api/request-logs/export.har, the log as a HAR
// file, narrowed by the same search and scope switch as the list.
func (h Handler) ExportHAR(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()

	name, write, err := h.Svc.ExportHAR(r.Context(), ListParams{
		Search:      q.Get("search"),
		OnlyInScope: q.Get("only_in_scope") == "true",
		Saved:       q.Get("saved") == "true",
	})
	if err != nil {
		return mapErr(err)
	}

	ServeAttachment(w, name+".har", "application/json")

	if err := write(w); err != nil {
		// The status is gone. The client sees a document cut short.
		h.Svc.log.Error("reqlog: har export", "err", err)
	}

	return nil
}

// ServeAttachment sets the headers of a download: contentType, a file
// name reduced to what every filesystem takes, and no caching.
func ServeAttachment(w http.ResponseWriter, filename, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(filename)+`"`)
	w.Header().Set("Cache-Control", "no-store")
}

// safeFilename keeps letters, digits, dot, dash and underscore and turns
// everything else into a dash, so a project name is a file name.
func safeFilename(name string) string {
	out := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)

	if strings.Trim(out, "-.") == "" {
		return "export"
	}

	return out
}

// Delete handles DELETE /api/request-logs/{id}.
func (h Handler) Delete(w http.ResponseWriter, r *http.Request) error {
	if err := h.Svc.Delete(r.Context(), r.PathValue("id")); err != nil {
		return mapErr(err)
	}

	w.WriteHeader(http.StatusNoContent)

	return nil
}

// Clear handles DELETE /api/request-logs. With a search or the scope
// switch in the query only what they select goes, and the count is
// answered. Without either the whole log goes.
func (h Handler) Clear(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	params := ListParams{
		Search:      q.Get("search"),
		OnlyInScope: q.Get("only_in_scope") == "true",
		Saved:       q.Get("saved") == "true",
	}

	if strings.TrimSpace(params.Search) == "" && !params.OnlyInScope && !params.Saved {
		if err := h.Svc.Clear(r.Context()); err != nil {
			return mapErr(err)
		}

		w.WriteHeader(http.StatusNoContent)

		return nil
	}

	n, err := h.Svc.DeleteMatching(r.Context(), params)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusOK, DeletedResponse{Deleted: n})

	return nil
}

// ImportHAR handles POST /api/request-logs/import.har: the body is the
// file, the answer is how many entries it wrote. Every entry is added to
// the open project's log with an `imported` tag. Nothing already there
// is touched, so importing twice doubles the rows rather than merging
// them.
func (h Handler) ImportHAR(w http.ResponseWriter, r *http.Request) error {
	body := http.MaxBytesReader(w, r.Body, MaxHARImportBytes)

	n, err := h.Svc.ImportHAR(r.Context(), body)
	if err != nil {
		return mapErr(err)
	}

	response.JSON(w, http.StatusCreated, ImportResult{Entries: n})

	return nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return response.NotFound("request log entry not found")
	case errors.Is(err, ErrMessageNotFound):
		return response.NotFound("message not found")
	case errors.Is(err, project.ErrNoActiveProject):
		return response.Conflict("no project is open")
	case errors.Is(err, ErrInvalidMarks):
		return response.BadRequest(strings.TrimPrefix(err.Error(), ErrInvalidMarks.Error()+": "))
	case errors.Is(err, filter.ErrInvalid):
		return response.BadRequest(err.Error())
	case errors.Is(err, ErrBadHAR):
		return response.BadRequest(err.Error())
	case errors.Is(err, ErrBadRanking):
		return response.BadRequest(strings.TrimPrefix(err.Error(), ErrBadRanking.Error()+": "))
	}

	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return response.PayloadTooLarge("the file is larger than the import allows")
	}

	return err
}

// queryInt reads an integer query parameter. Absent is 0, a value that
// is not a number is a 400.
func queryInt(q url.Values, name string) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, response.BadRequest(name + " must be a number")
	}

	return n, nil
}
