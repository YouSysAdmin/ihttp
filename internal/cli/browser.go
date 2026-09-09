package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/ihttp/internal/core/browser"
)

// newBrowserCmd launches a browser against an ihttp that is already
// running, the same way `serve --browser` does. The browser is closed
// when this command is interrupted.
func newBrowserCmd() *cobra.Command {
	var (
		proxyURL string
		open     string
		dataDir  string
		caCert   string
	)

	cmd := &cobra.Command{
		Use:   "browser chrome|firefox",
		Short: "Launch a browser with a throwaway profile pointed at a running proxy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, err := browser.ParseKind(args[0])
			if err != nil || kind == "" {
				return fmt.Errorf("browser: expected chrome or firefox, got %q", args[0])
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			res, err := browser.Launch(ctx, browser.Options{
				Kind:       kind,
				ProxyURL:   proxyURL,
				OpenURL:    open,
				CACertPath: firstNonEmpty(expandHome(caCert), filepath.Join(expandHome(dataDir), "ca.pem")),
			})
			if err != nil {
				return err
			}

			slog.Info("launched "+string(kind), "binary", res.Binary, "proxy", proxyURL)
			slog.Debug("browser profile", "path", res.Profile, "pid", res.Cmd.Process.Pid)

			if res.Warning != "" {
				slog.Warn(res.Warning)
			}

			// Wait for the browser or for Ctrl+C, whichever comes first.
			done := make(chan error, 1)
			go func() { done <- res.Cmd.Wait() }()

			select {
			case <-ctx.Done():
				return nil
			case <-done:
				return nil
			}
		},
	}

	f := cmd.Flags()
	f.StringVar(&proxyURL, "proxy-url", "http://localhost:8080", "the running proxy")
	f.StringVar(&open, "open", "http://127.0.0.1:8081", "page to open, typically the console")
	f.StringVar(&dataDir, "data-dir", defaultDataDir(), "where the CA lives")
	f.StringVar(&caCert, "ca-cert", "", "CA certificate PEM (default <data-dir>/ca.pem)")

	return cmd
}
