// Package cli wires the cobra commands the ihttp binary exposes. The
// product is operated from the console, so this surface is small: run
// it, manage the CA certificate, print the version.
package cli

import (
	"github.com/spf13/cobra"

	"github.com/yousysadmin/ihttp/pkg"
)

// NewRoot builds the root command.
func NewRoot() *cobra.Command {
	var logs logOptions

	root := &cobra.Command{
		Use:   pkg.AppName,
		Short: "An HTTP toolkit for security research: MITM proxy, request log, intercept and sender",

		// Errors are reported once, by main through the logger, so the
		// line looks like every other line the tool prints.
		SilenceUsage:  true,
		SilenceErrors: true,

		// Runs before every subcommand: the environment fills in the
		// flags that were not given, then the logger is built from them
		// so the command's first line already goes through it.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := bindEnv(cmd); err != nil {
				return err
			}

			_, err := setupLogging(logs)

			return err
		},
	}

	addLogFlags(root.PersistentFlags(), &logs)

	// The defaults are installed right away, so an error before the
	// flags are read - an unknown flag, a bad level - is reported through
	// the same logger as everything else.
	_, _ = setupLogging(logs)

	root.AddCommand(newServeCmd())
	root.AddCommand(newCertCmd())
	root.AddCommand(newBrowserCmd())
	root.AddCommand(newEnvCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newVersionCmd())

	return root
}
