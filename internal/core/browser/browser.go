// Package browser launches a browser pointed at the proxy, with its own
// profile so the operator's everyday browser is not the one whose
// certificate checks are loosened.
//
// Two families, because they are configured in opposite ways.
// Chromium takes everything on the command line.
// Firefox takes nothing there: the proxy is a set of preferences in the profile, and trust is a
// certificate database in the profile - so a profile is written before it starts.
package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Kind is a browser family.
type Kind string

// The families the launcher knows.
const (
	KindChrome  Kind = "chrome"
	KindFirefox Kind = "firefox"
)

// ParseKind reads a --browser value. "" is no browser and not an error.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case "chrome", "chromium", "edge", "brave":
		return KindChrome, nil
	case "firefox":
		return KindFirefox, nil
	default:
		return "", fmt.Errorf("browser: unknown browser %q (chrome or firefox)", s)
	}
}

// ErrNotFound is no known browser binary on this machine.
var ErrNotFound = errors.New("browser: no browser binary found")

// Options say how to launch.
type Options struct {
	Kind Kind

	// ProxyURL is the http proxy the browser is told to use.
	ProxyURL string

	// OpenURL is the page to land on, typically the console.
	OpenURL string

	// ProfileDir is the profile to use. Empty means a fresh temporary
	// one, removed when the browser exits.
	ProfileDir string

	// CACertPath is the CA to trust. Firefox imports it into the profile
	// when certutil is on the PATH. Chromium ignores certificate errors
	// outright and does not need it.
	CACertPath string
}

// Result reports what Launch did beyond starting the process.
type Result struct {
	Cmd *exec.Cmd

	// Binary is the executable that was started.
	Binary string

	// Profile is the profile directory the browser runs with. A temporary
	// one is removed when the browser exits.
	Profile string

	// Warning is advice for the operator when something was not possible
	// - typically that Firefox could not be given the CA.
	Warning string
}

func chromeCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"google-chrome", "chromium",
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("LocalAppData"), `Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
			"chrome.exe", "msedge.exe",
		}
	default:
		return []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
			"brave-browser", "microsoft-edge",
		}
	}
}

func firefoxCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Firefox.app/Contents/MacOS/firefox",
			"/Applications/Firefox Developer Edition.app/Contents/MacOS/firefox",
			"firefox",
		}
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), `Mozilla Firefox\firefox.exe`),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), `Mozilla Firefox\firefox.exe`),
			"firefox.exe",
		}
	default:
		return []string{"firefox", "firefox-esr"}
	}
}

// Find returns the first binary of kind that exists.
func Find(kind Kind) (string, error) {
	var candidates []string

	switch kind {
	case KindFirefox:
		candidates = firefoxCandidates()
	default:
		candidates = chromeCandidates()
	}

	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}

			continue
		}

		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("%w: no %s", ErrNotFound, kind)
}

// Launch starts the browser and returns once it has been started. The
// process is tied to ctx: cancelling it closes the browser, and a
// temporary profile is removed once it exits.
func Launch(ctx context.Context, opts Options) (*Result, error) {
	if opts.Kind == "" {
		opts.Kind = KindChrome
	}

	bin, err := Find(opts.Kind)
	if err != nil {
		return nil, err
	}

	profile := opts.ProfileDir
	temp := false

	if profile == "" {
		profile, err = os.MkdirTemp("", "ihttp-"+string(opts.Kind)+"-")
		if err != nil {
			return nil, fmt.Errorf("browser: profile dir: %w", err)
		}

		temp = true
	}

	var args []string
	var warning string

	switch opts.Kind {
	case KindFirefox:
		args, warning, err = prepareFirefox(ctx, profile, opts)
	default:
		args, err = prepareChrome(profile, opts)
	}

	if err != nil {
		if temp {
			_ = os.RemoveAll(profile)
		}

		return nil, err
	}

	cmd := exec.CommandContext(ctx, bin, args...)

	if opts.Kind == KindFirefox {
		// A release Firefox ignores services.settings.server unless this
		// is set, and then goes to the real remote-settings server for a
		// dozen collections on every start. Nightly honours the pref on
		// its own. Measured: nine requests to Mozilla with the pref alone,
		// none with the variable beside it.
		cmd.Env = append(os.Environ(), "MOZ_REMOTE_SETTINGS_DEVTOOLS=1")
	}

	if err := cmd.Start(); err != nil {
		if temp {
			_ = os.RemoveAll(profile)
		}

		return nil, fmt.Errorf("browser: start %s: %w", bin, err)
	}

	if temp {
		go func() {
			_ = cmd.Wait()
			_ = os.RemoveAll(profile)
		}()
	}

	return &Result{Cmd: cmd, Binary: bin, Profile: profile, Warning: warning}, nil
}

// prepareChrome seeds the profile and returns the flag set.
//
// Chromium is configured two ways at once. Flags cover the proxy and most
// of the background traffic. What flags do not reach - sign-in, the new
// tab page's promotions, search suggestions, autofill - lives in the
// profile's Preferences file, which is JSON Chromium reads on first load,
// so it is written before the first start.
func prepareChrome(profile string, opts Options) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(profile, "Default"), 0o700); err != nil {
		return nil, fmt.Errorf("browser: profile dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(profile, "Default", "Preferences"), []byte(chromePreferences), 0o600); err != nil {
		return nil, fmt.Errorf("browser: write Preferences: %w", err)
	}

	if err := os.WriteFile(filepath.Join(profile, "Local State"), []byte(chromeLocalState), 0o600); err != nil {
		return nil, fmt.Errorf("browser: write Local State: %w", err)
	}

	return chromeArgs(profile, opts), nil
}

// chromePreferences is the per-profile JSON. Every key here is one the
// browser would otherwise ask a Google server about.
const chromePreferences = `{
  "alternate_error_pages": {"enabled": false},
  "autofill": {"credit_card_enabled": false, "profile_enabled": false},
  "browser": {"has_seen_welcome_page": true, "show_home_button": false},
  "credentials_enable_service": false,
  "dns_prefetching": {"enabled": false},
  "enable_do_not_track": true,
  "net": {"network_prediction_options": 2},
  "ntp": {"shortcuts_visible": false, "promo_visible": false, "use_most_visited_tiles": false, "num_personal_suggestions": 0},
  "session": {"restore_on_startup": 4, "startup_urls": []},
  "homepage_is_newtabpage": false,
  "profile": {"password_manager_enabled": false, "default_content_setting_values": {"notifications": 2}},
  "safebrowsing": {"enabled": false, "enhanced": false, "scout_reporting_enabled": false},
  "search": {"suggest_enabled": false},
  "signin": {"allowed": false, "allowed_on_next_startup": false},
  "spellcheck": {"use_spelling_service": false},
  "translate": {"enabled": false},
  "url_keyed_anonymized_data_collection": {"enabled": false}
}
`

// chromeLocalState is the browser-wide JSON: no metrics, no variations.
const chromeLocalState = `{
  "browser": {"enabled_labs_experiments": []},
  "user_experience_metrics": {"reporting_enabled": false},
  "variations_seed": "",
  "variations_compressed_seed": ""
}
`

// chromeArgs is the flag set. Beyond the proxy and the profile it turns
// off what a fresh Chromium does on its own the moment it opens - the
// component updater, sync, safe browsing, phishing detection, domain
// reliability reports, metrics uploads, push registration - because
// every one of those is a request in the log that the operator did not make.
//
// The bypass list is the one thing that must be exact. Chromium skips
// the proxy for loopback by default, and a security tool wants the
// opposite: an application under test on localhost:3000 must be
// proxied. <-loopback> removes that default, and then the console alone
// is put back, so the browser reads the console directly rather than
// logging its own page loads.
func chromeArgs(profile string, opts Options) []string {
	bypass := []string{"<-loopback>", "*.invalid"}
	if host := hostPort(opts.OpenURL); host != "" {
		bypass = append(bypass, host)
	}

	args := []string{
		"--user-data-dir=" + profile,
		"--proxy-server=" + opts.ProxyURL,
		"--proxy-bypass-list=" + strings.Join(bypass, ";"),
		"--ignore-certificate-errors",
		"--test-type",
		"--no-first-run",
		"--no-default-browser-check",
		"--no-service-autorun",
		"--password-store=basic",
		"--use-mock-keychain",
		"--disable-search-engine-choice-screen",

		// The network the browser makes for itself.
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-component-extensions-with-background-pages",
		"--disable-default-apps",
		"--disable-sync",
		"--disable-domain-reliability",
		"--disable-client-side-phishing-detection",
		"--safebrowsing-disable-download-protection",
		"--metrics-recording-only",
		"--disable-breakpad",
		"--disable-crash-reporter",
		"--no-pings",
		"--disable-field-trial-config",
		"--disable-extensions",
		"--allow-browser-signin=false",
		"--disable-signin-promo",
		"--variations-server-url=https://variations.invalid/",
		"--gcm-checkin-url=https://gcm.invalid/checkin",
		"--gcm-registration-url=https://gcm.invalid/register",
		"--gcm-mcs-endpoint=https://gcm.invalid:5228",
		"--disable-features=" + strings.Join([]string{
			"OptimizationHints",
			"OptimizationHintsFetching",
			"OptimizationGuideModelDownloading",
			"MediaRouter",
			"DialMediaRouteProvider",
			"Translate",
			"InterestFeedContentSuggestions",
			"AutofillServerCommunication",
			"CertificateTransparencyComponentUpdater",
			"HttpsUpgrades",
			"LensOverlay",
			"SafeBrowsingEnhancedProtection",
			"SafetyHub",
			"PrivacySandboxSettings4",
			"SidePanelPinning",
			"NetworkTimeServiceQuerying",
			"ChromeWhatsNewUI",
			"NtpMiddleSlotPromo",
			"NtpModules",
			"NtpRealboxMatchOmniboxTheme",
			"AccountConsistency",
			"ChromeSigninIntercept",
			"SigninInterceptBubble",
			"NtpPromo",
			"NtpMobilePromo",
			"NtpOneGoogleBar",
			"NtpLogo",
			"NtpDriveModule",
			"NtpCalendarModule",
			"NtpOutlookCalendarModule",
			"NtpMostRelevantTabResumptionModule",
			"OmniboxOnDeviceHeadSuggestions",
		}, ","),
	}

	if opts.OpenURL != "" {
		args = append(args, opts.OpenURL)
	}

	return args
}

// hostPort is the host:port of a URL, with the default port spelled out
// so it matches the form a bypass list compares against.
func hostPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}

	if u.Port() != "" {
		return u.Host
	}

	if u.Scheme == "https" {
		return u.Hostname() + ":443"
	}

	return u.Hostname() + ":80"
}

// prepareFirefox writes the profile and returns the arguments to start
// with. The CA goes into the profile's certificate database through
// certutil when that tool is available. Without it the profile is told
// to trust the operating system's roots, which is where `ihttp cert
// install` puts the certificate, and the warning says so.
func prepareFirefox(ctx context.Context, profile string, opts Options) ([]string, string, error) {
	prefs, err := FirefoxPrefs(opts.ProxyURL)
	if err != nil {
		return nil, "", err
	}

	if err := os.WriteFile(filepath.Join(profile, "user.js"), []byte(prefs), 0o600); err != nil {
		return nil, "", fmt.Errorf("browser: write user.js: %w", err)
	}

	warning := ""

	if opts.CACertPath != "" {
		if err := importFirefoxCA(ctx, profile, opts.CACertPath); err != nil {
			warning = "Firefox could not be given the CA certificate (" + err.Error() +
				"). Install it with `ihttp cert install` so the profile can trust it through the system store, " +
				"or import ca.pem in Firefox under Settings > Certificates."
		}
	}

	args := []string{"-no-remote", "-profile", profile}
	if opts.OpenURL != "" {
		args = append(args, opts.OpenURL)
	}

	return args, warning, nil
}

