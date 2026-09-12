package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	logger "github.com/yousysadmin/go-logger"

	"github.com/yousysadmin/ihttp/internal/core/blobstore"
	"github.com/yousysadmin/ihttp/internal/core/browser"
	"github.com/yousysadmin/ihttp/internal/core/certgen"
	"github.com/yousysadmin/ihttp/internal/core/clientcert"
	"github.com/yousysadmin/ihttp/internal/core/eventbus"
	"github.com/yousysadmin/ihttp/internal/core/upstream"
	"github.com/yousysadmin/ihttp/internal/database"
	"github.com/yousysadmin/ihttp/internal/domain/automation"
	"github.com/yousysadmin/ihttp/internal/domain/instance"
	"github.com/yousysadmin/ihttp/internal/domain/intercept"
	"github.com/yousysadmin/ihttp/internal/domain/project"
	"github.com/yousysadmin/ihttp/internal/domain/protoschema"
	"github.com/yousysadmin/ihttp/internal/domain/reqlog"
	"github.com/yousysadmin/ihttp/internal/domain/rules"
	"github.com/yousysadmin/ihttp/internal/domain/sender"
	"github.com/yousysadmin/ihttp/internal/domain/transfer"
	"github.com/yousysadmin/ihttp/internal/domain/upstreams"
	"github.com/yousysadmin/ihttp/internal/proxy"
	"github.com/yousysadmin/ihttp/internal/server"
	"github.com/yousysadmin/ihttp/pkg"
	"github.com/yousysadmin/ihttp/web"
)

// serveOptions is every flag of `ihttp serve`.
type serveOptions struct {
	proxyAddr string
	addr      string
	dataDir   string
	dbPath    string
	caCert    string
	caKey     string
	maxBodyMB int
	insecure  bool
	h2        bool
	wsExtLive bool

	bodyBlobMB int

	upstreamProxy  string
	noUpstreamList []string
	clientCerts    []string

	chrome      bool
	firefox     bool
	browser     string
	shutdownSec int
}

