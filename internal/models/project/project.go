// Package project is the stored shape of a project and its settings.
package project

import "time"

// Project is one unit of work: a request log, a sender history and the
// settings that decide what the proxy does while it is open.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Settings  Settings  `json:"settings"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IsActive  bool      `json:"is_active"`
}

// Settings is the JSON document kept beside a project. Filters are kept
// as their SOURCE text and compiled when the project is opened, so the
// stored form does not depend on any version of the filter package.
type Settings struct {
	RequestLog RequestLogSettings `json:"request_log"`
	Intercept  InterceptSettings  `json:"intercept"`
	Scope      []ScopeRule        `json:"scope"`
	Rules      []Rule             `json:"rules"`
	Views      []View             `json:"views,omitempty"`

	// NoDecrypt are host globs the proxy relays WITHOUT decrypting: the
	// bytes go between client and upstream and nobody in the middle
	// sees them. For a client that pins its server's certificate and
	// would otherwise refuse to connect at all, and for traffic that
	// has no business being in a log.
	//
	// A project decision, because what a project may see is the
	// project's business. The connection is still logged as a CONNECT,
	// with the host, the duration and the bytes each way - passthrough
	// is not the same as invisible.
	NoDecrypt []string `json:"no_decrypt,omitempty"`

	// Upstream is which of the instance's proxies this project goes out
	// through: empty to inherit the instance default, UpstreamDirect to
	// go out on our own whatever the default is, or the id of one in the
	// list. Switching project switches the way out, with no restart.
	//
	// A REFERENCE, never a URL: an export carries this member, and a
	// URL here would carry our credentials to whoever we shared the
	// project with. An id that means nothing on another machine falls
	// back to that machine's default.
	Upstream string `json:"upstream,omitempty"`

	// HostOverrides are the hosts this project dials somewhere of the
	// operator's choosing: a hosts file for the proxy, so a lab address
	// does not have to go into dnsmasq on every machine a client runs
	// on. First match wins, so a specific entry above a wildcard sends
	// one host in a domain elsewhere.
	HostOverrides []HostOverride `json:"host_overrides,omitempty"`
}

// HostOverride moves where a host is REACHED and nothing else. The Host
// header, the SNI and the certificate check all come from the URL,
// which is untouched, so the target sees exactly the request it would
// have seen - which is what makes this different from a rewrite_url
// rule, where the name moves with the connection and the server knows.
type HostOverride struct {
	// Host is a host glob, the same shape NoDecrypt and the upstream
	// bypass list take: api.example.com, *.example.com. No port.
	Host string `json:"host"`

	// Address is where it is dialled: an IP, optionally with a port.
	// 10.0.0.5 keeps the port the request asked for, 10.0.0.5:8443
	// replaces that too.
	Address string `json:"address"`

	// Enabled off keeps an override without applying it, so a target can
	// be compared against the real host without retyping the address.
	Enabled bool `json:"enabled"`
}

// MaxHostOverrides bounds the list. A hosts file for one engagement,
// not a zone.
const MaxHostOverrides = 200

// UpstreamDirect is the Settings.Upstream value that means "out on our
// own", as against an empty value, which means "whatever the instance
// was started with". A reserved word rather than an id, and no id can
// collide with it since ids are UUIDs.
const UpstreamDirect = "direct"

// View is a named way of looking at the log: a filter query, whether to
// keep to the scope, and which of the two lists it reads. Saved on the
// project, so a set of investigations survives a restart and travels
// with a settings-only export.
//
// The query is SOURCE text like every other filter here, compiled when
// it is used. A view narrows what is shown and nothing else - it never
// decides what is logged.
type View struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Query       string `json:"query"`
	OnlyInScope bool   `json:"only_in_scope"`

	// Saved reads the saved list rather than the log.
	Saved bool `json:"saved"`
}

// MaxViews bounds the list, so a picker stays a picker.
const MaxViews = 20