// FirefoxPrefs renders the user.js that points a profile at the proxy
// and keeps the browser quiet.
//
// Type 1 is a manual proxy, the same host and port for http and https.
// Everything Firefox fetches for itself on startup - remote settings and
// their attachments, telemetry, push, updates, captive portal probes, safe
// browsing lists, the new tab feed, search suggestions, codec downloads,
// experiments - is switched off, and the endpoints that cannot be turned
// off are pointed at a host under .invalid, which the bypass list keeps
// away from the proxy so it fails on the spot instead of in the log. A
// fresh profile made a hundred requests before the operator made one.
//
// Enterprise roots make the system trust store count, so a CA installed
// there is honoured even when certutil could not write the profile's own.
func FirefoxPrefs(proxyURL string) (string, error) {
	u, err := url.Parse(proxyURL)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("browser: bad proxy url %q", proxyURL)
	}

	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", fmt.Errorf("browser: proxy url %q has no port", proxyURL)
	}

	host := u.Hostname()

	var b strings.Builder

	pref := func(name string, value any) {
		switch v := value.(type) {
		case string:
			fmt.Fprintf(&b, "user_pref(%q, %q);\n", name, v)
		default:
			fmt.Fprintf(&b, "user_pref(%q, %v);\n", name, v)
		}
	}

	section := func(title string) {
		fmt.Fprintf(&b, "\n// %s\n", title)
	}

	b.WriteString("// Written by ihttp. This profile sends everything through the proxy and\n")
	b.WriteString("// makes no requests of its own.\n")

	section("Proxy")
	pref("network.proxy.type", 1)
	pref("network.proxy.http", host)
	pref("network.proxy.http_port", port)
	pref("network.proxy.ssl", host)
	pref("network.proxy.ssl_port", port)
	pref("network.proxy.share_proxy_settings", true)
	pref("network.proxy.no_proxies_on", "localhost, 127.0.0.1, ::1, .invalid")
	pref("network.proxy.allow_hijacking_localhost", false)
	pref("network.trr.mode", 5)
	pref("security.enterprise_roots.enabled", true)

	section("First run and shell")
	pref("browser.shell.checkDefaultBrowser", false)
	pref("browser.startup.homepage_override.mstone", "ignore")
	pref("browser.aboutwelcome.enabled", false)
	pref("browser.startup.page", 0)
	pref("browser.newtabpage.enabled", false)
	pref("browser.newtab.preload", false)
	pref("identity.fxaccounts.enabled", false)
	pref("browser.region.update.enabled", false)
	pref("browser.region.network.url", "")

	section("Remote settings and experiments")
	pref("services.settings.server", "https://remote-settings.invalid/v1")
	pref("app.normandy.enabled", false)
	pref("app.normandy.api_url", "")
	pref("app.shield.optoutstudies.enabled", false)
	pref("messaging-system.rsexperimentloader.enabled", false)
	pref("browser.newtabpage.activity-stream.asrouter.providers.cfr", "")
	pref("browser.newtabpage.activity-stream.asrouter.providers.message-groups", "")
	pref("browser.newtabpage.activity-stream.asrouter.providers.messaging-experiments", "")
	pref("browser.newtabpage.activity-stream.asrouter.providers.whats-new-panel", "")
	pref("security.remote_settings.crlite_filters.enabled", false)
	pref("security.remote_settings.intermediates.enabled", false)
	pref("security.pki.crlite_mode", 0)
	pref("security.OCSP.enabled", 0)
	pref("extensions.blocklist.enabled", false)
	pref("extensions.getAddons.cache.enabled", false)
	pref("extensions.update.enabled", false)
	pref("extensions.update.autoUpdateDefault", false)
	pref("extensions.systemAddon.update.enabled", false)
	pref("extensions.systemAddon.update.url", "")

	section("Telemetry and reporting")
	pref("datareporting.policy.dataSubmissionEnabled", false)
	pref("datareporting.healthreport.uploadEnabled", false)
	pref("datareporting.usage.uploadEnabled", false)
	pref("toolkit.telemetry.enabled", false)
	pref("toolkit.telemetry.unified", false)
	pref("toolkit.telemetry.archive.enabled", false)
	pref("toolkit.telemetry.server", "https://telemetry.invalid")
	pref("toolkit.telemetry.newProfilePing.enabled", false)
	pref("toolkit.telemetry.shutdownPingSender.enabled", false)
	pref("toolkit.telemetry.updatePing.enabled", false)
	pref("toolkit.telemetry.bhrPing.enabled", false)
	pref("toolkit.telemetry.firstShutdownPing.enabled", false)
	pref("toolkit.telemetry.shutdownPingSender.enabledFirstSession", false)
	pref("toolkit.telemetry.coverage.opt-out", true)
	pref("telemetry.fog.test.localhost_port", -1)
	pref("telemetry.fog.init_on_shutdown", false)
	pref("datareporting.healthreport.service.enabled", false)
	pref("toolkit.coverage.opt-out", true)
	pref("toolkit.coverage.endpoint.base", "")
	pref("browser.ping-centre.telemetry", false)
	pref("browser.discovery.enabled", false)
	pref("browser.newtabpage.activity-stream.telemetry", false)
	pref("browser.newtabpage.activity-stream.feeds.telemetry", false)
	pref("browser.urlbar.eventTelemetry.enabled", false)
	pref("browser.search.serpEventTelemetry.enabled", false)
	pref("breakpad.reportURL", "")
	pref("browser.tabs.crashReporting.sendReport", false)
	pref("browser.crashReports.unsubmittedCheck.autoSubmit2", false)
	pref("network.http.speculative-parallel-limit", 0)
	pref("network.dns.disablePrefetch", true)
	pref("network.prefetch-next", false)
	pref("network.predictor.enabled", false)

	section("Updates")
	pref("app.update.auto", false)
	pref("app.update.disabledForTesting", true)
	pref("app.update.checkInstallTime", false)
	pref("app.update.url", "https://updates.invalid/")
	pref("app.update.url.manual", "https://updates.invalid/")
	pref("app.update.url.details", "https://updates.invalid/")
	pref("app.update.staging.enabled", false)
	pref("media.gmp-manager.url", "https://gmp.invalid/")
	pref("media.gmp-manager.updateEnabled", false)
	pref("media.gmp-provider.enabled", false)
	pref("media.gmp-gmpopenh264.enabled", false)
	pref("media.gmp-gmpopenh264.autoupdate", false)
	pref("media.gmp-widevinecdm.enabled", false)
	pref("media.gmp-widevinecdm.autoupdate", false)

	section("Push, captive portal, connectivity")
	pref("dom.push.enabled", false)
	pref("dom.push.connection.enabled", false)
	pref("dom.push.serverURL", "wss://push.invalid/")
	pref("network.captive-portal-service.enabled", false)
	pref("captivedetect.canonicalURL", "")
	pref("network.connectivity-service.enabled", false)
	pref("network.connectivity-service.IPv4.url", "")
	pref("network.connectivity-service.IPv6.url", "")
	pref("network.connectivity-service.DNSv4.domain", "")
	pref("network.connectivity-service.DNSv6.domain", "")

	section("Safe browsing")
	pref("browser.safebrowsing.malware.enabled", false)
	pref("browser.safebrowsing.phishing.enabled", false)
	pref("browser.safebrowsing.downloads.enabled", false)
	pref("browser.safebrowsing.downloads.remote.enabled", false)
	pref("browser.safebrowsing.blockedURIs.enabled", false)
	pref("browser.safebrowsing.provider.google4.updateURL", "")
	pref("browser.safebrowsing.provider.google4.gethashURL", "")
	pref("browser.safebrowsing.provider.google4.dataSharingURL", "")
	pref("browser.safebrowsing.provider.google.updateURL", "")
	pref("browser.safebrowsing.provider.google.gethashURL", "")
	pref("browser.safebrowsing.provider.mozilla.updateURL", "")
	pref("browser.safebrowsing.provider.mozilla.gethashURL", "")

	section("New tab, urlbar and search")
	pref("browser.newtabpage.activity-stream.feeds.topsites", false)
	pref("browser.newtabpage.activity-stream.feeds.system.topsites", false)
	pref("browser.newtabpage.activity-stream.feeds.section.topstories", false)
	pref("browser.newtabpage.activity-stream.feeds.system.topstories", false)
	pref("browser.newtabpage.activity-stream.showSponsored", false)
	pref("browser.newtabpage.activity-stream.showSponsoredTopSites", false)
	pref("browser.newtabpage.activity-stream.showWeather", false)
	pref("browser.newtabpage.activity-stream.system.showWeather", false)
	pref("browser.newtabpage.activity-stream.discoverystream.enabled", false)
	pref("browser.topsites.contile.enabled", false)
	pref("browser.search.suggest.enabled", false)
	pref("browser.urlbar.suggest.searches", false)
	pref("browser.urlbar.suggest.quicksuggest.sponsored", false)
	pref("browser.urlbar.suggest.quicksuggest.nonsponsored", false)
	pref("browser.urlbar.quicksuggest.enabled", false)
	pref("browser.urlbar.merino.enabled", false)
	pref("browser.urlbar.merino.endpointURL", "")
	pref("browser.urlbar.trending.featureGate", false)
	pref("browser.search.update", false)
	pref("browser.translations.enable", false)
	pref("browser.translations.automaticallyPopup", false)

	return b.String(), nil
}

// importFirefoxCA adds the CA to the profile's NSS database with
// certutil, trusted for server authentication. certutil creates the
// database when the profile has none yet.
func importFirefoxCA(ctx context.Context, profile, caPath string) error {
	certutil, err := exec.LookPath("certutil")
	if err != nil {
		return errors.New("certutil is not installed")
	}

	cmd := exec.CommandContext(ctx, certutil, "-A", "-n", "ihttp CA", "-t", "C,,", "-i", caPath, "-d", "sql:"+profile)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("certutil: %s", strings.TrimSpace(string(out)))
	}

	return nil
}
