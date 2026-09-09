package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/ihttp/pkg"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the binary version",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", pkg.AppName, pkg.Version)
		},
	}
}