// RequestLogSettings decides what the proxy writes to the log.
type RequestLogSettings struct {
	// Paused stops the log while the proxy keeps forwarding, so a client
	// under test never loses its connection while what was already
	// captured is read. Nothing is deleted and nothing else changes:
	// rules, intercept and the scope go on as before.
	Paused bool `json:"paused"`

	// BypassOutOfScope skips logging for requests no scope rule matches.
	BypassOutOfScope bool `json:"bypass_out_of_scope"`

	// IgnoreFilter is a filter over the REQUEST alone: what matches is
	// not logged. The response is not known when the decision is made, so
	// res.* keys are refused.
	IgnoreFilter string `json:"ignore_filter"`

	// MutedHosts are hosts hidden from the LOG VIEW while still being
	// captured: the entries are written, kept, exported and reachable
	// by a filter that names the host. For a noisy host you are not
	// working on but may want to look at later.
	//
	// The opposite of IgnoreFilter, which refuses to write at all - what
	// was never written cannot be un-ignored.
	MutedHosts []string `json:"muted_hosts,omitempty"`

	// MaxEntries caps the log: past it the oldest entries are dropped as
	// new ones arrive. Zero is no cap, and the default. Saved entries are never dropped - that is what saving
	// an entry is for.
	MaxEntries int `json:"max_entries,omitzero"`
}

// MaxEntriesLimit bounds what the cap itself may be set to. A cap is a
// guard against a session that runs for days, not a way to ask for a
// hundred million rows in one bbolt file.
const MaxEntriesLimit = 10_000_000

// InterceptSettings decides what the proxy holds for review.
type InterceptSettings struct {
	RequestsEnabled  bool   `json:"requests_enabled"`
	ResponsesEnabled bool   `json:"responses_enabled"`
	RequestFilter    string `json:"request_filter"`
	ResponseFilter   string `json:"response_filter"`
}

// ScopeRule is one way a request may be in scope. Every field is a
// regular expression source, and an empty one is "not tested". A rule
// with a header key and a header value requires both to match on one
// header.
type ScopeRule struct {
	URL         string `json:"url"`
	HeaderKey   string `json:"header_key"`
	HeaderValue string `json:"header_value"`
	Body        string `json:"body"`
}

// IsEmpty reports a rule that tests nothing, which the API refuses.
func (r ScopeRule) IsEmpty() bool {
	return r.URL == "" && r.HeaderKey == "" && r.HeaderValue == "" && r.Body == ""
}

