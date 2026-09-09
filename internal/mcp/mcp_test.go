package mcp_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	"github.com/yousysadmin/ihttp/internal/mcp"
)

// session drives the server over one pipe and returns the answers, in
// order, as decoded maps.
func session(t *testing.T, tools []mcp.Tool, lines ...string) []map[string]any {
	t.Helper()

	srv := mcp.NewServer("ihttp", "test", tools, slog.New(slog.DiscardHandler))

	in := strings.NewReader(strings.Join(lines, "\n") + "\n")

	var out strings.Builder
	if err := srv.Serve(t.Context(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var answers []map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}

		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("answer %q: %v", line, err)
		}

		answers = append(answers, m)
	}

	return answers
}

func echoTool() mcp.Tool {
	return mcp.Tool{
		Name:        "echo",
		Description: "says it back",
		InputSchema: jsontext.Value(`{"type":"object","properties":{"say":{"type":"string"}}}`),
		Call: func(_ context.Context, args jsontext.Value) (string, error) {
			var in struct {
				Say string `json:"say"`
			}

			_ = json.Unmarshal(args, &in)

			if in.Say == "boom" {
				return "", fmt.Errorf("it went boom")
			}

			return "you said " + in.Say, nil
		},
	}
}

func TestInitializeNegotiatesTheVersion(t *testing.T) {
	cases := map[string]string{
		// A version we speak comes back as it was asked for.
		"2024-11-05": "2024-11-05",
		"2025-06-18": "2025-06-18",
		"2025-11-25": "2025-11-25",
		// One we do not gets our newest, and the client decides.
		"1999-01-01": "2025-11-25",
		"":           "2025-11-25",
	}

	for asked, want := range cases {
		answers := session(t, nil,
			fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q}}`, asked))

		if len(answers) != 1 {
			t.Fatalf("%q: %d answers", asked, len(answers))
		}

		result, _ := answers[0]["result"].(map[string]any)
		if got := result["protocolVersion"]; got != want {
			t.Errorf("asked %q, got %v, want %v", asked, got, want)
		}
	}
}

// The tools capability has to be announced, and it is an EMPTY object -
// which json/v2's omitempty would have dropped, quietly telling every
// client there are no tools.
func TestInitializeAnnouncesTools(t *testing.T) {
	answers := session(t, []mcp.Tool{echoTool()},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)

	result := answers[0]["result"].(map[string]any)

	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("no capabilities: %v", result)
	}

	if _, ok := caps["tools"]; !ok {
		t.Errorf("the tools capability was not announced: %v", caps)
	}
}

func TestToolsListAndCall(t *testing.T) {
	answers := session(t, []mcp.Tool{echoTool()},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"say":"hello"}}}`,
	)

	if len(answers) != 2 {
		t.Fatalf("%d answers", len(answers))
	}

	list := answers[0]["result"].(map[string]any)["tools"].([]any)
	if len(list) != 1 {
		t.Fatalf("%d tools", len(list))
	}

	tool := list[0].(map[string]any)
	if tool["name"] != "echo" || tool["description"] == "" {
		t.Errorf("tool: %v", tool)
	}

	// The schema must survive as an object, not as a quoted string.
	if _, ok := tool["inputSchema"].(map[string]any); !ok {
		t.Errorf("inputSchema is not an object: %T", tool["inputSchema"])
	}

	result := answers[1]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"]

	if text != "you said hello" {
		t.Errorf("call result: %v", text)
	}

	if result["isError"] == true {
		t.Error("a working call was reported as an error")
	}
}

