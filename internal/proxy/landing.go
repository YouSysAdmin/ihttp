package proxy

import (
	"html/template"
	"net/http"

	"github.com/yousysadmin/ihttp/internal/core/certgen"
)

// landingHost is the name a browser configured to use the proxy can
// visit to reach the landing page and the certificate, the way mitm.it works for mitmproxy.
const landingHost = "ihttp.proxy"

var landingPage = template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>ihttp proxy</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
         max-width: 36rem; margin: 10vh auto; padding: 0 1.5rem; color: #0a0a0a; background: #fafafa; }
  @media (prefers-color-scheme: dark) { body { color: #fafafa; background: #0a0a0a; } }
  h1 { font-size: 1.4rem; margin: 0 0 .5rem; }
  p { margin: .5rem 0 1rem; opacity: .8; }
  a.btn { display: inline-block; padding: .55rem 1rem; border-radius: .5rem; border: 1px solid currentColor;
          text-decoration: none; color: inherit; margin-right: .5rem; }
  code { font-family: ui-monospace, Menlo, monospace; }
</style>
</head>
<body>
<h1>ihttp proxy</h1>
<p>This port is the MITM proxy. Point a browser or a tool at it and open the console to see what passes through.</p>
<p>
  {{if .Console}}<a class="btn" href="{{.Console}}">Open the console</a>{{end}}
  <a class="btn" href="/ca.pem" download="ihttp-ca.pem">Download the CA certificate</a>
</p>
<p>Install the certificate in your trust store so HTTPS sites load without warnings, or run <code>ihttp cert install</code>.</p>
</body>
</html>
`))

// newLanding answers requests addressed to the proxy itself.
func newLanding(ca *certgen.Authority, consoleURL string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /ca.pem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `attachment; filename="ihttp-ca.pem"`)
		_, _ = w.Write(ca.CAPEM())
	})

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = landingPage.Execute(w, struct{ Console string }{consoleURL})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	return mux
}
