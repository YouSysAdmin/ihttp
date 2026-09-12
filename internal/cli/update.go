package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/ihttp/pkg"
	"github.com/yousysadmin/ihttp/pkg/update"
)

// newUpdateCmd asks GitHub what the latest release is and, unless only
// asked to look, replaces the running binary with it. The download is
// checked against the release's own checksums file before anything is
// written, and the binary is swapped in by rename, so a failed update
// leaves the working one in place.
//
// A binary installed by a package manager - Homebrew, a .deb - is best
// updated by that manager instead: this writes over the file in place,
// and the manager will not know.
func newUpdateCmd() *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update to the latest release",
		Long: `Download the latest release from GitHub and replace this binary.

  ihttp update           install the latest release
  ihttp update --check   only say whether a newer one exists

The archive is verified against the release checksums before the binary
is replaced, and the old one is only removed once the new one is in
place.

A development build - a plain ` + "`go build`" + `, which reports "devel" - has no
version to compare against, so --check says nothing and a plain update
installs the latest release.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			if !check {
				return update.DownloadAndReplace(cmd.Context(), pkg.Version, out)
			}

			if update.IsDevBuild(pkg.Version) {
				_, err := fmt.Fprintf(out,
					"This is a development build (%s), so there is nothing to compare against.\n", pkg.Version)

				return err
			}

			res := update.CheckLatestVersion(cmd.Context(), pkg.Version)
			if res.Err != nil {
				return res.Err
			}

			if !res.Available() {
				_, err := fmt.Fprintf(out, "%s %s is the latest release.\n", pkg.AppName, pkg.Version)

				return err
			}

			_, err := fmt.Fprintf(out, "%s v%s is available - running %s.\nInstall it with: %s update\n",
				pkg.AppName, res.LatestVersion, pkg.Version, pkg.AppName)

			return err
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "only report whether a newer release exists, change nothing")

	return cmd
}