// A tool that fails reports it as an error RESULT, not a protocol error:
// the agent sees what went wrong and can try something else instead of
// the connection breaking.
func TestAFailingToolIsAResultNotAProtocolError(t *testing.T) {
	answers := session(t, []mcp.Tool{echoTool()},
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"say":"boom"}}}`)

	if _, isRPCError := answers[0]["error"]; isRPCError {
		t.Fatalf("the failure came back as a protocol error: %v", answers[0])
	}

	result := answers[0]["result"].(map[string]any)
	if result["isError"] != true {
		t.Errorf("isError was not set: %v", result)
	}

	if text := result["content"].([]any)[0].(map[string]any)["text"]; text != "it went boom" {
		t.Errorf("text: %v", text)
	}
}

// A notification has no id and must get no answer at all, or the client
// sees a reply to a message it never asked about.
func TestNotificationsAreNotAnswered(t *testing.T) {
	answers := session(t, nil,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
	)

	if len(answers) != 1 {
		t.Fatalf("%d answers, want only the ping's", len(answers))
	}

	if answers[0]["id"] != 1.0 {
		t.Errorf("the answer is not the ping's: %v", answers[0])
	}
}

func TestBadInput(t *testing.T) {
	answers := session(t, []mcp.Tool{echoTool()},
		`this is not json`,
		`{"jsonrpc":"2.0","id":2,"method":"nonsense"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"missing"}}`,
	)

	if len(answers) != 3 {
		t.Fatalf("%d answers", len(answers))
	}

	for i, want := range []float64{-32700, -32601, -32602} {
		e, ok := answers[i]["error"].(map[string]any)
		if !ok {
			t.Fatalf("answer %d is not an error: %v", i, answers[i])
		}

		if e["code"] != want {
			t.Errorf("answer %d code %v, want %v", i, e["code"], want)
		}
	}
}

// Every tool must be callable: a nil Call would panic the session, and a
// missing schema would make the tool unusable by a client that validates.
func TestEveryToolIsWellFormed(t *testing.T) {
	for _, allowWrite := range []bool{false, true} {
		tools := mcp.Tools(mcp.NewClient("127.0.0.1:1"), mcp.Options{AllowWrite: allowWrite, Redact: true})

		if len(tools) == 0 {
			t.Fatal("no tools")
		}

		seen := map[string]bool{}

		for _, tool := range tools {
			if tool.Name == "" || tool.Description == "" {
				t.Errorf("a tool has no name or no description: %+v", tool.Name)
			}

			if seen[tool.Name] {
				t.Errorf("two tools are called %s", tool.Name)
			}

			seen[tool.Name] = true

			if tool.Call == nil {
				t.Errorf("%s has no Call", tool.Name)
			}

			var schema map[string]any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Errorf("%s has an unparseable inputSchema: %v", tool.Name, err)

				continue
			}

			if schema["type"] != "object" {
				t.Errorf("%s inputSchema is not an object schema: %v", tool.Name, schema)
			}
		}

		// The write tools appear only when asked for.
		if _, ok := seen["project_open"]; ok != allowWrite {
			t.Errorf("allowWrite=%v but project_open present=%v", allowWrite, ok)
		}
	}
}

// Without a running ihttp every tool has to say so in terms the operator
// can act on, rather than reporting a bare connection error.
func TestToolsSayWhenNothingIsRunning(t *testing.T) {
	tools := mcp.Tools(mcp.NewClient("127.0.0.1:1"), mcp.Options{Redact: true})

	for _, tool := range tools {
		if tool.Name == "filter_help" {
			continue
		}

		_, err := tool.Call(t.Context(), jsontext.Value(`{"id":"x","side":"request","query":""}`))
		if err == nil {
			continue
		}

		if !strings.Contains(err.Error(), "no ihttp is answering") &&
			!strings.Contains(err.Error(), "is not an argument") {
			t.Errorf("%s said %q, which does not tell the operator what to do", tool.Name, err)
		}
	}
}

// The help an agent is handed has to be the vocabulary, not a stub.
func TestFilterHelpIsServed(t *testing.T) {
	tools := mcp.Tools(mcp.NewClient("127.0.0.1:1"), mcp.Options{Redact: true})

	var help mcp.Tool

	for _, tool := range tools {
		if tool.Name == "filter_help" {
			help = tool
		}
	}

	if help.Call == nil {
		t.Fatal("there is no filter_help tool")
	}

	text, err := help.Call(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"res.statusCode", "res.ttfb", "req.header.<name>", "contains", "AND"} {
		if !strings.Contains(text, want) {
			t.Errorf("the help does not mention %q", want)
		}
	}
}

func TestRedactor(t *testing.T) {
	on := mcp.Redactor{On: true}
	off := mcp.Redactor{On: false}

	headers := httpmsg.Headers{
		{Name: "Authorization", Value: "Bearer secret"},
		{Name: "cookie", Value: "session=abc"},
		{Name: "Set-Cookie", Value: "session=abc"},
		{Name: "X-Api-Key", Value: "k"},
		{Name: "Content-Type", Value: "application/json"},
	}

	masked := on.Headers(headers)
	for _, h := range masked {
		if h.Name == "Content-Type" {
			if h.Value != "application/json" {
				t.Errorf("an innocent header was masked: %+v", h)
			}

			continue
		}

		if strings.Contains(h.Value, "secret") || strings.Contains(h.Value, "abc") || h.Value == "k" {
			t.Errorf("%s was not masked: %q", h.Name, h.Value)
		}
	}

	// Off means off, or --no-redact would be a lie.
	if got := off.Headers(headers); got[0].Value != "Bearer secret" {
		t.Errorf("redaction off still masked: %q", got[0].Value)
	}

	cases := map[string][]string{
		// The name survives, the value does not.
		"http://x/y?access_token=leaky&page=2": {"access_token", "page=2"},
		"http://x/y?api_key=leaky":             {"api_key"},
		"http://x/y?sessionId=leaky":           {"sessionId"},
		"http://x/y?signature=leaky":           {"signature"},
	}

	for raw, keep := range cases {
		got := on.URL(raw)

		if strings.Contains(got, "leaky") {
			t.Errorf("URL(%q) = %q, which still carries the value", raw, got)
		}

		for _, k := range keep {
			if !strings.Contains(got, strings.SplitN(k, "=", 2)[0]) {
				t.Errorf("URL(%q) = %q, which lost %q", raw, got, k)
			}
		}
	}

	// A URL with nothing secret is returned untouched, byte for byte.
	plain := "http://x/y?page=2&sort=asc"
	if got := on.URL(plain); got != plain {
		t.Errorf("URL(%q) = %q, want it unchanged", plain, got)
	}

	// The mask must survive URL encoding as itself.
	if got := on.URL("http://x/y?token=t"); strings.Contains(got, "%") {
		t.Errorf("the mask was percent-encoded: %q", got)
	}
}
