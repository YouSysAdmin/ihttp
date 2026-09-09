package httpmsg

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

// SnippetInput is a request as the log or the sender holds it, ready to
// be spelled as a command.
type SnippetInput struct {
	Method  string
	URL     string
	Proto   string
	Headers Headers
	Body    Body
}

// Curl spells the request as a curl command that sends the same bytes.
// Headers keep their order. Content-Length is left to curl, and an
// Accept-Encoding becomes --compressed so curl decodes what it asked
// for. A binary body is piped in from base64 so the command stays one
// self-contained line of shell.
func Curl(in SnippetInput) string {
	method := strings.ToUpper(in.Method)
	hasBody := len(in.Body) > 0
	binary := hasBody && IsBinary(in.Headers.Get("Content-Type"), in.Body)

	var b strings.Builder

	if binary {
		b.WriteString("printf '%s' '" + base64.StdEncoding.EncodeToString(in.Body) + "' | base64 -d | ")
	}

	b.WriteString("curl " + shellQuote(in.URL))

	if in.Proto == ProtoHTTP20 {
		b.WriteString(" \\\n  --http2")
	}

	// curl infers GET, and POST when there is data. HEAD is -I, so curl
	// does not wait for a body. Anything else is said.
	implied := (method == http.MethodGet && !hasBody) || (method == http.MethodPost && hasBody)
	switch {
	case method == http.MethodHead:
		b.WriteString(" \\\n  -I")
	case !implied:
		b.WriteString(" \\\n  -X " + method)
	}

	compressed := false

	for _, h := range in.Headers {
		switch strings.ToLower(h.Name) {
		case "content-length":
			continue
		case "accept-encoding":
			compressed = true

			continue
		}

		b.WriteString(" \\\n  -H " + shellQuote(h.Name+": "+h.Value))
	}

	if compressed {
		b.WriteString(" \\\n  --compressed")
	}

	switch {
	case binary:
		b.WriteString(" \\\n  --data-binary @-")
	case hasBody:
		b.WriteString(" \\\n  --data-raw " + shellQuote(string(in.Body)))
	}

	return b.String()
}

// Fetch spells the request as a fetch() call for a browser console or a
// script. Content-Length and Accept-Encoding are dropped, a browser sets
// both itself. A binary body is rebuilt from base64.
func Fetch(in SnippetInput) string {
	method := strings.ToUpper(in.Method)
	hasBody := len(in.Body) > 0
	binary := hasBody && IsBinary(in.Headers.Get("Content-Type"), in.Body)

	var b strings.Builder

	b.WriteString("await fetch(" + jsQuote(in.URL) + ", {\n")

	if method != http.MethodGet {
		b.WriteString("  method: " + jsQuote(method) + ",\n")
	}

	var headers []Header
	for _, h := range in.Headers {
		switch strings.ToLower(h.Name) {
		case "content-length", "accept-encoding":
			continue
		}

		headers = append(headers, h)
	}

	if len(headers) > 0 {
		b.WriteString("  headers: {\n")
		for _, h := range headers {
			b.WriteString("    " + jsQuote(h.Name) + ": " + jsQuote(h.Value) + ",\n")
		}
		b.WriteString("  },\n")
	}

	switch {
	case binary:
		b.WriteString("  body: Uint8Array.from(atob(" + jsQuote(base64.StdEncoding.EncodeToString(in.Body)) + "), (c) => c.charCodeAt(0)),\n")
	case hasBody:
		b.WriteString("  body: " + jsQuote(string(in.Body)) + ",\n")
	}

	b.WriteString("})")

	return b.String()
}

// shellQuote wraps s in single quotes, the only quoting a POSIX shell
// never interprets. A single quote inside is closed, escaped and reopened.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// jsQuote renders s as a JavaScript single-quoted string literal.
func jsQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')

	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f || r == unicode.ReplacementChar {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}

	b.WriteByte('\'')

	return b.String()
}

