// Package mcp serves ihttp's request log and tools to an AI agent over
// the Model Context Protocol, on stdin and stdout.
//
// The protocol is hand-written rather than taken from a library. MCP
// over stdio is newline-delimited JSON-RPC 2.0 and the surface used here
// is four methods. The official Go SDK would add seven direct
// dependencies and does not use encoding/json/v2, which everything here
// does.
//
// This server is a CLIENT of a running ihttp: it talks to the console
// API over HTTP rather than opening the database. So it works against an
// instance in a container or on another host, needs no shared state, and
// cannot be the reason two processes fight over one bbolt file.
package mcp

import (
	"bufio"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
)

// The protocol versions this server speaks, newest first. A client that
// asks for one of these gets it back, and anything else gets the newest and
// may disconnect, which is what the spec says to do.
var protocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC 2.0 error codes the spec defines.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// request is one incoming message. A message with no id is a
// notification and is never answered.
type request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      jsontext.Value `json:"id,omitzero"`
	Method  string         `json:"method"`
	Params  jsontext.Value `json:"params,omitzero"`
}

func (r request) isNotification() bool {
	return len(r.ID) == 0 || string(r.ID) == "null"
}

type response struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      jsontext.Value `json:"id"`
	Result  any            `json:"result,omitzero"`
	Error   *rpcError      `json:"error,omitzero"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      implementation     `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

type serverCapabilities struct {
	// omitzero, not omitempty: in encoding/json/v2 omitempty drops an
	// empty JSON value, and the tools capability IS an empty object -
	// so omitempty would silently announce no tools at all.
	Tools *toolsCapability `json:"tools,omitzero"`
}

type toolsCapability struct{}

// Tool is one thing an agent can call.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// InputSchema is a JSON Schema object. Written by hand: the shapes
	// are small and a generator would be another dependency.
	InputSchema jsontext.Value `json:"inputSchema"`

	// Call runs the tool. The text it returns is what the agent reads.
	Call func(ctx context.Context, args jsontext.Value) (string, error) `json:"-"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolParams struct {
	Name      string         `json:"name"`
	Arguments jsontext.Value `json:"arguments,omitzero"`
}

type callToolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitzero"`
}

// Server answers one stdio session.
type Server struct {
	name    string
	version string
	tools   []Tool
	log     *slog.Logger
}

// NewServer builds a server over the given tools. log must NOT write to
// stdout: stdout is the protocol channel and a log line on it would be a
// parse error at the other end.
func NewServer(name, version string, tools []Tool, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}

	return &Server{name: name, version: version, tools: tools, log: log}
}

// Serve reads messages from in and writes answers to out until in ends.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	// A captured body or a HAR can be large, and a line is one message.
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), 64<<20)

	w := bufio.NewWriter(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		answer := s.handle(ctx, []byte(line))
		if answer == nil {
			// A notification: nothing to say back.
			continue
		}

		data, err := json.Marshal(answer)
		if err != nil {
			return fmt.Errorf("mcp: marshal answer: %w", err)
		}

		if _, err := w.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("mcp: write answer: %w", err)
		}

		// Flushed per message: the client is waiting on this one.
		if err := w.Flush(); err != nil {
			return fmt.Errorf("mcp: flush: %w", err)
		}
	}

	return scanner.Err()
}

// handle answers one message, or returns nil for a notification.
func (s *Server) handle(ctx context.Context, line []byte) *response {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.log.Debug("mcp: unparseable message", "err", err)

		return &response{JSONRPC: "2.0", ID: jsontext.Value("null"), Error: &rpcError{Code: codeParse, Message: "could not parse the message"}}
	}

	if req.isNotification() {
		s.log.Debug("mcp: notification", "method", req.Method)

		return nil
	}

	reply := &response{JSONRPC: "2.0", ID: req.ID}

	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		reply.Error = &rpcError{Code: codeInvalidRequest, Message: "this is JSON-RPC 2.0"}

		return reply
	}

	s.log.Debug("mcp: request", "method", req.Method)

	switch req.Method {
	case "initialize":
		reply.Result = s.initialize(req.Params)
	case "tools/list":
		reply.Result = toolsListResult{Tools: s.tools}
	case "tools/call":
		reply.Result, reply.Error = s.callTool(ctx, req.Params)
	case "ping":
		reply.Result = struct{}{}
	default:
		reply.Error = &rpcError{Code: codeMethodNotFound, Message: "no such method: " + req.Method}
	}

	return reply
}

func (s *Server) initialize(params jsontext.Value) initializeResult {
	var in initializeParams
	if len(params) > 0 {
		_ = json.Unmarshal(params, &in)
	}

	// The client's version when it is one we speak, else our newest -
	// the client then decides whether it can live with that.
	version := protocolVersions[0]
	if slices.Contains(protocolVersions, in.ProtocolVersion) {
		version = in.ProtocolVersion
	}

	return initializeResult{
		ProtocolVersion: version,
		Capabilities:    serverCapabilities{Tools: &toolsCapability{}},
		ServerInfo:      implementation{Name: s.name, Version: s.version},
		Instructions: "ihttp is an HTTP proxy with a searchable log of everything it has seen. " +
			"Search it with the filter query language, which log_search documents: a query like " +
			`res.statusCode >= 500 AND req.host = api.example.com AND res.ttfb > 1s ` +
			"answers a question that would otherwise take scrolling. Read one exchange with " +
			"log_get, its body with log_body, and hand it back to the human as a shell command " +
			"with log_curl.",
	}
}

// callTool runs one tool. A tool that fails reports it as an error
// RESULT rather than a protocol error, so the agent sees what went
// wrong and can try something else instead of losing the connection.
func (s *Server) callTool(ctx context.Context, params jsontext.Value) (any, *rpcError) {
	var in callToolParams
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "could not read the call"}
	}

	i := slices.IndexFunc(s.tools, func(t Tool) bool { return t.Name == in.Name })
	if i < 0 {
		return nil, &rpcError{Code: codeInvalidParams, Message: "no such tool: " + in.Name}
	}

	text, err := s.tools[i].Call(ctx, in.Arguments)
	if err != nil {
		s.log.Debug("mcp: tool failed", "tool", in.Name, "err", err)

		return callToolResult{Content: []content{{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}

	return callToolResult{Content: []content{{Type: "text", Text: text}}}, nil
}