func newServeCmd() *cobra.Command {
	var o serveOptions

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the proxy and the console",
		Long: `Run the MITM proxy on one port and the web console on another.

Every flag can also be given as an environment variable: --proxy-addr is
IHTTP_PROXY_ADDR, --data-dir is IHTTP_DATA_DIR, and so on. A flag on the
command line wins over the environment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), o)
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.proxyAddr, "proxy-addr", ":6080", "address the proxy listens on")
	f.StringVar(&o.addr, "addr", "127.0.0.1:6081", "address the console and API listen on")
	f.StringVar(&o.dataDir, "data-dir", defaultDataDir(), "where the database and CA live")
	f.StringVar(&o.dbPath, "db", "", "database file (default <data-dir>/ihttp.db)")
	f.StringVar(&o.caCert, "ca-cert", "", "CA certificate PEM (default <data-dir>/ca.pem)")
	f.StringVar(&o.caKey, "ca-key", "", "CA private key PEM (default <data-dir>/ca_key.pem)")
	f.IntVar(&o.maxBodyMB, "max-body-mb", 16, "largest request or response body kept, in MiB")
	f.IntVar(&o.bodyBlobMB, "body-blob-mb", 1,
		"bodies this size or larger are kept in files beside the database rather than in it, in MiB - 0 keeps every body in the database")
	f.BoolVar(&o.insecure, "insecure", false, "accept any upstream certificate, in the proxy and the sender")
	f.BoolVar(&o.h2, "h2", true, "speak HTTP/2 to clients inside CONNECT tunnels when they offer it")
	f.BoolVar(&o.wsExtLive, "ws-extensions", false,
		"pass Sec-WebSocket-Extensions through, so compression is negotiated and frames are recorded as opaque bytes")
	f.StringVar(&o.upstreamProxy, "upstream-proxy", "",
		"proxy ihttp goes out through: http://, https://, socks5:// - credentials go in IHTTP_UPSTREAM_PROXY, not on a command line")
	f.StringSliceVar(&o.noUpstreamList, "no-upstream-proxy", nil,
		"host globs reached directly, e.g. *.internal - localhost is always direct")
	f.StringArrayVar(&o.clientCerts, "client-cert", nil,
		"present a client certificate to an upstream that asks: <host glob>=<cert file>[,<key file>], repeatable")
	f.StringVar(&o.browser, "browser", "", "launch a browser configured for the proxy: chrome or firefox")
	f.BoolVar(&o.chrome, "chrome", false, "same as --browser chrome")
	f.BoolVar(&o.firefox, "firefox", false, "same as --browser firefox")
	f.IntVar(&o.shutdownSec, "shutdown-timeout", 10, "seconds to wait for in-flight requests on exit")

	return cmd
}

func runServe(ctx context.Context, o serveOptions) error {
	log := slog.Default()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// A flag that cannot be honoured fails here, before a port is taken
	// or a database opened.
	kind, err := browserKind(o)
	if err != nil {
		return err
	}

	o.dataDir = expandHome(o.dataDir)
	dbPath := firstNonEmpty(expandHome(o.dbPath), filepath.Join(o.dataDir, "ihttp.db"))
	caCert := firstNonEmpty(expandHome(o.caCert), filepath.Join(o.dataDir, "ca.pem"))
	caKey := firstNonEmpty(expandHome(o.caKey), filepath.Join(o.dataDir, "ca_key.pem"))

	log.Debug("resolved paths", "data_dir", o.dataDir, "db", dbPath, "ca_cert", caCert, "ca_key", caKey)
	log.Debug("options", "max_body_mb", o.maxBodyMB, "insecure", o.insecure, "h2", o.h2,
		"ws_extensions", o.wsExtLive,
		"shutdown_timeout_sec", o.shutdownSec)

	// One configuration for every way out: the MITM's upstream, the
	// sender and the automation. Refused here rather than on the first
	// request that would have used it.
	upstreamCfg := upstream.Config{URL: o.upstreamProxy, Bypass: o.noUpstreamList}

	if _, err := upstreamCfg.Compile(); err != nil {
		return err
	}

	if upstreamCfg.Configured() {
		// Redacted: a password in the URL must not reach a log line.
		log.Info("going out through an upstream proxy",
			"proxy", upstream.Redact(o.upstreamProxy), "direct", o.noUpstreamList)
	}

	certEntries := make([]clientcert.Entry, 0, len(o.clientCerts))

	for _, spec := range o.clientCerts {
		e, err := clientcert.Parse(spec)
		if err != nil {
			return err
		}

		certEntries = append(certEntries, e)
	}

	clientCerts, err := clientcert.Load(certEntries)
	if err != nil {
		return err
	}

	if clientCerts != nil {
		log.Info("presenting a client certificate to these hosts when asked", "hosts", clientCerts.Hosts())
	}

	caCertificate, caSigner, minted, err := certgen.LoadOrCreateCA(caCert, caKey, pkg.AppName+" CA", pkg.AppName)
	if err != nil {
		return err
	}

	if minted {
		log.Info("minted a new CA certificate - install it with `ihttp cert install`", "path", caCert)
	} else {
		log.Debug("loaded the CA certificate", "path", caCert, "subject", caCertificate.Subject.CommonName,
			"not_after", caCertificate.NotAfter.Format(time.RFC3339))
	}

	ca, err := certgen.New(caCertificate, caSigner)
	if err != nil {
		return err
	}

	db, err := database.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	log.Debug("database opened", "path", dbPath)

	// Large bodies live beside the database rather than inside it, so a
	// log of big responses does not leave a bbolt file that never
	// shrinks. Nothing above the log's store knows.
	blobDir := filepath.Join(o.dataDir, "bodies")

	blobs, err := blobstore.New(blobDir, o.bodyBlobMB<<20)
	if err != nil {
		return err
	}

	consoleURL := browserURL(o.addr)
	proxyURL := browserURL(o.proxyAddr)

	bus := eventbus.New()
	projects := project.NewService(project.NewStore(db), bus, log)
	logStore := reqlog.NewStore(db, blobs)
	logs := reqlog.NewService(logStore, projects, bus, log.With("svc", "reqlog"))

	// A body file whose record never committed - a crash between the
	// two writes - is nobody's, and nothing else will ever remove it.
	if n, err := logStore.SweepBodies(ctx); err != nil {
		log.Warn("could not sweep orphaned body files", "err", err)
	} else if n > 0 {
		log.Info("removed body files no entry points at", "count", n)
	}

	// A deleted project's buckets are gone, so its body files can no
	// longer be reconciled against anything: they are dropped when the
	// deletion is announced. Missing the event costs disk until the
	// next start, when the sweep above finds them - never correctness.
	go func() {
		for ev := range bus.Subscribe(ctx) {
			if ev.Type != "project.deleted" {
				continue
			}

			if data, ok := ev.Data.(map[string]string); ok {
				logStore.DropProjectBodies(data["id"])
			}
		}
	}()
	interceptor := intercept.NewService(projects, bus, log.With("svc", "intercept"))
	// The list of ways out, and which one the open project uses. Built
	// after the project service, since it asks it on every request.
	ways, err := upstreams.NewService(upstreams.NewStore(db), projects, upstreamCfg, log.With("svc", "upstreams"))
	if err != nil {
		return err
	}

	upstreamProxy := ways.ProxyFunc()

	sending := sender.NewService(sender.NewStore(db), projects, logs, bus, sender.Options{
		MaxBody:            int64(o.maxBodyMB) << 20,
		InsecureSkipVerify: o.insecure,
		UpstreamProxy:      upstreamProxy,
		ClientCerts:        clientCerts,
		Logger:             log.With("svc", "sender"),
	})
	automating := automation.NewService(automation.NewStore(db), projects, logs, bus, automation.Options{
		InsecureSkipVerify: o.insecure,
		UpstreamProxy:      upstreamProxy,
		ClientCerts:        clientCerts,
		Logger:             log.With("svc", "automation"),
	})

	if err := projects.Restore(ctx); err != nil {
		log.Warn("could not reopen the last project", "err", err)
	}

	ruleHook := rules.NewService(projects.RulesProvider(), log.With("svc", "rules"))

	// A captured value belongs to the project it was captured in, so it
	// goes when the open project does.
	projects.Watch(func(_, _ *project.Active) { ruleHook.ClearVariables() })

	// One event per capture, which is rare by construction: a token is
	// captured on a sign-in, not on every request.
	ruleHook.OnCapture(func(v rules.Var) { bus.Emit("rules.captured", v) })

	// Rules first, so a mock or a rewrite is what the intercept shows and
	// the log records. Intercept before logging, so the log records what
	// was actually sent - the request as a person edited it, and the
	// response they let through.
	mitm := proxy.New(ca, []proxy.Hook{ruleHook, interceptor, logs}, proxy.Options{
		MaxBody:            int64(o.maxBodyMB) << 20,
		ConsoleURL:         consoleURL,
		InsecureSkipVerify: o.insecure,
		DisableHTTP2:       !o.h2,

		KeepWebSocketExtensions: o.wsExtLive,
		UpstreamProxy:           upstreamProxy,
		ClientCerts:             clientCerts,
		Passthrough:             projects.PassthroughProvider(),
		Logger:                  log.With("svc", "proxy"),
	})

	proxySrv := &http.Server{
		Addr:              o.proxyAddr,
		Handler:           mitm,
		ReadHeaderTimeout: 30 * time.Second,
		ErrorLog:          logger.NewStdLogger(log.With("svc", "proxy"), slog.LevelWarn),
	}

	consoleSrv := server.New(o.addr, server.Deps{
		Projects:   projects,
		ReqLogs:    logs,
		Intercept:  interceptor,
		Sender:     sending,
		Automation: automating,
		Transfer:   transfer.NewService(db, projects, log.With("svc", "transfer")),
		Schemas:    protoschema.NewService(protoschema.NewStore(db), projects),
		CA:         ca,
		Bus:        bus,
		Console:    web.FS(),
		Logger:     log.With("svc", "console"),
		Info: server.Info{
			Version:    pkg.Version,
			ProxyAddr:  o.proxyAddr,
			ProxyURL:   proxyURL,
			ConsoleURL: consoleURL,
			DataDir:    o.dataDir,
			CAPath:     caCert,
			ProbeURL:   "http://" + proxy.DefaultProbeHost,
			// Redacted: the console shows this, and a password in it
			// would be shown with it.
			UpstreamProxy:  upstream.Redact(o.upstreamProxy),
			UpstreamBypass: o.noUpstreamList,
			ClientCerts:    clientCerts.Summaries(),
		},
		Upstreams: ways,
		Instance:  instance.NewService(instance.NewStore(db), log.With("svc", "instance")),
		Rules:     ruleHook,
	})

	errs := make(chan error, 2)

	listenAndServe := func(name string, srv *http.Server) {
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			errs <- fmt.Errorf("%s: listen %s: %w", name, srv.Addr, err)

			return
		}

		log.Info(name+" listening", "addr", ln.Addr().String())

		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- fmt.Errorf("%s: %w", name, err)
		}
	}

	go listenAndServe("proxy", proxySrv)
	go listenAndServe("console", consoleSrv)

	log.Info(fmt.Sprintf("%s %s is running", pkg.AppName, pkg.Version), "console", consoleURL, "proxy", proxyURL)

	if !web.Available() {
		log.Warn("no console is built into this binary - the API is up, the pages are not")
	}

	if kind != "" {
		res, err := browser.Launch(ctx, browser.Options{
			Kind:       kind,
			ProxyURL:   proxyURL,
			OpenURL:    consoleURL,
			CACertPath: caCert,
		})

		switch {
		case err != nil:
			log.Warn("could not launch a browser", "err", err)
		default:
			log.Info("launched "+string(kind), "binary", res.Binary)
			log.Debug("browser profile", "path", res.Profile, "pid", res.Cmd.Process.Pid)

			if res.Warning != "" {
				log.Warn(res.Warning)
			}
		}
	}

	select {
	case <-ctx.Done():
		log.Info("shutting down - press Ctrl+C again to force")
		stop()
	case err := <-errs:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(o.shutdownSec)*time.Second)
	defer cancel()

	// The console first: it holds the event streams, which end the
	// moment their context does. The proxy may have tunnels open that
	// only close when the client hangs up, and Shutdown waits on them.
	if err := consoleSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("console shutdown", "err", err)
	} else {
		log.Debug("console stopped")
	}

	if err := proxySrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("proxy shutdown incomplete, closing", "err", err)
		_ = proxySrv.Close()
	} else {
		log.Debug("proxy stopped")
	}

	return nil
}

// browserKind resolves --browser and the --chrome and --firefox
// shorthands. Two of them given and disagreeing is an error rather than
// a guess.
func browserKind(o serveOptions) (browser.Kind, error) {
	kind, err := browser.ParseKind(o.browser)
	if err != nil {
		return "", err
	}

	if o.chrome && o.firefox {
		return "", errors.New("--chrome and --firefox disagree")
	}

	for _, short := range []struct {
		set  bool
		flag string
		kind browser.Kind
	}{
		{o.chrome, "--chrome", browser.KindChrome},
		{o.firefox, "--firefox", browser.KindFirefox},
	} {
		if !short.set {
			continue
		}

		if kind != "" && kind != short.kind {
			return "", fmt.Errorf("%s and --browser %s disagree", short.flag, kind)
		}

		return short.kind, nil
	}

	return kind, nil
}

// browserURL turns a listen address into the URL a browser on this
// machine reaches it at. An unspecified host is localhost.
func browserURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}

	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "localhost"
	}

	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}

	return "http://" + host + ":" + port
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