// HTTPie spells the request as an http command. Headers are given in
// httpie's own Name:value form with no space, which needs no thought
// about how it splits the value. Content-Length is left to httpie, and
// a binary body is piped in, which is how httpie reads a body from
// stdin.
func HTTPie(in SnippetInput) string {
	method := strings.ToUpper(in.Method)
	hasBody := len(in.Body) > 0
	binary := hasBody && IsBinary(in.Headers.Get("Content-Type"), in.Body)

	var b strings.Builder

	if binary {
		b.WriteString("printf '%s' '" + base64.StdEncoding.EncodeToString(in.Body) + "' | base64 -d | ")
	}

	// The method is always named: httpie infers GET and POST, and a
	// snippet that says which one it is cannot be misread.
	b.WriteString("http ")

	// httpie treats a non-terminal stdin as the request body, so run
	// from a script or a pipeline it refuses to take --raw as well and
	// waits for input when there is nothing to read. --ignore-stdin is
	// httpie's own answer, and it is only wrong when the body IS being
	// piped in, which is the binary case below.
	if !binary {
		b.WriteString("--ignore-stdin ")
	}

	b.WriteString(method + " " + shellQuote(in.URL))

	for _, h := range in.Headers {
		if strings.EqualFold(h.Name, "content-length") {
			continue
		}

		b.WriteString(" \\\n  " + shellQuote(h.Name+":"+h.Value))
	}

	if hasBody && !binary {
		b.WriteString(" \\\n  --raw " + shellQuote(string(in.Body)))
	}

	return b.String()
}

// Python spells the request as a requests call. Content-Length and
// Accept-Encoding are left out: requests sets the first and urllib3
// negotiates the second, and asking for an encoding it will not decode
// is how a snippet returns bytes nobody can read.
func Python(in SnippetInput) string {
	method := strings.ToLower(in.Method)
	if method == "" {
		method = "get"
	}

	hasBody := len(in.Body) > 0
	binary := hasBody && IsBinary(in.Headers.Get("Content-Type"), in.Body)

	var b strings.Builder

	if binary {
		b.WriteString("import base64\n")
	}

	b.WriteString("import requests\n\n")

	// requests has a function per common method and request() for the
	// rest, so an unusual method is still spelled correctly.
	call := "requests." + method + "("
	if !isCommonMethod(method) {
		call = "requests.request(" + pyQuote(strings.ToUpper(in.Method)) + ", "
	}

	b.WriteString("response = " + call + "\n    " + pyQuote(in.URL) + ",\n")

	var headers []Header

	for _, h := range in.Headers {
		switch strings.ToLower(h.Name) {
		case "content-length", "accept-encoding":
			continue
		}

		headers = append(headers, h)
	}

	if len(headers) > 0 {
		b.WriteString("    headers={\n")

		for _, h := range headers {
			b.WriteString("        " + pyQuote(h.Name) + ": " + pyQuote(h.Value) + ",\n")
		}

		b.WriteString("    },\n")
	}

	switch {
	case binary:
		b.WriteString("    data=base64.b64decode(" + pyQuote(base64.StdEncoding.EncodeToString(in.Body)) + "),\n")
	case hasBody:
		b.WriteString("    data=" + pyQuote(string(in.Body)) + ".encode(),\n")
	}

	b.WriteString(")\n\nprint(response.status_code)\nprint(response.text)")

	return b.String()
}

