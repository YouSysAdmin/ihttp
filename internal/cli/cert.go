package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/smallstep/truststore"
	"github.com/spf13/cobra"
)

// certOptions is shared by install and uninstall.
type certOptions struct {
	dataDir    string
	caCert     string
	firefox    bool
	java       bool
	skipSystem bool
}

func newCertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert",
		Short: "Manage the CA certificate in the trust stores",
	}

	cmd.AddCommand(newCertInstallCmd(), newCertUninstallCmd(), newCertPathCmd())

	return cmd
}

func addCertFlags(cmd *cobra.Command, o *certOptions) {
	f := cmd.Flags()
	f.StringVar(&o.dataDir, "data-dir", defaultDataDir(), "where the CA lives")
	f.StringVar(&o.caCert, "ca-cert", "", "CA certificate PEM (default <data-dir>/ca.pem)")
	f.BoolVar(&o.firefox, "firefox", false, "also the Firefox trust store")
	f.BoolVar(&o.java, "java", false, "also the Java trust store")
	f.BoolVar(&o.skipSystem, "skip-system", false, "leave the system trust store alone")
}

func (o certOptions) path() string {
	return firstNonEmpty(expandHome(o.caCert), filepath.Join(expandHome(o.dataDir), "ca.pem"))
}

func (o certOptions) truststoreOptions() []truststore.Option {
	var opts []truststore.Option

	if o.skipSystem {
		opts = append(opts, truststore.WithNoSystem())
	}

	if o.firefox {
		opts = append(opts, truststore.WithFirefox())
	}

	if o.java {
		opts = append(opts, truststore.WithJava())
	}

	return opts
}

func newCertInstallCmd() *cobra.Command {
	var o certOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Trust the CA certificate on this machine",
		Long: `Install the CA certificate into the system trust store, and optionally
into Firefox and Java. Run ` + "`ihttp serve`" + ` once first so the certificate exists.
You may be asked for your password.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := o.path()
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("no CA certificate at %s - run `ihttp serve` once to mint it", path)
			}

			if err := truststore.InstallFile(path, o.truststoreOptions()...); err != nil {
				return fmt.Errorf("install certificate: %w", err)
			}

			slog.Info("certificate installed", "path", path)

			return nil
		},
	}

	addCertFlags(cmd, &o)

	return cmd
}

func newCertUninstallCmd() *cobra.Command {
	var o certOptions

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop trusting the CA certificate on this machine",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := truststore.UninstallFile(o.path(), o.truststoreOptions()...); err != nil {
				return fmt.Errorf("uninstall certificate: %w", err)
			}

			slog.Info("certificate removed", "path", o.path())

			return nil
		},
	}

	addCertFlags(cmd, &o)

	return cmd
}

func newCertPathCmd() *cobra.Command {
	var o certOptions

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print where the CA certificate is",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), o.path())

			return nil
		},
	}

	addCertFlags(cmd, &o)

	return cmd
}
