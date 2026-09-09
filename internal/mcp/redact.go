package mcp

import (
	"net/url"
	"strings"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// This is the one place in ihttp where captured traffic can leave the
// machine: an agent may be a model on somebody else's hardware. So the
// default is to mask what is obviously a credential, and turning it off
// is an explicit flag rather than a setting to forget.
//
// Masking is not safety. A body may carry a token under any name, and
// nothing here reads bodies. What it does is stop the everyday case -
// an Authorization header, a session cookie - from being copied into a
// prompt by accident.

// secretHeaders are masked whole.
var secretHeaders = []string{
	"authorization",
	"proxy-authorization",
	"cookie",
	"set-cookie",
	"x-api-key",
	"x-auth-token",
	"x-amz-security-token",
	"x-csrf-token",
	"x-xsrf-token",
}

// secretish names a query or form parameter whose VALUE is masked. A
// substring match, so access_token and api_key are caught with their
// families.
var secretish = []string{"token", "secret", "password", "passwd", "apikey", "api_key", "auth", "signature", "sig", "session", "credential"}

// mask is what replaces a value. Its length says nothing about the
// original, on purpose. No spaces or brackets, so it survives URL
// encoding as itself rather than as %5B...%5D when a query value is
// masked and the query is put back together.
const mask = "REDACTED"

// Redactor masks credentials on their way to an agent. The zero value
// masks nothing, which is what --no-redact asks for.
type Redactor struct {
	On bool
}

// Headers returns headers with the secret ones masked.
func (r Redactor) Headers(in httpmsg.Headers) httpmsg.Headers {
	if !r.On || len(in) == 0 {
		return in
	}

	out := make(httpmsg.Headers, 0, len(in))

	for _, h := range in {
		if isSecretHeader(h.Name) {
			out = append(out, httpmsg.Header{Name: h.Name, Value: mask})

			continue
		}

		out = append(out, h)
	}

	return out
}

// URL masks the values of query parameters that look like credentials,
// keeping their names, since which parameter it was is the useful half.
func (r Redactor) URL(raw string) string {
	if !r.On || raw == "" {
		return raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.RawQuery == "" {
		return raw
	}

	q := u.Query()

	changed := false

	for name, values := range q {
		if !isSecretish(name) {
			continue
		}

		for i := range values {
			if values[i] != "" {
				values[i] = mask
				changed = true
			}
		}

		q[name] = values
	}

	if !changed {
		return raw
	}

	u.RawQuery = q.Encode()

	return u.String()
}

func isSecretHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))

	for _, s := range secretHeaders {
		if name == s {
			return true
		}
	}

	return false
}

func isSecretish(name string) bool {
	name = strings.ToLower(name)

	for _, s := range secretish {
		if strings.Contains(name, s) {
			return true
		}
	}

	return false
}

// Note is what a tool appends so the agent knows what it is looking at.
// Silence would let a masked value be read as the real one.
func (r Redactor) Note() string {
	if !r.On {
		return "\n(redaction is off: credentials are shown as captured)"
	}

	return "\n(credential headers and secret-looking query values are masked - bodies are not scanned)"
}