// Go spells the request as a program using net/http, which is what a
// reader of this project is most likely to paste it into.
func Go(in SnippetInput) string {
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}

	hasBody := len(in.Body) > 0
	binary := hasBody && IsBinary(in.Headers.Get("Content-Type"), in.Body)

	imports := []string{"fmt", "io", "net/http"}

	switch {
	case binary:
		imports = append([]string{"bytes", "encoding/base64"}, imports...)
	case hasBody:
		imports = append(imports, "strings")
	}

	var b strings.Builder

	b.WriteString("package main\n\nimport (\n")

	for _, im := range imports {
		b.WriteString("\t" + goQuote(im) + "\n")
	}

	b.WriteString(")\n\nfunc main() {\n")

	reader := "nil"

	switch {
	case binary:
		b.WriteString("\tbody, err := base64.StdEncoding.DecodeString(" +
			goQuote(base64.StdEncoding.EncodeToString(in.Body)) + ")\n")
		b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")

		reader = "bytes.NewReader(body)"
	case hasBody:
		reader = "strings.NewReader(" + goQuote(string(in.Body)) + ")"
	}

	b.WriteString("\treq, err := http.NewRequest(" + goQuote(method) + ", " + goQuote(in.URL) + ", " + reader + ")\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")

	for _, h := range in.Headers {
		switch strings.ToLower(h.Name) {
		case "content-length":
			// Set from the body by net/http.
			continue
		case "host":
			// A Host in the header map is ignored by net/http, so it has
			// to be said the way net/http reads it.
			b.WriteString("\treq.Host = " + goQuote(h.Value) + "\n")

			continue
		}

		b.WriteString("\treq.Header.Add(" + goQuote(h.Name) + ", " + goQuote(h.Value) + ")\n")
	}

	b.WriteString("\n\tres, err := http.DefaultClient.Do(req)\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")
	b.WriteString("\tdefer res.Body.Close()\n\n")
	b.WriteString("\tout, err := io.ReadAll(res.Body)\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n\n")
	b.WriteString("\tfmt.Println(res.Status)\n\tfmt.Println(string(out))\n}")

	return b.String()
}

// PowerShell spells the request as an Invoke-RestMethod call.
//
// Content-Type and User-Agent go in their own parameters rather than in
// the header table: PowerShell refuses those two in -Headers, and a
// snippet that throws is worse than no snippet.
func PowerShell(in SnippetInput) string {
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}

	hasBody := len(in.Body) > 0
	binary := hasBody && IsBinary(in.Headers.Get("Content-Type"), in.Body)

	var (
		b           strings.Builder
		headers     []Header
		contentType string
		userAgent   string
	)

	for _, h := range in.Headers {
		switch strings.ToLower(h.Name) {
		case "content-length":
			continue
		case "content-type":
			contentType = h.Value
		case "user-agent":
			userAgent = h.Value
		default:
			headers = append(headers, h)
		}
	}

	if len(headers) > 0 {
		b.WriteString("$headers = @{\n")

		for _, h := range headers {
			b.WriteString("  " + psQuote(h.Name) + " = " + psQuote(h.Value) + "\n")
		}

		b.WriteString("}\n\n")
	}

	b.WriteString("Invoke-RestMethod -Method " + method + " -Uri " + psQuote(in.URL))

	if len(headers) > 0 {
		b.WriteString(" `\n  -Headers $headers")
	}

	if contentType != "" {
		b.WriteString(" `\n  -ContentType " + psQuote(contentType))
	}

	if userAgent != "" {
		b.WriteString(" `\n  -UserAgent " + psQuote(userAgent))
	}

	switch {
	case binary:
		b.WriteString(" `\n  -Body ([Convert]::FromBase64String(" +
			psQuote(base64.StdEncoding.EncodeToString(in.Body)) + "))")
	case hasBody:
		b.WriteString(" `\n  -Body " + psQuote(string(in.Body)))
	}

	return b.String()
}

// isCommonMethod says requests has a function of that name.
func isCommonMethod(lower string) bool {
	switch lower {
	case "get", "post", "put", "patch", "delete", "head", "options":
		return true
	}

	return false
}

// pyQuote renders s as a Python string literal. Python's escapes are
// JavaScript's for everything this has to write, so one form serves
// both - but they are separate functions, since the next escape either
// language grows will not be shared.
func pyQuote(s string) string {
	return quoteWith(s, '"')
}

// goQuote renders s as a Go interpreted string literal.
func goQuote(s string) string {
	return quoteWith(s, '"')
}

// psQuote renders s as a PowerShell single-quoted string, where the
// only escape is a doubled quote and nothing else is interpreted.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// quoteWith renders s as a double-quoted literal with C-style escapes,
// which is what Python, Go and JSON all read.
func quoteWith(s string, quote byte) string {
	var b strings.Builder

	b.WriteByte(quote)

	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case rune(quote):
			b.WriteByte('\\')
			b.WriteByte(quote)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f || r == unicode.ReplacementChar {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}

	b.WriteByte(quote)

	return b.String()
}
