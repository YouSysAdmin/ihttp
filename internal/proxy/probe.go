package proxy

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/yousysadmin/ihttp/internal/core/httpmsg"
)

// DefaultProbeHost is the host the proxy answers itself so a setup can
// be checked. It does not resolve anywhere, which is the point: a probe
// must work with no network and must never reach a real server.
//
// Deliberately not the landing host: that one is served before the
// transport and so is never logged, while a probe has to turn up in the
// log to prove anything.
const DefaultProbeHost = "ihttp.probe"

// probeResponse is what a probe request is answered with. The path is
// echoed, so the token the console put there comes back where the
// person running the command can see it too.
func probeResponse(req *http.Request) *http.Response {
	path := "/"
	if req.URL != nil && req.URL.Path != "" {
		path = req.URL.Path
	}

	body := fmt.Sprintf("ihttp saw this request.\n\nmethod: %s\npath: %s\nproto: %s\n\n"+
		"It reached the proxy and is in the request log, if a project is open.\n"+
		"Nothing left this machine: %s does not resolve anywhere.\n",
		req.Method, path, req.Proto, DefaultProbeHost)

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header: http.Header{
			"Content-Type":  []string{"text/plain; charset=utf-8"},
			"X-Ihttp-Probe": []string{"1"},
		},
		Body: httpmsg.Replace([]byte(body)),
	}
}

// firstNonEmptyHost is the first host that is not blank, lower-cased so
// the comparison on the request path needs no folding.
func firstNonEmptyHost(values ...string) string {
	for _, v := range values {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			return v
		}
	}

	return ""
}
