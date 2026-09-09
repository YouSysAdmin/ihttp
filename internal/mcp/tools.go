package mcp

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yousysadmin/ihttp/internal/core/filter"
	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
	reqlogmodels "github.com/yousysadmin/ihttp/internal/models/reqlog"
)

// Options tune the tool set.
type Options struct {
	// AllowWrite adds the tools that change something: opening a
	// project, sending a request, answering a held exchange. Off by
	// default, so an agent given this server can look and not touch.
	AllowWrite bool

	// Redact masks credentials on the way out. On by default.
	Redact bool
}

// Tools builds the tool set for a client.
func Tools(c *Client, opts Options) []Tool {
	r := Redactor{On: opts.Redact}

	tools := []Tool{
		infoTool(c),
		projectsListTool(c),
		logSearchTool(c, r),
		logGetTool(c, r),
		logBodyTool(c),
		logCurlTool(c),
		rulesListTool(c),
		interceptListTool(c, r),
		filterHelpTool(),
	}

	if opts.AllowWrite {
		tools = append(tools,
			projectOpenTool(c),
			senderSendTool(c, r),
			interceptForwardTool(c),
			interceptDropTool(c),
		)
	}

	return tools
}

// schema is a JSON Schema written out as it is. Small enough to read,
// and it saves a dependency on a generator.
func schema(s string) jsontext.Value {
	return jsontext.Value(s)
}

const noArgs = `{"type":"object","properties":{},"additionalProperties":false}`

// ---------- read ----------

func infoTool(c *Client) Tool {
	return Tool{
		Name: "info",
		Description: "What this ihttp is: its version, the address of its proxy, where its data " +
			"lives, which project is open, and which upstream proxy it goes out through. " +
			"Call this first - most other tools need a project to be open.",
		InputSchema: schema(noArgs),
		Call: func(ctx context.Context, _ jsontext.Value) (string, error) {
			var info struct {
				Version         string   `json:"version"`
				ProxyURL        string   `json:"proxy_url"`
				ConsoleURL      string   `json:"console_url"`
				DataDir         string   `json:"data_dir"`
				ActiveProjectID string   `json:"active_project_id"`
				UpstreamProxy   string   `json:"upstream_proxy"`
				UpstreamBypass  []string `json:"upstream_bypass"`
			}

			if err := c.get(ctx, "/info", nil, &info); err != nil {
				return "", err
			}

			var b strings.Builder

			fmt.Fprintf(&b, "ihttp %s\nproxy: %s\nconsole: %s\ndata: %s\n",
				info.Version, info.ProxyURL, info.ConsoleURL, info.DataDir)

			if info.ActiveProjectID == "" {
				b.WriteString("project: none open - nothing is being logged. " +
					"Open one from projects_list before searching.\n")
			} else {
				fmt.Fprintf(&b, "project: %s is open\n", info.ActiveProjectID)
			}

			if info.UpstreamProxy != "" {
				fmt.Fprintf(&b, "goes out through: %s\n", info.UpstreamProxy)
			}

			return b.String(), nil
		},
	}
}

func projectsListTool(c *Client) Tool {
	return Tool{
		Name:        "projects_list",
		Description: "The projects on this instance, and which one is open. A project owns its own request log.",
		InputSchema: schema(noArgs),
		Call: func(ctx context.Context, _ jsontext.Value) (string, error) {
			var out struct {
				Projects []struct {
					ID       string `json:"id"`
					Name     string `json:"name"`
					IsActive bool   `json:"is_active"`
				} `json:"projects"`
			}

			if err := c.get(ctx, "/projects", nil, &out); err != nil {
				return "", err
			}

			if len(out.Projects) == 0 {
				return "No projects yet.", nil
			}

			var b strings.Builder
			for _, p := range out.Projects {
				open := ""
				if p.IsActive {
					open = "  <- open"
				}

				fmt.Fprintf(&b, "%s  %s%s\n", p.ID, p.Name, open)
			}

			return b.String(), nil
		},
	}
}

