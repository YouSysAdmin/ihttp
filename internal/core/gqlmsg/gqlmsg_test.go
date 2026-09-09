package gqlmsg_test

import (
	"strings"
	"testing"

	"github.com/yousysadmin/ihttp/internal/core/gqlmsg"
)

func TestDescribe(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		query    string
		wantType string
		wantName string
	}{
		{name: "shorthand", query: "{ viewer { id } }", wantType: "query"},
		{name: "named query", query: "query Viewer { viewer { id } }", wantType: "query", wantName: "Viewer"},
		{name: "mutation", query: "mutation SignIn($p: String!) { signIn(p: $p) { token } }", wantType: "mutation", wantName: "SignIn"},
		{name: "subscription", query: "subscription OnTick { tick }", wantType: "subscription", wantName: "OnTick"},
		{name: "anonymous with variables", query: "query($id: ID!) { node(id: $id) { id } }", wantType: "query"},
		{name: "leading comment", query: "# what this does\n#\nquery Viewer { viewer { id } }", wantType: "query", wantName: "Viewer"},
		{name: "leading commas and newlines", query: "\n\n  query Viewer { viewer { id } }", wantType: "query", wantName: "Viewer"},
		{
			name:     "fragment first",
			query:    "fragment F on User { id }\nquery Viewer { viewer { ...F } }",
			wantType: "query",
			wantName: "Viewer",
		},
		{
			name:     "fragment holding a brace in a string",
			query:    `fragment F on User { name(fallback: "}") }` + "\nmutation Save { save { id } }",
			wantType: "mutation",
			wantName: "Save",
		},
		{
			name:     "fragment holding a block string",
			query:    "fragment F on User { note(text: \"\"\"a } b\"\"\") }\nquery Q { viewer { id } }",
			wantType: "query",
			wantName: "Q",
		},
		{name: "two operations names the first", query: "query A { a }\nquery B { b }", wantType: "query", wantName: "A"},
		{name: "a schema is not an operation", query: "type User { id: ID! }", wantType: "", wantName: ""},
		{name: "empty", query: "   \n ", wantType: "", wantName: ""},
		{name: "not graphql at all", query: `{"json": true}`, wantType: "query", wantName: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			opType, name := gqlmsg.Describe(c.query)
			if opType != c.wantType || name != c.wantName {
				t.Errorf("Describe() = (%q, %q), want (%q, %q)", opType, name, c.wantType, c.wantName)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	t.Run("the ordinary POST every client sends", func(t *testing.T) {
		t.Parallel()

		op, ok := gqlmsg.Detect("application/json", []byte(
			`{"operationName":"Viewer","query":"query Viewer { viewer { id } }","variables":{"id":"7"}}`))
		if !ok {
			t.Fatal("a GraphQL POST was not recognised")
		}

		if op.Type != "query" || op.Name != "Viewer" || op.Batch != 1 {
			t.Errorf("%+v", op)
		}

		if string(op.Variables) != `{"id":"7"}` {
			t.Errorf("variables %q, want the raw JSON as sent", op.Variables)
		}
	})

	t.Run("operationName wins over the document", func(t *testing.T) {
		t.Parallel()

		// Which is right: operationName is what the SERVER uses to pick
		// between the operations in the document.
		op, _ := gqlmsg.Detect("application/json",
			[]byte(`{"operationName":"B","query":"query A { a }\nquery B { b }"}`))
		if op.Name != "B" {
			t.Errorf("name %q, want B", op.Name)
		}
	})

	t.Run("a batch names its first and counts them", func(t *testing.T) {
		t.Parallel()

		op, ok := gqlmsg.Detect("application/json",
			[]byte(`[{"query":"query A { a }"},{"query":"mutation B { b }"}]`))
		if !ok {
			t.Fatal("a batched body was not recognised")
		}

		if op.Name != "A" || op.Batch != 2 {
			t.Errorf("%+v, want A with a batch of 2", op)
		}
	})

	t.Run("application/graphql carries the document itself", func(t *testing.T) {
		t.Parallel()

		op, ok := gqlmsg.Detect("application/graphql", []byte("mutation Save { save { id } }"))
		if !ok || op.Type != "mutation" || op.Name != "Save" {
			t.Errorf("%+v ok=%v", op, ok)
		}
	})

	t.Run("what is not GraphQL", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name        string
			contentType string
			body        string
		}{
			{name: "plain JSON", contentType: "application/json", body: `{"user":"admin"}`},
			{name: "an empty query", contentType: "application/json", body: `{"query":"  "}`},
			{name: "a query that is not a string", contentType: "application/json", body: `{"query":{"a":1}}`},
			{name: "a form", contentType: "application/x-www-form-urlencoded", body: "query=x"},
			{name: "an empty body", contentType: "application/json", body: ""},
			{name: "broken JSON", contentType: "application/json", body: `{"query":`},
			{name: "an empty batch", contentType: "application/json", body: `[]`},
		}

		for _, c := range cases {
			if _, ok := gqlmsg.Detect(c.contentType, []byte(c.body)); ok {
				t.Errorf("%s was read as GraphQL", c.name)
			}
		}
	})
}

// A failed GraphQL call is usually a 200, which is the whole reason
// this field exists.
func TestErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		contentType string
		body        string
		want        int
		wantOK      bool
	}{
		{
			name: "data and errors together", contentType: "application/json",
			body:   `{"data":{"viewer":null},"errors":[{"message":"nope"},{"message":"also nope"}]}`,
			want:   2,
			wantOK: true,
		},
		{
			name: "data alone", contentType: "application/json",
			body: `{"data":{"viewer":{"id":"1"}}}`, want: 0, wantOK: true,
		},
		{
			name: "errors alone", contentType: "application/json",
			body: `{"errors":[{"message":"nope"}]}`, want: 1, wantOK: true,
		},
		{
			name: "a large data member is stepped over, not decoded", contentType: "application/json",
			body:   `{"extensions":{"a":1},"data":{"list":[` + strings.Repeat(`{"x":1},`, 100) + `{"x":1}]},"errors":[{"message":"nope"}]}`,
			want:   1,
			wantOK: true,
		},
		{name: "plain JSON", contentType: "application/json", body: `{"ok":true}`, wantOK: false},
		{name: "not JSON", contentType: "text/html", body: `{"data":{}}`, wantOK: false},
		{name: "an array", contentType: "application/json", body: `[{"data":{}}]`, wantOK: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, ok := gqlmsg.Errors(c.contentType, []byte(c.body))
			if ok != c.wantOK || got != c.want {
				t.Errorf("Errors() = (%d, %v), want (%d, %v)", got, ok, c.want, c.wantOK)
			}
		})
	}
}
