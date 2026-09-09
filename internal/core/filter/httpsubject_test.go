package filter

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func exchange() HTTPSubject {
	u, _ := url.Parse("https://api.example.com:8443/v2/users/7.json?page=2&sort=asc")

	return NewHTTPSubject(Request{
		ID:     "id1",
		Method: "POST",
		URL:    u,
		Proto:  "HTTP/2.0",
		Header: http.Header{
			"Content-Type":  {"application/x-www-form-urlencoded"},
			"Cookie":        {"session=abc; theme=dark"},
			"Authorization": {"Bearer x"},
		},
		Body:      []byte("user=admin&password=Secret1"),
		Timestamp: time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC),
	}, &Response{
		StatusCode: 404,
		Status:     "Not Found",
		Proto:      "HTTP/2.0",
		Header:     http.Header{"Content-Type": {"application/json; charset=utf-8"}, "Set-Cookie": {"sid=zzz; Path=/; HttpOnly"}},
		Body:       []byte(`{"error":"nope"}`),
		DurationMS: 2500,
	})
}

func TestHTTPSubjectFields(t *testing.T) {
	s := exchange()

	cases := map[string]bool{
		`req.host = api.example.com`:                  true,
		`req.host = API.EXAMPLE.COM`:                  true,
		`req.port = 8443`:                             true,
		`req.scheme = https`:                          true,
		`req.path = /v2/users/7.json`:                 true,
		`req.ext = json`:                              true,
		`req.query contains "sort=asc"`:               true,
		`req.query.page = 2`:                          true,
		`req.query.missing exists`:                    false,
		`req.query.page exists`:                       true,
		`req.form.password contains secret`:           true,
		`req.form.user = admin`:                       true,
		`req.body contains PASSWORD`:                  true,
		`req.header.authorization exists`:             true,
		`req.header.x-nope exists`:                    false,
		`req.header.Content-Type contains urlencoded`: true,
		`req.cookie.session = abc`:                    true,
		`req.cookie.theme in (light, dark)`:           true,
		`req.method in (post, put)`:                   true,
		`req.method in (GET)`:                         false,
		`req.size > 10`:                               true,
		`res.statusCode in (400, 404)`:                true,
		`res.type = json`:                             true,
		`res.mime = application/json`:                 true,
		`res.size < 1kb`:                              true,
		`res.duration > 2s`:                           true,
		`res.duration > 3s`:                           false,
		`res.cookie.sid = zzz`:                        true,
		`res.cookie.sid exists`:                       true,
		`res.header.content-type contains json`:       true,
		`res.headers exists`:                          true,
		`req.timestamp > "2026-09-05T19:00:00Z"`:      true,
		`NOT res.type in (image, font, css)`:          true,
		`secret1`:                                     true,
	}

	for in, want := range cases {
		got, err := Match(mustParse(t, in), s)
		if err != nil {
			t.Fatalf("Match(%q): %v", in, err)
		}

		if got != want {
			t.Errorf("Match(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestHTTPSubjectWithoutResponse(t *testing.T) {
	s := exchange()
	s.Res = nil

	for _, in := range []string{`res.statusCode = ""`, `res.type = ""`, `NOT res.header.x exists`} {
		if got, err := Match(mustParse(t, in), s); err != nil || !got {
			t.Errorf("Match(%q) = %v, %v on a request alone", in, got, err)
		}
	}

	if _, err := Match(mustParse(t, `req.nope exists`), s); err == nil {
		t.Fatal("an unknown key must be reported under exists too")
	}
}

func TestBodyType(t *testing.T) {
	cases := []struct {
		ct, body, want string
	}{
		{"application/json", "{}", "json"},
		{"text/html; charset=utf-8", "<p>", "html"},
		{"image/png", "\x89PNG", "image"},
		{"application/octet-stream", "\x00\x01", "binary"},
		{"text/plain", "hi", "text"},
		{"font/woff2", "wOF2", "font"},
		{"", "", ""},
	}

	for _, c := range cases {
		if got := BodyType(c.ct, []byte(c.body)); got != c.want {
			t.Errorf("BodyType(%q) = %q, want %q", c.ct, got, c.want)
		}
	}
}

func TestHTTPSubjectMarks(t *testing.T) {
	s := exchange()
	s.Req.Tags = []string{"todo", "Auth-Bug"}
	s.Req.Note = "check the token refresh"
	s.Req.Color = "red"

	cases := map[string]bool{
		`req.tag = todo`:                     true,
		`req.tag = TODO`:                     true,
		`req.tag = done`:                     false,
		`req.tag != done`:                    true,
		`req.tag != todo`:                    false,
		`req.tag in (done, auth-bug)`:        true,
		`req.tag contains bug`:               true,
		`req.tag =~ "^auth"`:                 false,
		`req.tag =~ "(?i)^auth"`:             true,
		`req.tag exists`:                     true,
		`req.tags exists`:                    true,
		`req.note contains "token"`:          true,
		`req.color = RED`:                    true,
		`req.color in (blue, red)`:           true,
		`refresh`:                            true,
		`auth-bug`:                           true,
		`req.tag = todo AND req.color = red`: true,
	}

	for q, want := range cases {
		e, err := Parse(q)
		if err != nil {
			t.Fatalf("Parse(%q): %v", q, err)
		}

		got, err := Match(e, s)
		if err != nil {
			t.Fatalf("Match(%q): %v", q, err)
		}

		if got != want {
			t.Errorf("Match(%q) = %v, want %v", q, got, want)
		}
	}

	bare := exchange()
	if got, _ := Match(mustParse(t, `req.tag exists`), bare); got {
		t.Error("an entry without tags answers exists")
	}

	if _, err := Match(mustParse(t, `req.tag > 3`), s); err == nil {
		t.Error("ordering over a list was accepted")
	}
}

func mustParse(t *testing.T, q string) Expr {
	t.Helper()

	e, err := Parse(q)
	if err != nil {
		t.Fatal(err)
	}

	return e
}

func TestHTTPSubjectWebSocketKeys(t *testing.T) {
	u, _ := url.Parse("wss://chat.example.com/socket")
	s := NewHTTPSubject(Request{
		Method: "GET",
		URL:    u,
		Header: http.Header{"Connection": {"keep-alive, Upgrade"}, "Upgrade": {"websocket"}},
	}, &Response{StatusCode: 101, Streamed: true}).WithWebSocket(&WebSocket{Subprotocol: "graphql-ws", Messages: 12, CloseCode: 1000})

	cases := map[string]bool{
		`req.websocket = true`:        true,
		`req.websocket exists`:        true,
		`res.streamed = true`:         true,
		`ws.messages > 10`:            true,
		`ws.subprotocol = GRAPHQL-WS`: true,
		`ws.closeCode = 1000`:         true,
		`ws.closeCode exists`:         true,
	}

	for q, want := range cases {
		got, err := Match(mustParse(t, q), s)
		if err != nil || got != want {
			t.Errorf("Match(%q) = %v, %v, want %v", q, got, err, want)
		}
	}

	plain := exchange()
	for _, q := range []string{`req.websocket exists`, `ws.messages exists`, `res.streamed exists`} {
		if got, _ := Match(mustParse(t, q), plain); got {
			t.Errorf("%q matched a plain exchange", q)
		}
	}
}

// A phase key answers a number when the exchange was measured, and
// nothing at all when it was not - so a query for a slow phase never
// matches an entry nobody timed.
func TestTimingKeys(t *testing.T) {
	u, _ := url.Parse("https://api.example.com/v1")

	measured := NewHTTPSubject(
		Request{Method: "GET", URL: u},
		&Response{
			StatusCode: 200,
			Header:     http.Header{},
			DurationMS: 250,
			Timings: &Timings{
				Blocked: 1, DNS: 12, Connect: 30, TLS: 40, Send: 2, Wait: 150, Receive: 15,
				ServerAddr: "93.184.216.34:443", TLSVersion: "TLS 1.3", ALPN: "h2",
			},
		},
	)

	cases := map[string]bool{
		`res.dns = 12`:     true,
		`res.wait > 100ms`: true,
		`res.wait < 100ms`: false,
		`res.ttfb = 235`:   true, // 1+12+30+40+2+150
		`res.ttfb > 200ms AND res.receive < 20ms`: true,
		`res.tlsVersion = "TLS 1.3"`:              true,
		`res.alpn = h2`:                           true,
		`res.serverIP contains 93.184`:            true,
		`res.reused = true`:                       false,
	}

	for in, want := range cases {
		got, err := Match(mustParse(t, in), measured)
		if err != nil {
			t.Fatalf("Match(%q): %v", in, err)
		}

		if got != want {
			t.Errorf("Match(%q) = %v, want %v", in, got, want)
		}
	}

	// Not measured: every phase key is empty, so nothing about a phase
	// is claimed either way.
	unmeasured := NewHTTPSubject(
		Request{Method: "GET", URL: u},
		&Response{StatusCode: 200, Header: http.Header{}, DurationMS: 250},
	)

	keys := []string{
		"res.ttfb", "res.blocked", "res.dns", "res.connect", "res.tls",
		"res.send", "res.wait", "res.receive", "res.reused", "res.serverip",
		"res.tlsversion", "res.alpn",
	}

	for _, key := range keys {
		v, ok := unmeasured.Field(key)
		if !ok {
			t.Errorf("%s is not a known key", key)

			continue
		}

		if v != "" {
			t.Errorf("%s = %q on an unmeasured exchange, want empty", key, v)
		}
	}
}

// Validate refuses a field the vocabulary does not have, and does so on
// the whole tree rather than only the branches a match would reach.
// GraphQL over HTTP is one POST to one path, so the log can only tell
// two calls apart by reading the body. These are the keys that do it.
func TestGraphQLKeys(t *testing.T) {
	subject := func(reqBody, resBody string) HTTPSubject {
		u, _ := url.Parse("https://api.example.com/graphql")

		return NewHTTPSubject(Request{
			Method: "POST",
			URL:    u,
			Header: http.Header{"Content-Type": {"application/json"}},
			Body:   []byte(reqBody),
		}, &Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(resBody),
		})
	}

	// A failed GraphQL call with a 200, which is the ordinary case and
	// the reason res.gqlErrors is worth having.
	failed := subject(
		`{"operationName":"SignIn","query":"mutation SignIn { signIn { token } }"}`,
		`{"data":null,"errors":[{"message":"bad credentials"}]}`,
	)

	for query, want := range map[string]bool{
		`req.gqlType = mutation`:                     true,
		`req.gqlType = query`:                        false,
		`req.gqlName = SignIn`:                       true,
		`res.gqlErrors > 0`:                          true,
		`res.statusCode = 200 AND res.gqlErrors > 0`: true,
	} {
		assertMatch(t, failed, query, want)
	}

	// One that worked: no errors, and "no errors" is 0 rather than
	// nothing, since the body IS a GraphQL result.
	worked := subject(
		`{"query":"query Viewer { viewer { id } }"}`,
		`{"data":{"viewer":{"id":"7"}}}`,
	)

	for query, want := range map[string]bool{
		`req.gqlType = query`:  true,
		`req.gqlName = Viewer`: true,
		`res.gqlErrors = 0`:    true,
		`res.gqlErrors > 0`:    false,
	} {
		assertMatch(t, worked, query, want)
	}

	// Not GraphQL at all: every gql key answers nothing, so none of
	// these queries can match. res.gqlErrors = 0 must NOT match a
	// response that simply is not a GraphQL result.
	plain := subject(`{"user":"admin"}`, `{"ok":true}`)

	for query, want := range map[string]bool{
		`req.gqlType exists`:   false,
		`req.gqlName exists`:   false,
		`res.gqlErrors exists`: false,
		`res.gqlErrors = 0`:    false,
		`req.gqlType = query`:  false,
	} {
		assertMatch(t, plain, query, want)
	}
}

func assertMatch(t *testing.T, s HTTPSubject, query string, want bool) {
	t.Helper()

	expr, err := Parse(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	got, err := Match(expr, s)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	if got != want {
		t.Errorf("%s = %v, want %v", query, got, want)
	}
}

func TestValidateCatchesUnknownFields(t *testing.T) {
	// A zero subject is the vocabulary itself: every accessor answers a
	// known key with "" and an unknown one with not-ok.
	var probe HTTPSubject

	good := []string{
		"req.method = GET",
		"res.statusCode >= 400 AND res.ttfb > 1s",
		"req.header.authorization exists",
		"req.headers =~ token",
		"req.tag = todo",
		"req.query.id > 100 OR req.form.user = admin",
		"res.trailer.grpc-status = 0",
		"ws.messages > 10",
		// A false left side would short-circuit a match past the right,
		// which is exactly why Validate walks the tree instead.
		"req.method = NOPE AND res.alpn = h2",
	}

	for _, q := range good {
		if err := Validate(mustParse(t, q), probe); err != nil {
			t.Errorf("Validate(%q): %v", q, err)
		}
	}

	bad := []string{
		"req.nothing = 1",
		"res.nothing = 1",
		"ws.nothing = 1",
		"nonsense exists",
		// Short-circuited on the right, and still caught.
		"req.method = NOPE AND req.nothing = 1",
		"req.headerz.x = 1",
	}

	for _, q := range bad {
		if err := Validate(mustParse(t, q), probe); err == nil {
			t.Errorf("Validate(%q) accepted an unknown field", q)
		}
	}

	// An empty query is valid: it matches everything.
	if err := Validate(nil, probe); err != nil {
		t.Errorf("Validate(nil): %v", err)
	}
}

// Help is prose beside the switch statements, which is a copy - so this
// test refuses a copy that has drifted. Every field the text names has
// to be one a subject actually answers, or an agent handed this
// vocabulary would write a query that 400s.
//
// The other direction is not checkable here: a field added to the
// subject and not to Help would need an enumerable list of keys, which
// would be a third copy of the vocabulary. That one stays the author's
// to remember - as does FilterHelp.vue.
func TestHelpNamesOnlyRealFields(t *testing.T) {
	var probe HTTPSubject

	// Field names as they appear in Help: req.x, res.x, ws.x, and the
	// dynamic prefixes written as req.header.<name>.
	re := regexp.MustCompile(`\b(req|res|ws|tunnel)\.[a-zA-Z.<>]+`)

	seen := map[string]bool{}

	for _, match := range re.FindAllString(filterHelpForTest(), -1) {
		key := strings.TrimRight(match, ".")

		// A dynamic key is documented as req.header.<name>, so the thing to
		// check is that the PREFIX is known, so a real name is used.
		if strings.HasSuffix(key, ".<name>") {
			key = strings.TrimSuffix(key, "<name>") + "probe"
		}

		if seen[key] {
			continue
		}

		seen[key] = true

		if !Known(probe, Normalize(key)) {
			t.Errorf("Help names %q, which no subject answers", key)
		}
	}

	if len(seen) < 40 {
		t.Errorf("only %d fields were found in Help, so this test is not reading it", len(seen))
	}
}

func filterHelpForTest() string { return Help }