func logSearchTool(c *Client, r Redactor) Tool {
	return Tool{
		Name: "log_search",
		Description: "Search the open project's request log with ihttp's filter query language and " +
			"get one line per exchange. This is the tool that makes the log useful: a query " +
			"answers a question instead of scrolling. Call filter_help for the full vocabulary. " +
			"Examples: `res.statusCode >= 500`, `req.host = api.example.com AND req.method = POST`, " +
			"`res.ttfb > 1s AND res.receive < 50ms` (the server was slow, not the network), " +
			"`req.header.authorization exists`, `res.type = json AND res.size > 100kb`. " +
			"An empty query returns the most recent exchanges.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"query":{"type":"string","description":"A filter query. Empty for the most recent."},` +
			`"limit":{"type":"integer","description":"How many, 1 to 200. Default 20.","minimum":1,"maximum":200},` +
			`"only_in_scope":{"type":"boolean","description":"Keep to the project's scope rules."}` +
			`},"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			var in struct {
				Query       string `json:"query"`
				Limit       int    `json:"limit"`
				OnlyInScope bool   `json:"only_in_scope"`
			}

			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}

			if in.Limit <= 0 {
				in.Limit = 20
			}

			q := url.Values{}
			q.Set("limit", strconv.Itoa(min(in.Limit, 200)))

			if in.Query != "" {
				q.Set("search", in.Query)
			}

			if in.OnlyInScope {
				q.Set("only_in_scope", "true")
			}

			var out struct {
				Entries []reqlogmodels.Summary `json:"entries"`
				More    bool                   `json:"more"`
			}

			if err := c.get(ctx, "/request-logs", q, &out); err != nil {
				return "", err
			}

			if len(out.Entries) == 0 {
				return "Nothing matches. An empty query returns the most recent exchanges, " +
					"call info to check a project is open and traffic is being logged.", nil
			}

			var b strings.Builder
			for _, e := range out.Entries {
				status := "pending"
				if e.StatusCode != 0 {
					status = strconv.Itoa(e.StatusCode)
				}

				fmt.Fprintf(&b, "%s  %s  %-7s %s  %s",
					e.ID, e.CreatedAt.Format(time.RFC3339), e.Method, status, r.URL(e.URL))

				if e.DurationMS > 0 {
					fmt.Fprintf(&b, "  %dms", e.DurationMS)
				}

				if len(e.Tags) > 0 {
					fmt.Fprintf(&b, "  [%s]", strings.Join(e.Tags, " "))
				}

				b.WriteString("\n")
			}

			if out.More {
				b.WriteString("(more match: narrow the query or raise the limit)\n")
			}

			b.WriteString(r.Note())

			return b.String(), nil
		},
	}
}

func logGetTool(c *Client, r Redactor) Tool {
	return Tool{
		Name: "log_get",
		Description: "One exchange in full: the request line, both sets of headers, the timing " +
			"breakdown, and as much of each body as is text. Ids come from log_search. " +
			"Use log_body for a body that was cut short here, and log_curl to hand it to a human.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The exchange id from log_search."}` +
			`},"required":["id"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			var in struct {
				ID string `json:"id"`
			}

			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}

			if in.ID == "" {
				return "", fmt.Errorf("an exchange id is needed - get one from log_search")
			}

			var out struct {
				Entry reqlogmodels.Entry `json:"entry"`
			}

			if err := c.get(ctx, "/request-logs/"+url.PathEscape(in.ID), nil, &out); err != nil {
				return "", err
			}

			e := out.Entry

			var b strings.Builder

			fmt.Fprintf(&b, "%s %s %s\n", e.Method, r.URL(e.URL), e.Proto)
			writeHeaders(&b, "request headers", r.Headers(e.Headers))
			writeBody(&b, "request body", e.Body, e.BodyBinary, e.BodyTruncated, in.ID, "request")

			if e.Response == nil {
				b.WriteString("\nNo response yet.\n")

				return b.String(), nil
			}

			res := e.Response

			fmt.Fprintf(&b, "\n%s %d %s  in %dms\n", res.Proto, res.StatusCode, res.Status, res.DurationMS)

			if t := res.Timings; t != nil {
				fmt.Fprintf(&b, "timing: dns %.1f  connect %.1f  tls %.1f  send %.1f  wait %.1f  receive %.1f ms",
					t.DNS, t.Connect, t.TLS, t.Send, t.Wait, t.Receive)

				if t.Reused {
					b.WriteString("  (reused connection, so no dns/connect/tls)")
				}

				if t.ServerAddr != "" {
					fmt.Fprintf(&b, "  from %s", t.ServerAddr)
				}

				b.WriteString("\n")
			}

			writeHeaders(&b, "response headers", r.Headers(res.Headers))

			if len(res.Trailers) > 0 {
				writeHeaders(&b, "response trailers", r.Headers(res.Trailers))
			}

			if res.BodyStreamed {
				b.WriteString("\nresponse body: streamed to the client and not captured\n")
			} else {
				writeBody(&b, "response body", res.Body, res.BodyBinary, res.BodyTruncated, in.ID, "response")
			}

			b.WriteString(r.Note())

			return b.String(), nil
		},
	}
}

func logBodyTool(c *Client) Tool {
	return Tool{
		Name: "log_body",
		Description: "One side's body as it was captured, up to 256 KiB. Use this when log_get cut " +
			"a body short. A binary body comes back described rather than dumped, since bytes " +
			"are not much use in a conversation.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The exchange id."},` +
			`"side":{"type":"string","enum":["request","response"],"description":"Which half."}` +
			`},"required":["id","side"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			var in struct {
				ID   string `json:"id"`
				Side string `json:"side"`
			}

			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}

			if in.Side != "request" && in.Side != "response" {
				return "", fmt.Errorf("side must be request or response")
			}

			media, data, err := c.raw(ctx, "/request-logs/"+url.PathEscape(in.ID)+"/body/"+in.Side, maxBody)
			if err != nil {
				return "", err
			}

			if len(data) == 0 {
				return "The body is empty.", nil
			}

			if httpmsg.IsBinary(media, data) {
				return fmt.Sprintf("%s, %d bytes of binary data, not shown. "+
					"Read it in the console at the raw endpoint if you need the bytes.", media, len(data)), nil
			}

			return fmt.Sprintf("%s, %d bytes:\n%s", media, len(data), data), nil
		},
	}
}

func logCurlTool(c *Client) Tool {
	return Tool{
		Name: "log_curl",
		Description: "The exchange as a curl command, and as a fetch() call. This is how to hand a " +
			"captured request back to a human so they can run it themselves. Never redacted: a " +
			"command with a masked token would not work.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The exchange id."}` +
			`},"required":["id"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			var in struct {
				ID string `json:"id"`
			}

			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}

			var out struct {
				Curl  string `json:"curl"`
				Fetch string `json:"fetch"`
			}

			if err := c.get(ctx, "/request-logs/"+url.PathEscape(in.ID)+"/snippets", nil, &out); err != nil {
				return "", err
			}

			return fmt.Sprintf("curl:\n%s\n\nfetch:\n%s\n\n(not redacted, so it runs as captured - "+
				"treat it as carrying whatever credentials the original did)", out.Curl, out.Fetch), nil
		},
	}
}

func rulesListTool(c *Client) Tool {
	return Tool{
		Name: "rules_list",
		Description: "The open project's traffic rules: what the proxy is doing to matching traffic " +
			"on the operator's behalf - mocking, blocking, rewriting, delaying. Worth reading " +
			"before concluding a response came from the real backend.",
		InputSchema: schema(noArgs),
		Call: func(ctx context.Context, _ jsontext.Value) (string, error) {
			var out struct {
				Settings struct {
					Rules []struct {
						Enabled bool   `json:"enabled"`
						Name    string `json:"name"`
						URL     string `json:"url"`
						Method  string `json:"method"`
						Filter  string `json:"filter"`
						Action  struct {
							Type string `json:"type"`
						} `json:"action"`
					} `json:"rules"`
				} `json:"settings"`
			}

			if err := c.get(ctx, "/project/settings", nil, &out); err != nil {
				return "", err
			}

			rules := out.Settings.Rules
			if len(rules) == 0 {
				return "No rules: every response came from the real upstream.", nil
			}

			var b strings.Builder
			for _, rule := range rules {
				state := "off"
				if rule.Enabled {
					state = "on "
				}

				match := strings.TrimSpace(strings.Join([]string{rule.Method, rule.URL, rule.Filter}, " "))
				if match == "" {
					match = "every request"
				}

				fmt.Fprintf(&b, "%s  %-18s %s  <- %s\n", state, rule.Action.Type, match, rule.Name)
			}

			return b.String(), nil
		},
	}
}

func interceptListTool(c *Client, r Redactor) Tool {
	return Tool{
		Name: "intercept_list",
		Description: "Exchanges held at the proxy, waiting to be let through. A held request has " +
			"stopped a real client, so answer it or say so.",
		InputSchema: schema(noArgs),
		Call: func(ctx context.Context, _ jsontext.Value) (string, error) {
			var out struct {
				Items []struct {
					ID      string `json:"id"`
					Kind    string `json:"kind"`
					Request struct {
						Method string `json:"method"`
						URL    string `json:"url"`
					} `json:"request"`
					Response *struct {
						StatusCode int `json:"status_code"`
					} `json:"response"`
				} `json:"items"`
			}

			if err := c.get(ctx, "/intercept/items", nil, &out); err != nil {
				return "", err
			}

			if len(out.Items) == 0 {
				return "Nothing is held.", nil
			}

			var b strings.Builder
			for _, it := range out.Items {
				status := ""
				if it.Response != nil {
					status = fmt.Sprintf("  status %d", it.Response.StatusCode)
				}

				fmt.Fprintf(&b, "%s  %s  %s %s%s\n", it.ID, it.Kind, it.Request.Method, r.URL(it.Request.URL), status)
			}

			return b.String(), nil
		},
	}
}

// filterHelpTool is the vocabulary itself. An agent that has this does
// not have to guess at field names, and a guessed field is a 400.
func filterHelpTool() Tool {
	return Tool{
		Name: "filter_help",
		Description: "The filter query language log_search takes: every field, every operator, and " +
			"worked examples. Read this before writing a query with a field you have not used.",
		InputSchema: schema(noArgs),
		Call: func(context.Context, jsontext.Value) (string, error) {
			return filter.Help, nil
		},
	}
}

// ---------- write ----------

func projectOpenTool(c *Client) Tool {
	return Tool{
		Name: "project_open",
		Description: "Open a project, which is what its log can then be searched. Only one is open " +
			"at a time, and opening another changes what the proxy logs from that moment - so " +
			"say what you are doing before you do it.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The project id from projects_list."}` +
			`},"required":["id"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			var in struct {
				ID string `json:"id"`
			}

			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}

			var out struct {
				Project struct {
					Name string `json:"name"`
				} `json:"project"`
			}

			if err := c.post(ctx, "/projects/"+url.PathEscape(in.ID)+"/open", nil, &out); err != nil {
				return "", err
			}

			return fmt.Sprintf("Opened %s. From now on the proxy logs into this project.", out.Project.Name), nil
		},
	}
}

func senderSendTool(c *Client, r Redactor) Tool {
	return Tool{
		Name: "sender_send",
		Description: "Re-send a captured exchange, optionally with a different method, URL, headers " +
			"or body, and read the answer. The request really goes out. Use it to check whether a " +
			"fix worked, not to explore.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"from_log_id":{"type":"string","description":"The exchange to copy, from log_search."},` +
			`"method":{"type":"string","description":"Override the method."},` +
			`"url":{"type":"string","description":"Override the URL."},` +
			`"body":{"type":"string","description":"Override the body."}` +
			`},"required":["from_log_id"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			var in struct {
				FromLogID string `json:"from_log_id"`
				Method    string `json:"method"`
				URL       string `json:"url"`
				Body      string `json:"body"`
			}

			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}

			if in.FromLogID == "" {
				return "", fmt.Errorf("from_log_id is needed - copy an exchange from log_search")
			}

			var cloned struct {
				Request struct {
					ID string `json:"id"`
				} `json:"request"`
			}

			if err := c.post(ctx, "/sender/clone/"+url.PathEscape(in.FromLogID), nil, &cloned); err != nil {
				return "", err
			}

			id := cloned.Request.ID

			// Only what was asked for is changed: the clone already
			// carries the original's method, URL, headers and body.
			if in.Method != "" || in.URL != "" || in.Body != "" {
				var current struct {
					Request struct {
						Method  string          `json:"method"`
						URL     string          `json:"url"`
						Proto   string          `json:"proto"`
						Headers httpmsg.Headers `json:"headers"`
						Body    httpmsg.Body    `json:"body"`
					} `json:"request"`
				}

				if err := c.get(ctx, "/sender/requests/"+url.PathEscape(id), nil, &current); err != nil {
					return "", err
				}

				edit := map[string]any{
					"id":      id,
					"method":  firstNonEmpty(in.Method, current.Request.Method),
					"url":     firstNonEmpty(in.URL, current.Request.URL),
					"proto":   current.Request.Proto,
					"headers": current.Request.Headers,
				}

				if in.Body != "" {
					edit["body"] = in.Body
				} else {
					edit["body"] = current.Request.Body
				}

				if err := c.post(ctx, "/sender/requests", edit, nil); err != nil {
					return "", err
				}
			}

			var sent struct {
				Request struct {
					Method   string `json:"method"`
					URL      string `json:"url"`
					Response *struct {
						StatusCode int             `json:"status_code"`
						Status     string          `json:"status"`
						Headers    httpmsg.Headers `json:"headers"`
						Body       httpmsg.Body    `json:"body"`
						BodyBinary bool            `json:"body_binary"`
						DurationMS int64           `json:"duration_ms"`
					} `json:"response"`
				} `json:"request"`
			}

			if err := c.post(ctx, "/sender/requests/"+url.PathEscape(id)+"/send", nil, &sent); err != nil {
				return "", err
			}

			res := sent.Request.Response
			if res == nil {
				return "The request was sent but no response was recorded.", nil
			}

			var b strings.Builder

			fmt.Fprintf(&b, "%s %s\n-> %d %s in %dms\n",
				sent.Request.Method, r.URL(sent.Request.URL), res.StatusCode, res.Status, res.DurationMS)
			writeHeaders(&b, "response headers", r.Headers(res.Headers))
			writeBody(&b, "response body", res.Body, res.BodyBinary, false, "", "")
			b.WriteString(r.Note())

			return b.String(), nil
		},
	}
}

func interceptForwardTool(c *Client) Tool {
	return Tool{
		Name:        "intercept_forward",
		Description: "Let a held exchange through unchanged. A real client is waiting on it.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The held item's id from intercept_list."},` +
			`"kind":{"type":"string","enum":["request","response"],"description":"Which half is held."}` +
			`},"required":["id","kind"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			return answerIntercept(ctx, c, args, "forward")
		},
	}
}

func interceptDropTool(c *Client) Tool {
	return Tool{
		Name: "intercept_drop",
		Description: "Refuse a held exchange. The client is answered with a 502, so only do this " +
			"when the human asked for it.",
		InputSchema: schema(`{"type":"object","properties":{` +
			`"id":{"type":"string","description":"The held item's id from intercept_list."},` +
			`"kind":{"type":"string","enum":["request","response"],"description":"Which half is held."}` +
			`},"required":["id","kind"],"additionalProperties":false}`),
		Call: func(ctx context.Context, args jsontext.Value) (string, error) {
			return answerIntercept(ctx, c, args, "drop")
		},
	}
}

func answerIntercept(ctx context.Context, c *Client, args jsontext.Value, verb string) (string, error) {
	var in struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}

	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}

	if in.Kind != "request" && in.Kind != "response" {
		return "", fmt.Errorf("kind must be request or response")
	}

	path := fmt.Sprintf("/intercept/%ss/%s/%s", in.Kind, url.PathEscape(in.ID), verb)

	body := any(nil)
	if verb == "forward" {
		// An empty object forwards what was held, unchanged.
		body = map[string]any{}
	}

	if err := c.post(ctx, path, body, nil); err != nil {
		return "", err
	}

	return fmt.Sprintf("The held %s was %sed.", in.Kind, verb), nil
}

// ---------- shared formatting ----------

func writeHeaders(b *strings.Builder, label string, hs httpmsg.Headers) {
	if len(hs) == 0 {
		return
	}

	fmt.Fprintf(b, "%s:\n", label)

	for _, h := range hs {
		fmt.Fprintf(b, "  %s: %s\n", h.Name, h.Value)
	}
}

// bodyInline is how much of a body log_get shows before pointing at
// log_body. Enough to read a JSON error, short enough not to bury the
// headers above it.
const bodyInline = 4 << 10

func writeBody(b *strings.Builder, label string, body httpmsg.Body, binary, truncated bool, id, side string) {
	if len(body) == 0 {
		return
	}

	if binary {
		fmt.Fprintf(b, "%s: %d bytes of binary data, not shown\n", label, len(body))

		return
	}

	text := string(body)
	cut := len(text) > bodyInline

	if cut {
		text = text[:bodyInline]
	}

	fmt.Fprintf(b, "%s (%d bytes):\n%s\n", label, len(body), text)

	switch {
	case truncated:
		fmt.Fprintf(b, "(the proxy captured only the first part of this body)\n")
	case cut && id != "":
		fmt.Fprintf(b, "(shown to %d bytes - call log_body with id %s and side %s for the rest)\n", bodyInline, id, side)
	case cut:
		fmt.Fprintf(b, "(shown to %d bytes)\n", bodyInline)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
