package browser

import (
	"strings"
	"testing"
)

func TestFirefoxPrefs(t *testing.T) {
	prefs, err := FirefoxPrefs("http://127.0.0.1:6080")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`user_pref("network.proxy.type", 1);`,
		`user_pref("network.proxy.http", "127.0.0.1");`,
		`user_pref("network.proxy.http_port", 6080);`,
		`user_pref("network.proxy.ssl_port", 6080);`,
		`user_pref("security.enterprise_roots.enabled", true);`,
		`user_pref("services.settings.server", "https://remote-settings.invalid/v1");`,
		`user_pref("toolkit.telemetry.enabled", false);`,
		`user_pref("app.update.disabledForTesting", true);`,
		`user_pref("network.proxy.no_proxies_on", "localhost, 127.0.0.1, ::1, .invalid");`,
	} {
		if !strings.Contains(prefs, want) {
			t.Errorf("missing %s in\n%s", want, prefs)
		}
	}

	if _, err := FirefoxPrefs("http://localhost"); err == nil {
		t.Fatal("a proxy url without a port must be refused")
	}
}

func TestParseKind(t *testing.T) {
	cases := map[string]Kind{"": "", "chrome": KindChrome, "Chromium": KindChrome, "firefox": KindFirefox}
	for in, want := range cases {
		got, err := ParseKind(in)
		if err != nil || got != want {
			t.Errorf("ParseKind(%q) = %q, %v", in, got, err)
		}
	}

	if _, err := ParseKind("safari"); err == nil {
		t.Fatal("an unknown browser must be refused")
	}
}

func TestChromeBypassIsExact(t *testing.T) {
	args := chromeArgs("/p", Options{ProxyURL: "http://127.0.0.1:6080", OpenURL: "http://127.0.0.1:6081"})

	var bypass string
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--proxy-bypass-list="); ok {
			bypass = v
		}
	}

	if bypass != "<-loopback>;*.invalid;127.0.0.1:6081" {
		t.Fatalf("bypass = %q", bypass)
	}

	if hostPort("https://example.com") != "example.com:443" || hostPort("nope") != "" {
		t.Fatal("hostPort default ports")
	}
}