// Rule is one thing the proxy does to traffic that matches: mock a
// response, serve a local file, rewrite the URL, add or drop a header,
// change the status, edit the body, add a delay. Rules run in order and
// every matched rule applies to the exchange, even after one answers
// instead of the upstream. Enabled off keeps a rule without applying it.
type Rule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`

	// URL is a regular expression over the whole request URL. Method
	// narrows further, empty is any method.
	URL    string `json:"url"`
	Method string `json:"method"`

	// Filter is a query in the same language the log and the intercept
	// take, decided on the REQUEST alone - a rule runs before the
	// response exists, so res.* and ws.* keys are refused. It narrows
	// further: a rule with a URL, a method and a filter needs all three.
	//
	// This is what makes a rule as targetable as an intercept filter -
	// `req.header.authorization exists`, `req.body contains password`,
	// `req.size > 1mb` - without a second matching language.
	Filter string `json:"filter,omitempty"`

	Action RuleAction `json:"action"`
}

// RuleActionType names what a rule does.
type RuleActionType string

// The actions. Only the fields the type needs are read.
const (
	// ActionMock answers with Status, Headers and Body, never calling the
	// upstream.
	ActionMock RuleActionType = "mock"

	// ActionMapLocal answers with the file at Path, never calling the
	// upstream. The content type follows the extension.
	ActionMapLocal RuleActionType = "map_local"

	// ActionRewriteURL replaces what Pattern matches in the URL with
	// Replace before the request leaves - point a production host at a
	// local server. An empty Pattern is the rule's own URL match.
	ActionRewriteURL RuleActionType = "rewrite_url"

	// ActionSetRequestHeader and friends set the Headers, or remove the
	// ones named in Headers, on the request or the response.
	ActionSetRequestHeader     RuleActionType = "set_request_header"
	ActionRemoveRequestHeader  RuleActionType = "remove_request_header"
	ActionSetResponseHeader    RuleActionType = "set_response_header"
	ActionRemoveResponseHeader RuleActionType = "remove_response_header"

	// ActionSetStatus overrides the response status code.
	ActionSetStatus RuleActionType = "set_status"

	// ActionReplaceBody applies Replacements to the response body, in
	// order. ActionReplaceRequestBody does the same to the request body.
	ActionReplaceBody        RuleActionType = "replace_body"
	ActionReplaceRequestBody RuleActionType = "replace_request_body"

	// ActionDelay waits DelayMS before the request leaves.
	ActionDelay RuleActionType = "delay"

	// ActionDelayResponse waits DelayMS before the response reaches the
	// client. ActionDelay holds the request back, which is what a slow
	// backend looks like. This holds the answer back, which is what a
	// slow link looks like to a client that has already sent everything.
	ActionDelayResponse RuleActionType = "delay_response"

	// ActionBlock answers with Status - 502 when it is not set - instead
	// of reaching the upstream, so an endpoint can be made to fail by
	// pattern rather than by hand in the intercept queue. The exchange
	// is logged like any other.
	ActionBlock RuleActionType = "block"

	// ActionThrottle paces the response body at RateBPS bytes a second.
	// Unlike a delay it works on a streamed body too, since it wraps the
	// reader rather than waiting before it.
	ActionThrottle RuleActionType = "throttle"

	// ActionAllowCORS answers preflights and adds permissive CORS headers
	// to every response, for a frontend on one origin talking to an API
	// on another.
	ActionAllowCORS RuleActionType = "allow_cors"

	// ActionCapture reads a value out of an exchange and remembers it
	// under a name, so LATER rules can put it into a request as
	// ${name}. From is a filter field - res.header.set-cookie,
	// res.body, req.query.id - and Pattern narrows it further.
	//
	// This is what carries a token from a sign-in response into the
	// requests that need it, which no other action can do: every other
	// one writes the text you typed, and this one writes what the
	// traffic said.
	ActionCapture RuleActionType = "capture"
)

// Header is one header of a rule action: a name and value for the mock
// and the set actions, a name alone for the remove actions.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Replacement is one regular expression and what replaces its matches,
// $1-style groups allowed.
type Replacement struct {
	Pattern string `json:"pattern"`
	Replace string `json:"replace"`
}

// RuleAction is what a rule does, with the fields its Type reads.
type RuleAction struct {
	Type RuleActionType `json:"type"`

	// Header and Value are the single header of an older rule. Normalize
	// moves them into Headers, so nothing else reads them.
	Header string `json:"header,omitempty"`
	Value  string `json:"value,omitempty"`

	// Mock and set_status.
	Status int `json:"status,omitzero"`

	// Headers to answer with (mock), to set (set_*_header) or the names
	// to drop (remove_*_header).
	Headers []Header `json:"headers,omitempty"`

	// Mock body.
	Body string `json:"body,omitempty"`

	// Map local.
	Path string `json:"path,omitempty"`

	// Rewrite URL: a regular expression over the URL, empty for the
	// rule's URL match, and its replacement. For the body actions the
	// pair is the single replacement of an older rule, which Normalize
	// moves into Replacements.
	Pattern string `json:"pattern,omitempty"`
	Replace string `json:"replace,omitempty"`

	// Replacements for the body actions, applied in order.
	Replacements []Replacement `json:"replacements,omitempty"`

	// Delay and delay_response.
	DelayMS int `json:"delay_ms,omitzero"`

	// Throttle: bytes a second the response body is paced at.
	RateBPS int `json:"rate_bps,omitzero"`

	// Capture: which field to read and what to remember it as. From is
	// a key of the filter language, so there is one vocabulary for
	// naming a part of an exchange. Pattern above narrows the value:
	// its first group when it has one, the whole match when it does
	// not, and the field as it stands when there is no pattern.
	From string `json:"from,omitempty"`
	Name string `json:"name,omitempty"`
}

// Normalize moves the single header or replacement of an older rule into
// the list the action reads, so a rule saved before the lists existed
// keeps working and is stored in the current shape on its next save.
func (a *RuleAction) Normalize() {
	switch a.Type {
	case ActionSetRequestHeader, ActionSetResponseHeader,
		ActionRemoveRequestHeader, ActionRemoveResponseHeader:
		if a.Header != "" {
			a.Headers = append([]Header{{Name: a.Header, Value: a.Value}}, a.Headers...)
		}

		a.Header, a.Value = "", ""
	case ActionReplaceBody, ActionReplaceRequestBody:
		if a.Pattern != "" {
			a.Replacements = append([]Replacement{{Pattern: a.Pattern, Replace: a.Replace}}, a.Replacements...)
		}

		a.Pattern, a.Replace = "", ""
	}
}
