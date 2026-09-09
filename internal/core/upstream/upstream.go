// Package upstream is the proxy ihttp itself goes out through, for a
// lab or a corporate network where nothing reaches the internet
// directly.
//
// It is an instance setting, not a project one: which way out the
// machine has is a fact about the machine. The same configuration is
// used by the MITM's upstream transport, the sender and the automation,
// so a replayed request leaves the same way the proxied original did.
package upstream

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

// Schemes an upstream proxy URL may use. socks5 and socks5h are handled
// by net/http itself, so no dependency is needed for them.
var schemes = []string{"http", "https", "socks5", "socks5h"}

// alwaysBypassed never goes through an upstream proxy, whatever the
// operator's list says: a target on this machine is reached directly, or
// a corporate proxy would be asked to route to a host only we can see.
// The console's own address is in here by the same argument.
var alwaysBypassed = []string{"localhost", "127.0.0.1", "::1", "[::1]"}

// Config is where ihttp sends its own outbound traffic.
type Config struct {
	// URL is the proxy, with a scheme: http://proxy:3128,
	// https://proxy:3128, socks5://proxy:1080. Credentials may be in it
	// as userinfo, and are then sent as Proxy-Authorization - put them
	// in IHTTP_UPSTREAM_PROXY rather than on a command line, where every
	// process listing would show them.
	//
	// Empty means no upstream proxy is configured, and the HTTP_PROXY
	// family of environment variables is honoured as before.
	URL string

	// Bypass are host patterns reached directly. A pattern is a host
	// glob: api.example.com, *.example.com, ?ost.local. Matching ignores
	// case and ignores the port.
	Bypass []string
}

// Configured says an upstream proxy was asked for.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.URL) != ""
}

// Compile checks the configuration and returns the Proxy function for an
// http.Transport. A Config with no URL yields http.ProxyFromEnvironment,
// so the environment keeps working exactly as it did.
func (c Config) Compile() (func(*http.Request) (*url.URL, error), error) {
	if !c.Configured() {
		return http.ProxyFromEnvironment, nil
	}

	u, err := Parse(c.URL)
	if err != nil {
		return nil, err
	}

	bypass, err := compileBypass(c.Bypass)
	if err != nil {
		return nil, err
	}

	return func(req *http.Request) (*url.URL, error) {
		if req.URL == nil || bypass(req.URL.Hostname()) {
			return nil, nil
		}

		return u, nil
	}, nil
}

// Parse reads a proxy URL and says what is wrong with it in the terms
// the operator typed it in.
func Parse(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("upstream proxy %q: %w", Redact(raw), err)
	}

	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("upstream proxy %q needs a scheme and a host, as in http://proxy:3128", Redact(raw))
	}

	scheme := strings.ToLower(u.Scheme)
	if !slices.Contains(schemes, scheme) {
		return nil, fmt.Errorf("upstream proxy scheme %q is not one of %s", scheme, strings.Join(schemes, ", "))
	}

	return u, nil
}

// Host is a proxy URL with any credentials removed entirely: scheme,
// host and port, and nothing else. Unlike Redact this leaves no trace
// of a user name either, so it is what a list, a picker or a log line
// shows.
func Host(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}

	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

// HasCredentials says the URL carries a user name, so the console can
// say a credential is stored without showing it.
func HasCredentials(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))

	return err == nil && u.User != nil && u.User.Username() != ""
}

// Redact replaces a password in a proxy URL with a mark, for a log line
// or the console. The user name is left, since knowing which account is
// being used is the point of showing it at all.
func Redact(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User == nil {
		return raw
	}

	if _, hasPassword := u.User.Password(); !hasPassword {
		return raw
	}

	u.User = url.UserPassword(u.User.Username(), "xxxxx")

	return u.String()
}

// compileBypass turns the patterns into one matcher. The always-bypassed
// hosts are included, so they cannot be lost by a list that forgets
// them.
func compileBypass(patterns []string) (func(host string) bool, error) {
	all := make([]string, 0, len(patterns)+len(alwaysBypassed))
	all = append(all, alwaysBypassed...)

	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// path.Match is the glob, and it reports a bad pattern.
		if _, err := path.Match(p, "probe"); err != nil {
			return nil, fmt.Errorf("bypass pattern %q: %w", p, err)
		}

		all = append(all, p)
	}

	return func(host string) bool {
		host = strings.ToLower(strings.Trim(host, "[]"))
		if host == "" {
			return false
		}

		for _, p := range all {
			p = strings.ToLower(strings.Trim(p, "[]"))
			if p == host {
				return true
			}

			// path.Match's separator is /, which a host never has, so a
			// * spans the whole name - `*.example.com` matches
			// `a.b.example.com` as well as `a.example.com`.
			if ok, _ := path.Match(p, host); ok {
				return true
			}
		}

		return false
	}, nil
}

// Result is what a connection test found.
type Result struct {
	// Proxy is the configuration that was tried, with any password
	// redacted, so the answer can be shown as it is.
	Proxy string `json:"proxy"`

	// Target is the host:port the test connected to.
	Target string `json:"target"`

	// Direct says the target is on the bypass list, so the test measured
	// a direct connection and the proxy was not involved.
	Direct bool `json:"direct"`

	// TookMS is how long the connection and any handshake took.
	TookMS int64 `json:"took_ms"`

	// Status is the status the target answered with, 0 when nothing was
	// reached.
	Status int `json:"status,omitzero"`
}

// ErrNotConfigured is a test with no upstream proxy to test.
var ErrNotConfigured = errors.New("upstream: no proxy is configured")

// Test opens one short-lived request through the configuration and
// reports what happened. target is a URL, and the default is a small,
// long-lived page that says little.
func (c Config) Test(ctx context.Context, target string) (Result, error) {
	if !c.Configured() {
		return Result{}, ErrNotConfigured
	}

	if strings.TrimSpace(target) == "" {
		target = "http://example.com/"
	}

	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return Result{}, fmt.Errorf("upstream: %q is not a URL to test against", target)
	}

	proxyFn, err := c.Compile()
	if err != nil {
		return Result{}, err
	}

	res := Result{Proxy: Redact(c.URL), Target: u.Host}

	// Whether this target would even use the proxy is the first thing
	// worth reporting: a bypassed target proves nothing about the proxy.
	probe, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return res, err
	}

	via, err := proxyFn(probe)
	if err != nil {
		return res, err
	}

	res.Direct = via == nil

	tr := &http.Transport{
		Proxy:                 proxyFn,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{Transport: tr, Timeout: 20 * time.Second}

	start := time.Now()

	answer, err := client.Do(probe)
	res.TookMS = time.Since(start).Milliseconds()

	if err != nil {
		return res, fmt.Errorf("upstream: %w", err)
	}

	_ = answer.Body.Close()
	res.Status = answer.StatusCode

	return res, nil
}
