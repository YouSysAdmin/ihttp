package cli

import (
	"log/slog"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/yousysadmin/ihttp/internal/mcp"
	"github.com/yousysadmin/ihttp/pkg"
)

// newMCPCmd serves the request log to an AI agent over the Model
// Context Protocol, on stdin and stdout.
//
// A separate process talking to a running ihttp over its console API,
// not a mode of `serve`: an agent's client starts and stops it whenever
// it likes, and it must not be able to take the proxy down with it.
func newMCPCmd() *cobra.Command {
	var (
		addr       string
		allowWrite bool
		noRedact   bool
	)

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the request log to an AI agent over MCP, on stdin and stdout",
		Long: `Serve a running ihttp to an AI agent over the Model Context Protocol.

Add it to Claude Code with:

  claude mcp add ihttp -- ihttp mcp

The agent can then search the log in the filter query language, read an
exchange, and hand it back to you as a curl command. What makes this
worth having is the query: an agent asks

  res.statusCode >= 500 AND req.host = api.example.com AND res.ttfb > 1s

instead of paging through a log.

This is the one place captured traffic can leave the machine, since the
agent may be a model on somebody else's hardware. So:

  - credential headers and secret-looking query values are masked unless
    --no-redact says otherwise. Bodies are NOT scanned; a token under an
    unusual name in a body will go out as it is.
  - nothing is changed without --allow-write, which adds opening a
    project, re-sending a request and answering a held exchange.

It needs an ihttp already running: it talks to the console API, so it
works against an instance in a container or on another host.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Interrupted, or the agent's client closing stdin, ends the
			// session. The proxy is another process and is unaffected.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			client := mcp.NewClient(addr)
			tools := mcp.Tools(client, mcp.Options{AllowWrite: allowWrite, Redact: !noRedact})

			// The process logger, installed by the root command, writes
			// to stderr. It must never write to stdout: that is the
			// protocol channel, and a log line on it is a parse error at
			// the other end.
			srv := mcp.NewServer(pkg.AppName, pkg.Version, tools, slog.Default())

			return srv.Serve(ctx, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	f := cmd.Flags()
	f.StringVar(&addr, "addr", "127.0.0.1:6081", "the console of the ihttp to serve")
	f.BoolVar(&allowWrite, "allow-write", false, "also offer the tools that change something")
	f.BoolVar(&noRedact, "no-redact", false, "show credentials to the agent as captured")

	return cmd
}
