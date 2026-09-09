package reqlog

import (
	"github.com/yousysadmin/ihttp/internal/core/grpcmsg"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// ListResponse is the body of GET /api/request-logs.
type ListResponse struct {
	Entries []reqlog.Summary `json:"entries"`
	More    bool             `json:"more"`
}

// EntryResponse is the body of GET /api/request-logs/{id}.
type EntryResponse struct {
	Entry reqlog.Entry `json:"entry"`
}

// SnippetsResponse is the body of GET .../snippets: one request,
// spelled as every command and program the tool knows how to write.
// All of them at once because they are cheap to render and a round
// trip per language is not.
type SnippetsResponse struct {
	Curl       string `json:"curl"`
	Fetch      string `json:"fetch"`
	HTTPie     string `json:"httpie"`
	Python     string `json:"python"`
	Go         string `json:"go"`
	PowerShell string `json:"powershell"`
}

// MessagesResponse is the body of GET /api/request-logs/{id}/messages.
type MessagesResponse struct {
	Messages []reqlog.MessageSummary `json:"messages"`
	More     bool                    `json:"more"`
}

// MessageResponse wraps one WebSocket message.
type MessageResponse struct {
	Message reqlog.Message `json:"message"`
}

// GRPCResponse is the body of GET /api/request-logs/{id}/grpc/{side}: one
// side of a gRPC exchange cut into frames and read without a schema.
type GRPCResponse struct {
	Kind    grpcmsg.Kind    `json:"kind"`
	Service string          `json:"service"`
	Method  string          `json:"method"`
	Status  string          `json:"status,omitempty"`
	Message string          `json:"message,omitempty"`
	Frames  []grpcmsg.Frame `json:"frames"`
	More    bool            `json:"more"`
	Error   string          `json:"error,omitempty"`

	// HasSchema says a project schema named the message, MessageType
	// which one.
	HasSchema   bool   `json:"has_schema"`
	MessageType string `json:"message_type,omitempty"`
}

// DeletedEvent is what reqlog.deleted carries: the ids that went, or,
// when retention did the deleting, how many rows it dropped without
// naming them - the console reloads on a trim rather than trying to
// remove rows it may never have had.
type DeletedEvent struct {
	IDs     []string `json:"ids"`
	Trimmed int      `json:"trimmed,omitzero"`
}

// DeletedResponse is the body of a DELETE that removed a selection.
type DeletedResponse struct {
	Deleted int `json:"deleted"`
}

// MarksRequest is the body of PATCH /api/request-logs/{id}. A member
// left out is left as it is, so a tag edit does not touch the note.
type MarksRequest struct {
	Tags  *[]string `json:"tags"`
	Note  *string   `json:"note"`
	Color *string   `json:"color"`
}

// TagsResponse is the body of GET /api/request-logs/tags: every tag in
// the open project's log with how many entries carry it.
type TagsResponse struct {
	Tags []TagCount `json:"tags"`
}

// HostsResponse is the body of GET /api/request-logs/hosts.
type HostsResponse struct {
	Hosts []HostCount `json:"hosts"`
}

// HostCount is one host in the log: how many entries, how many were
// answered with a 5xx, and whether the project has muted it. A 4xx is
// not an error here - it is usually the answer the request was after.
type HostCount struct {
	Host   string `json:"host"`
	Count  int    `json:"count"`
	Errors int    `json:"errors"`
	Muted  bool   `json:"muted,omitzero"`
}

// TagCount is one tag and its count.
type TagCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Snippets renders both spellings of a request.
func Snippets(in httpmsg.SnippetInput) SnippetsResponse {
	return SnippetsResponse{
		Curl:       httpmsg.Curl(in),
		Fetch:      httpmsg.Fetch(in),
		HTTPie:     httpmsg.HTTPie(in),
		Python:     httpmsg.Python(in),
		Go:         httpmsg.Go(in),
		PowerShell: httpmsg.PowerShell(in),
	}
}
