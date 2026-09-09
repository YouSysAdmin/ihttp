// Package gqlmsg recognises GraphQL over HTTP and reads what a request
// says it is doing: query, mutation or subscription, its name, its
// variables, and on the way back how many errors came with the data.
//
// It is a scanner, not a parser: there is no schema here and no
// validation, only enough of the document read to name the operation.
// That is what a log needs - what this call was and whether it failed -
// and it costs no dependency.
//
// A request is recognised BY ITS BODY rather than by its path. A path of
// /graphql is a convention and nothing more: plenty of services answer
// GraphQL somewhere else, and plenty of things live at /graphql that
// are not GraphQL.
package gqlmsg

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"unicode"
)

// The operation types, as GraphQL spells them.
const (
	TypeQuery        = "query"
	TypeMutation     = "mutation"
	TypeSubscription = "subscription"
)

// Operation is what a GraphQL request over HTTP carries.
type Operation struct {
	// Type is query, mutation or subscription. A document that opens
	// with a bare selection set is a query, which is what the
	// specification says of it.
	Type string

	// Name is the operation's name: what the body's operationName said,
	// or the name in the document when it said nothing. Empty for an
	// anonymous operation.
	Name string

	// Query is the document as it was sent.
	Query string

	// Variables is the variables member, raw JSON, nil when there was
	// none. Kept as JSON because that is what it is - re-encoding it
	// would lose the order and the exact numbers.
	Variables jsontext.Value

	// Batch is how many operations the body carried. 1 for the ordinary
	// case. A batched body reports its length and describes its FIRST
	// operation, since one row of a log names one thing.
	Batch int
}

// body is the shape of a GraphQL request over HTTP. Unknown members are
// ignored: extensions, an id for a persisted query, whatever a client
// adds.
type body struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     jsontext.Value `json:"variables"`
}

// Detect reads a request body as GraphQL, reporting false for anything
// that is not one.
//
// Two ways in, both from the specification: a JSON body carrying a
// string query - the way every client sends it - and a body of
// application/graphql, which is the document itself.
func Detect(contentType string, raw []byte) (Operation, bool) {
	media := mediaType(contentType)

	if media == "application/graphql" {
		query := string(raw)
		if strings.TrimSpace(query) == "" {
			return Operation{}, false
		}

		opType, name := Describe(query)

		return Operation{Type: opType, Name: name, Query: query, Batch: 1}, true
	}

	if !strings.Contains(media, "json") && media != "" {
		return Operation{}, false
	}

	// Every GraphQL request over HTTP carries a query member, so a body
	// without those seven bytes is not one and is not parsed. This runs
	// over every JSON request body in a page of the log, and a substring
	// scan is far cheaper than a JSON parse. A key written as an escape
	// ("\u0071uery") is legal JSON and is missed on purpose.
	if !bytes.Contains(raw, []byte(`"query"`)) {
		return Operation{}, false
	}

	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if trimmed == "" {
		return Operation{}, false
	}

	switch trimmed[0] {
	case '{':
		var b body
		if err := json.Unmarshal(raw, &b); err != nil {
			return Operation{}, false
		}

		return fromBody(b, 1)
	case '[':
		// A batch: several operations in one request. The row names the
		// first and says how many there were.
		var bs []body
		if err := json.Unmarshal(raw, &bs); err != nil || len(bs) == 0 {
			return Operation{}, false
		}

		return fromBody(bs[0], len(bs))
	}

	return Operation{}, false
}

func fromBody(b body, batch int) (Operation, bool) {
	if strings.TrimSpace(b.Query) == "" {
		return Operation{}, false
	}

	opType, name := Describe(b.Query)
	if b.OperationName != "" {
		name = b.OperationName
	}

	return Operation{
		Type:      opType,
		Name:      name,
		Query:     b.Query,
		Variables: b.Variables,
		Batch:     batch,
	}, true
}

// result is the response half. errors is a list by the specification,
// and a response may carry both data and errors at once - which is
// exactly why a status code is not enough to tell whether a GraphQL
// call worked.
type result struct {
	Errors []jsontext.Value `json:"errors"`
}

