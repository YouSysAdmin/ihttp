package cli

import (
	"strings"
	"testing"
)

func TestEnvVarsCoverBothCasesAndLeaveTrustAlone(t *testing.T) {
	t.Setenv("JAVA_TOOL_OPTIONS", "")

	vars := envVars("http://127.0.0.1:8080", "127.0.0.1:8081", "/tmp/ca.pem", false)

	got := map[string]string{}
	for _, v := range vars {
		got[v.name] = v.value
	}

	// Both cases, because clients disagree about which they read.
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		if got[name] == "" {
			t.Errorf("%s was not set", name)
		}
	}

	if got["NO_PROXY"] != "127.0.0.1:8081" {
		t.Errorf("NO_PROXY = %q, want the console's host and port only", got["NO_PROXY"])
	}

	if got["NODE_USE_ENV_PROXY"] != "1" {
		t.Errorf("NODE_USE_ENV_PROXY = %q, want 1", got["NODE_USE_ENV_PROXY"])
	}

	// Additive trust yes, replacement trust no.
	if got["NODE_EXTRA_CA_CERTS"] != "/tmp/ca.pem" {
		t.Errorf("NODE_EXTRA_CA_CERTS = %q", got["NODE_EXTRA_CA_CERTS"])
	}

	for _, name := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
		if _, ok := got[name]; ok {
			t.Errorf("%s replaces a runtime's whole trust store and must not be set by default", name)
		}
	}

	with := envVars("http://127.0.0.1:8080", "127.0.0.1:8081", "/tmp/ca.pem", true)

	found := false
	for _, v := range with {
		if v.name == "SSL_CERT_FILE" {
			found = true
		}
	}

	if !found {
		t.Error("--replace-ca-bundle did not set SSL_CERT_FILE")
	}
}

func TestJavaToolOptionsKeepsTheirsAndDoesNotStack(t *testing.T) {
	t.Setenv("JAVA_TOOL_OPTIONS", "-Xmx512m -Dfoo=bar")

	once := javaToolOptions("http://127.0.0.1:8080")
	if !strings.HasPrefix(once, "-Xmx512m -Dfoo=bar ") {
		t.Fatalf("their own options were lost: %q", once)
	}

	if !strings.Contains(once, "-Dhttp.proxyPort=8080") || !strings.Contains(once, "-Dhttps.proxyHost=127.0.0.1") {
		t.Fatalf("the proxy properties are missing: %q", once)
	}

	// Sourcing twice must not stack a second block.
	t.Setenv("JAVA_TOOL_OPTIONS", once)

	twice := javaToolOptions("http://127.0.0.1:8080")
	if twice != once {
		t.Fatalf("re-running stacked:\n once: %q\ntwice: %q", once, twice)
	}

	if n := strings.Count(twice, "-Dhttp.proxyHost="); n != 1 {
		t.Fatalf("-Dhttp.proxyHost appears %d times in %q", n, twice)
	}

	// A different proxy replaces the old block rather than adding to it.
	t.Setenv("JAVA_TOOL_OPTIONS", once)

	moved := javaToolOptions("http://127.0.0.1:9999")
	if strings.Contains(moved, "8080") || !strings.Contains(moved, "-Dhttp.proxyPort=9999") {
		t.Fatalf("the old proxy survived: %q", moved)
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct{ in, host, port string }{
		{"http://localhost:8080", "localhost", "8080"},
		{"http://127.0.0.1:8080/", "127.0.0.1", "8080"},
		{"http://proxy.example.com", "proxy.example.com", "80"},
		{"localhost:8080", "localhost", "8080"},
		{"http://[::1]:8080", "[::1]", "8080"},
	}

	for _, c := range cases {
		host, port := hostPort(c.in)
		if host != c.host || port != c.port {
			t.Errorf("hostPort(%q) = %q, %q; want %q, %q", c.in, host, port, c.host, c.port)
		}
	}
}

func TestRenderEnvPerShell(t *testing.T) {
	t.Setenv("JAVA_TOOL_OPTIONS", "")

	vars := []envVar{{"HTTP_PROXY", "http://127.0.0.1:8080"}}

	cases := map[string]string{
		"sh":         "export HTTP_PROXY='http://127.0.0.1:8080'",
		"fish":       "set -gx HTTP_PROXY 'http://127.0.0.1:8080'",
		"powershell": "$env:HTTP_PROXY = 'http://127.0.0.1:8080'",
		"cmd":        "set HTTP_PROXY=http://127.0.0.1:8080",
		"env":        "HTTP_PROXY=http://127.0.0.1:8080",
	}

	for shell, want := range cases {
		out, err := renderEnv(shell, vars, false)
		if err != nil {
			t.Fatalf("%s: %v", shell, err)
		}

		if !strings.Contains(out, want) {
			t.Errorf("%s output does not carry %q:\n%s", shell, want, out)
		}
	}

	if _, err := renderEnv("tcsh", vars, false); err == nil {
		t.Error("an unknown shell was accepted")
	}

	// Unsetting names every variable, whatever its value would be.
	out, err := renderEnv("sh", vars, true)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "unset HTTP_PROXY") {
		t.Errorf("unset output:\n%s", out)
	}
}

// A value carrying a quote must not end the quoting it sits in.
func TestQuotingSurvivesAQuote(t *testing.T) {
	if got := quotePOSIX("it's"); got != `'it'\''s'` {
		t.Errorf("quotePOSIX = %s", got)
	}

	if got := quotePowerShell("it's"); got != "'it''s'" {
		t.Errorf("quotePowerShell = %s", got)
	}
}
