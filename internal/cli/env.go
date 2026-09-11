package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

// Shell names `ihttp env --shell` takes. `env` is not a shell: it prints
// bare KEY=value lines, which is what a .env file and `docker run
// --env-file` read.
const (
	shellSh         = "sh"
	shellFish       = "fish"
	shellPowerShell = "powershell"
	shellCmd        = "cmd"
	shellEnv        = "env"
)

// javaMarker opens the block `ihttp env` appends to JAVA_TOOL_OPTIONS.
// Re-running strips the old block first, so sourcing twice does not
// stack two sets of proxy properties.
const javaMarker = "-Dihttp.proxy=true"

// envVar is one variable and the value it is being given. An empty value
// means the variable is being removed.
type envVar struct {
	name  string
	value string
}

// newEnvCmd prints the environment a runtime needs to send its traffic
// through the proxy, in the syntax of the shell that is going to read
// it. It changes nothing itself - the shell does, when the output is
// evaluated - so it is safe to run and read first.
func newEnvCmd() *cobra.Command {
	var (
		shell           string
		proxyURL        string
		addr            string
		dataDir         string
		caCert          string
		unset           bool
		replaceCABundle bool
	)

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print the proxy environment for a shell to evaluate",
		Long: `Print the environment variables that make a runtime use the proxy.

The command only prints; the shell applies it:

  eval "$(ihttp env)"                 sh, bash, zsh
  ihttp env --shell fish | source     fish
  ihttp env --shell powershell | iex  PowerShell
  ihttp env --shell env > .env        a file for docker --env-file

Undo it in the same shell with ihttp env --unset.

Certificate trust is only hinted at, never replaced: NODE_EXTRA_CA_CERTS
adds the CA to what Node already trusts, while SSL_CERT_FILE and its
kind REPLACE a runtime's whole trust store and would throw away the
system and corporate anchors, so they are left alone unless
--replace-ca-bundle says otherwise.

Loopback is proxied on purpose, so an app under test on localhost can be
debugged, and only the console's own address is left out. A client that
ignores the port in no_proxy - curl does - sends its console requests
through the proxy too, where they work and are logged as noise.

Routing is all this prepares; a runtime still decides for itself:

  - Python's urllib and requests follow the variables, but verify against
    their own CA bundle, so HTTPS needs --replace-ca-bundle or the CA
    added to certifi.
  - Node's fetch does not read the variables before Node 24, where
    NODE_USE_ENV_PROXY=1 - set here - turns it on. Earlier versions need
    an explicit agent.
  - A JVM reads the properties in JAVA_TOOL_OPTIONS, and needs the CA in
    its own trust store: ihttp cert install --java.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if shell == "" {
				shell = guessShell()
			}

			ca := firstNonEmpty(expandHome(caCert), filepath.Join(expandHome(dataDir), "ca.pem"))

			out, err := renderEnv(shell, envVars(proxyURL, addr, ca, replaceCABundle), unset)
			if err != nil {
				return err
			}

			_, err = fmt.Fprint(cmd.OutOrStdout(), out)

			return err
		},
	}

	f := cmd.Flags()
	f.StringVar(&shell, "shell", "", "sh, fish, powershell, cmd or env (default: from $SHELL)")
	f.StringVar(&proxyURL, "proxy-url", "http://localhost:6080", "the running proxy")
	f.StringVar(&addr, "addr", "127.0.0.1:6081", "the console's address, which is left unproxied")
	f.StringVar(&dataDir, "data-dir", defaultDataDir(), "where the CA lives")
	f.StringVar(&caCert, "ca-cert", "", "CA certificate PEM (default <data-dir>/ca.pem)")
	f.BoolVar(&unset, "unset", false, "print the lines that undo it instead")
	f.BoolVar(&replaceCABundle, "replace-ca-bundle", false,
		"also set the variables that REPLACE a runtime's trust store, discarding its own anchors")

	return cmd
}

// envVars is the environment itself, in the order it is printed.
func envVars(proxyURL, consoleAddr, caPath string, replaceCABundle bool) []envVar {
	// Both cases on purpose: which one a client reads is not something
	// the person running it should have to know. curl, for one, reads
	// only the lower-case http_proxy for plain HTTP, while Go and Python
	// read the upper-case names.
	out := []envVar{
		{"HTTP_PROXY", proxyURL},
		{"http_proxy", proxyURL},
		{"HTTPS_PROXY", proxyURL},
		{"https_proxy", proxyURL},
		{"ALL_PROXY", proxyURL},
		{"all_proxy", proxyURL},
	}

	// Only the console, never localhost as a whole: an app under test on
	// localhost:3000 has to be proxied or there is nothing to debug.
	// This is the same call the Chromium bypass list makes.
	//
	// A port in a no_proxy entry is not read by every client. Measured
	// against curl 8.22: `no_proxy=127.0.0.1:8099` still proxies the
	// console, `no_proxy=127.0.0.1` does not, so curl ignores the port.
	// Go's httpproxy and Python's requests do read it. Host and port is
	// still the right value - the cost where it is ignored is that the
	// client's own console requests are proxied and logged, which works
	// and is only noise, while a bare host would stop a local app under
	// test from being proxied at all. Re-measure before changing this.
	out = append(out,
		envVar{"NO_PROXY", consoleAddr},
		envVar{"no_proxy", consoleAddr},
	)

	if caPath != "" {
		out = append(out,
			envVar{"IHTTP_ROOT_CA", caPath},
			// Additive: Node keeps its own bundle and trusts this too.
			envVar{"NODE_EXTRA_CA_CERTS", caPath},
		)
	}

	// Node core has never read the proxy variables on its own. Node 24
	// added this switch to make fetch honour them - measured on Node
	// 22.14 it is ignored, and an unknown variable costs nothing, so it
	// is set either way.
	out = append(out, envVar{"NODE_USE_ENV_PROXY", "1"})

	out = append(out, envVar{"JAVA_TOOL_OPTIONS", javaToolOptions(proxyURL)})

	if replaceCABundle && caPath != "" {
		out = append(out,
			envVar{"SSL_CERT_FILE", caPath},
			envVar{"REQUESTS_CA_BUNDLE", caPath},
			envVar{"CURL_CA_BUNDLE", caPath},
			envVar{"GIT_SSL_CAINFO", caPath},
		)
	}

	return out
}

// javaToolOptions keeps whatever JAVA_TOOL_OPTIONS already says and adds
// the proxy properties, with any block an earlier run added removed
// first. A JVM reads proxies from properties, not from HTTP_PROXY.
func javaToolOptions(proxyURL string) string {
	host, port := hostPort(proxyURL)
	if host == "" {
		return os.Getenv("JAVA_TOOL_OPTIONS")
	}

	block := strings.Join([]string{
		javaMarker,
		"-Dhttp.proxyHost=" + host,
		"-Dhttp.proxyPort=" + port,
		"-Dhttps.proxyHost=" + host,
		"-Dhttps.proxyPort=" + port,
	}, " ")

	kept := stripJavaBlock(os.Getenv("JAVA_TOOL_OPTIONS"))
	if kept == "" {
		return block
	}

	return kept + " " + block
}

// stripJavaBlock removes the properties a previous run appended, so the
// value neither grows nor loses what the person set themselves.
func stripJavaBlock(current string) string {
	fields := strings.Fields(current)
	kept := make([]string, 0, len(fields))

	inBlock := false

	for _, f := range fields {
		if f == javaMarker {
			inBlock = true

			continue
		}

		if inBlock && (strings.HasPrefix(f, "-Dhttp.proxy") || strings.HasPrefix(f, "-Dhttps.proxy")) {
			continue
		}

		inBlock = false
		kept = append(kept, f)
	}

	return strings.Join(kept, " ")
}

// hostPort splits a proxy URL into its host and port, defaulting the
// port to 80 the way a URL without one means.
func hostPort(proxyURL string) (host, port string) {
	s := proxyURL
	if _, after, ok := strings.Cut(s, "://"); ok {
		s = after
	}

	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), "/")

	// An IPv6 literal keeps its brackets around the host.
	if strings.HasPrefix(s, "[") {
		if before, rest, ok := strings.CutLast(s, "]"); ok {
			host = before + "]"
			if p, ok := strings.CutPrefix(rest, ":"); ok {
				port = p
			}

			return host, firstNonEmpty(port, "80")
		}
	}

	host, port, found := strings.Cut(s, ":")
	if !found {
		port = "80"
	}

	return host, port
}

// renderEnv writes the variables in shell's syntax, or the lines that
// remove them when unset.
func renderEnv(shell string, vars []envVar, unset bool) (string, error) {
	var b strings.Builder

	comment := func(text string) {
		switch shell {
		case shellCmd:
			fmt.Fprintf(&b, "rem %s\n", text)
		default:
			fmt.Fprintf(&b, "# %s\n", text)
		}
	}

	switch shell {
	case shellSh, shellFish, shellPowerShell, shellCmd, shellEnv:
	default:
		return "", fmt.Errorf("env: unknown shell %q - expected sh, fish, powershell, cmd or env", shell)
	}

	if unset {
		comment("ihttp: removing the proxy environment from this shell")
	} else {
		comment("ihttp: this shell's traffic now goes through the proxy")
		comment("undo it with: ihttp env --unset")
	}

	for _, v := range vars {
		// A variable with nothing to say is skipped rather than set to
		// the empty string, which some clients read as a proxy of "".
		if !unset && v.value == "" {
			continue
		}

		b.WriteString(line(shell, v, unset))
	}

	return b.String(), nil
}

func line(shell string, v envVar, unset bool) string {
	if unset {
		switch shell {
		case shellFish:
			return fmt.Sprintf("set -e %s\n", v.name)
		case shellPowerShell:
			return fmt.Sprintf("Remove-Item -ErrorAction SilentlyContinue Env:\\%s\n", v.name)
		case shellCmd:
			return fmt.Sprintf("set %s=\n", v.name)
		case shellEnv:
			return fmt.Sprintf("%s=\n", v.name)
		default:
			return fmt.Sprintf("unset %s\n", v.name)
		}
	}

	switch shell {
	case shellFish:
		return fmt.Sprintf("set -gx %s %s\n", v.name, quotePOSIX(v.value))
	case shellPowerShell:
		return fmt.Sprintf("$env:%s = %s\n", v.name, quotePowerShell(v.value))
	case shellCmd:
		// cmd has no quoting worth the name: the value goes as it is.
		return fmt.Sprintf("set %s=%s\n", v.name, v.value)
	case shellEnv:
		return fmt.Sprintf("%s=%s\n", v.name, v.value)
	default:
		return fmt.Sprintf("export %s=%s\n", v.name, quotePOSIX(v.value))
	}
}

// quotePOSIX single-quotes a value for sh and fish, closing the quote
// around any quote of its own.
func quotePOSIX(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// quotePowerShell single-quotes a value, doubling any quote of its own.
func quotePowerShell(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// guessShell reads $SHELL, since that is what the person is typing into.
// Windows has no useful $SHELL, so PowerShell is the assumption there.
func guessShell() string {
	if sh := filepath.Base(os.Getenv("SHELL")); sh != "" && sh != "." {
		switch sh {
		case "fish":
			return shellFish
		case "pwsh", "powershell":
			return shellPowerShell
		default:
			return shellSh
		}
	}

	if runtime.GOOS == "windows" {
		return shellPowerShell
	}

	return shellSh
}