// Errors counts the errors a GraphQL response carries, and reports
// whether the body reads as a GraphQL result at all.
//
// A failed GraphQL call is usually a 200. This is the field that says
// otherwise.
func Errors(contentType string, raw []byte) (int, bool) {
	if media := mediaType(contentType); !strings.Contains(media, "json") {
		return 0, false
	}

	trimmed := strings.TrimLeft(string(raw), " \t\r\n")
	if trimmed == "" || trimmed[0] != '{' {
		return 0, false
	}

	// The members that make a JSON object a GraphQL result. Read as
	// names alone, so a large data member is not decoded to count the
	// errors beside it.
	var (
		hasData   bool
		hasErrors bool
		res       result
	)

	dec := jsontext.NewDecoder(strings.NewReader(trimmed))
	if _, err := dec.ReadToken(); err != nil { // The opening brace.
		return 0, false
	}

	for {
		tok, err := dec.ReadToken()
		if err != nil || tok.Kind() == '}' {
			break
		}

		switch tok.String() {
		case "data":
			hasData = true

			if err := dec.SkipValue(); err != nil {
				return 0, false
			}
		case "errors":
			hasErrors = true

			value, err := dec.ReadValue()
			if err != nil {
				return 0, false
			}

			if err := json.Unmarshal(value, &res.Errors); err != nil {
				return 0, false
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return 0, false
			}
		}
	}

	if !hasData && !hasErrors {
		return 0, false
	}

	return len(res.Errors), true
}

// Describe names the operation a document performs: its type and its
// name, both empty when the text is not a document at all.
//
// Fragments may come before the operation, and a document may hold
// several operations - the first one is the one described, since the
// body's operationName is what picks between them and that is read
// before this.
func Describe(query string) (opType, name string) {
	s := query

	for {
		s = skipIgnored(s)
		if s == "" {
			return "", ""
		}

		// A bare selection set is a query. The specification calls it
		// the shorthand form.
		if s[0] == '{' {
			return TypeQuery, ""
		}

		word, rest := readName(s)
		switch word {
		case TypeQuery, TypeMutation, TypeSubscription:
			opName, _ := readName(skipIgnored(rest))

			// A variable list or a directive can follow the keyword
			// directly, in which case the operation is anonymous.
			if opName == "" {
				return word, ""
			}

			return word, opName
		case "fragment":
			// Step over the whole fragment and look again.
			next := skipBlock(rest)
			if next == rest {
				return "", ""
			}

			s = next
		case "":
			// Punctuation where a definition should be: not a document.
			return "", ""
		default:
			// Some other definition - a type system one, in a schema
			// file rather than a request. Nothing to name.
			return "", ""
		}
	}
}

// skipIgnored steps over whitespace, commas - which GraphQL treats as
// whitespace - and # comments.
func skipIgnored(s string) string {
	for {
		trimmed := strings.TrimLeft(s, " \t\r\n,\ufeff")
		if trimmed == "" {
			return ""
		}

		if trimmed[0] != '#' {
			return trimmed
		}

		if at := strings.IndexAny(trimmed, "\r\n"); at >= 0 {
			s = trimmed[at:]

			continue
		}

		return ""
	}
}

// readName reads one GraphQL name, or "" when the text does not start
// with one.
func readName(s string) (string, string) {
	end := 0

	for end < len(s) {
		r := rune(s[end])
		if r >= 0x80 {
			break // A name is ASCII, so a wider rune ends it.
		}

		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}

		end++
	}

	if end == 0 {
		return "", s
	}

	return s[:end], s[end:]
}

// skipBlock steps over everything up to and past the first balanced
// brace group, so a fragment definition can be passed by. Strings are
// respected, since a brace inside one is not a brace.
func skipBlock(s string) string {
	depth := 0

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			if next, ok := skipString(s, i); ok {
				i = next
			} else {
				return s
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i+1:]
			}
		}
	}

	return s
}

// skipString returns the index of the closing quote of the string that
// starts at i, handling both a block string and an escape.
func skipString(s string, i int) (int, bool) {
	if strings.HasPrefix(s[i:], `"""`) {
		at := strings.Index(s[i+3:], `"""`)
		if at < 0 {
			return 0, false
		}

		return i + 3 + at + 2, true
	}

	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '"':
			return j, true
		case '\n', '\r':
			return 0, false
		}
	}

	return 0, false
}

// mediaType is the type without its parameters, lowercased.
func mediaType(contentType string) string {
	media, _, _ := strings.Cut(contentType, ";")

	return strings.ToLower(strings.TrimSpace(media))
}
