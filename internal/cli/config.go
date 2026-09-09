package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// envPrefix is what every flag can also be set through: --proxy-addr is
// IHTTP_PROXY_ADDR.
const envPrefix = "IHTTP_"

// bindEnv gives every flag on fs its environment variable as the default
// when the flag itself was not passed. Called after parsing, so a flag
// on the command line always wins.
func bindEnv(cmd *cobra.Command) error {
	var firstErr error

	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed || firstErr != nil {
			return
		}

		name := envPrefix + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))

		v, ok := os.LookupEnv(name)
		if !ok {
			return
		}

		if err := f.Value.Set(v); err != nil {
			firstErr = fmt.Errorf("%s: %w", name, err)
		}
	})

	return firstErr
}

// defaultDataDir is ~/.ihttp, or the current directory when there is no
// home to speak of.
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ihttp"
	}

	return filepath.Join(home, ".ihttp")
}

// expandHome turns a leading ~ into the home directory, so a path typed
// the way a shell would take it works in an env var too.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}

	return p
}
