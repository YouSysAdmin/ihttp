# Command line

```
ihttp serve      run the proxy and the console
ihttp cert       manage the CA certificate in the trust stores
ihttp browser    launch a browser pointed at a running proxy
ihttp env        print the proxy environment for a shell to evaluate
ihttp mcp        serve the request log to an AI agent over MCP
ihttp update     update to the latest release
ihttp version    print the binary version
```

**Every flag is also an environment variable**: `--proxy-addr` is
`IHTTP_PROXY_ADDR`, `--data-dir` is `IHTTP_DATA_DIR`,
`--upstream-proxy` is `IHTTP_UPSTREAM_PROXY`. A flag on the command line
always wins over the variable.

That is how a credential stays out of a process listing - put it in the
variable, not the flag.

## Global flags

Every command takes these, not only `serve`.

| Flag           | Default |                                                                 |
|----------------|---------|-----------------------------------------------------------------|
| `--log-level`  | `info`  | `trace`, `debug`, `info`, `warn` or `error`                     |
| `--log-format` | `text`  | `text` or `json`, on stderr, coloured when stderr is a terminal |
| `--log-file`   | none    | also append JSON lines to this file, at the same level          |

`info` is the lifecycle: listeners up, project reopened, browser
launched, shutdown. `debug` adds one line per proxied exchange, tunnel
and WebSocket, every intercept hold and release, every matched rule,
every send from the tools, and the console's own API calls.

Bodies are never logged, at any level.

## ihttp serve

| Flag                    | Default          |                                                                                                               |
|-------------------------|------------------|---------------------------------------------------------------------------------------------------------------|
| `--proxy-addr`          | `:6080`          | where the proxy listens                                                                                       |
| `--addr`                | `127.0.0.1:6081` | where the console and API listen                                                                              |
| `--data-dir`            | `~/.ihttp`       | database and CA key pair                                                                                      |
| `--db`                  | in `--data-dir`  | database file, when it lives elsewhere                                                                        |
| `--ca-cert`, `--ca-key` | in `--data-dir`  | the CA pair, when it lives elsewhere                                                                          |
| `--max-body-mb`         | `16`             | largest body kept for the log and intercept                                                                   |
| `--body-blob-mb`        | `1`              | bodies this size or larger go to files beside the database, `0` keeps them in it                              |
| `--insecure`            | off              | accept any upstream certificate, in the proxy and the tools                                                   |
| `--h2`                  | on               | speak HTTP/2 to clients inside CONNECT tunnels                                                                |
| `--ws-extensions`       | off              | pass `Sec-WebSocket-Extensions` through, so compression is negotiated and frames are recorded as opaque bytes |
| `--upstream-proxy`      | none             | the proxy ihttp goes out through: `http`, `https`, `socks5`                                                   |
| `--no-upstream-proxy`   | none             | host globs reached directly. localhost always is                                                              |
| `--client-cert`         | none             | `<host glob>=<cert file>[,<key file>]`, repeatable - see [Proxies](proxies.md#client-certificates-mtls)       |
| `--browser`             | none             | `chrome` or `firefox`, launched with a throwaway profile                                                      |
| `--chrome`, `--firefox` | off              | the same, shorter                                                                                             |
| `--shutdown-timeout`    | `10`             | seconds to wait for in-flight requests on exit                                                                |
| `--no-update-check`     | off              | do not ask GitHub on start whether a newer release exists                                                     |

Both `--ca-cert` and `--ca-key` are needed to bring your own CA. One
without the other is refused rather than silently minting a new pair
beside it.

## ihttp cert

```
ihttp cert install      trust it on this machine
ihttp cert uninstall    stop trusting it
ihttp cert path         print where it is
```

`install` and `uninstall` take:

| Flag                      |                                                |
|---------------------------|------------------------------------------------|
| `--firefox`               | also the Firefox trust store, which is its own |
| `--java`                  | also the Java trust store                      |
| `--skip-system`           | leave the system trust store alone             |
| `--ca-cert`, `--data-dir` | where the certificate is                       |

## ihttp browser

```
ihttp browser chrome|firefox
```

| Flag                      | Default                 |                                     |
|---------------------------|-------------------------|-------------------------------------|
| `--proxy-url`             | `http://localhost:6080` | the running proxy                   |
| `--open`                  | `http://127.0.0.1:6081` | page to open, typically the console |
| `--data-dir`, `--ca-cert` |                         | where the CA lives                  |

Chromium-family means Chrome, Chromium, Edge or Brave - whichever is
found first.

## ihttp env

| Flag                      | Default                 |                                                             |
|---------------------------|-------------------------|-------------------------------------------------------------|
| `--shell`                 | from `$SHELL`           | `sh`, `fish`, `powershell`, `cmd` or `env`                  |
| `--proxy-url`             | `http://localhost:6080` | the running proxy                                           |
| `--addr`                  | `127.0.0.1:6081`        | the console, which is left unproxied                        |
| `--unset`                 | off                     | print the lines that undo it instead                        |
| `--replace-ca-bundle`     | off                     | also set the variables that REPLACE a runtime's trust store |
| `--data-dir`, `--ca-cert` |                         | where the CA lives                                          |

`--shell env` prints bare `KEY=value` lines, for a `.env` file or
`docker run --env-file`.

See [Routing traffic in](clients.md#a-shell-and-anything-started-from-it)
for what it sets and why some things are left alone.

## ihttp update

```
ihttp update           install the latest release
ihttp update --check   only say whether a newer one exists
```

Downloads the latest release from GitHub and replaces this binary. The
archive is verified against the release's `checksums.sha256` before
anything is written, and the new binary is moved into place only once it
is on disk beside the old one - a download that fails, or a checksum
that does not match, leaves the working binary alone.

A development build - a plain `go build`, which reports `devel` - has no
version to compare against, so `--check` says so and a plain `update`
installs the latest release.

`serve` asks the same question on start and logs one line when a newer
release exists; it never installs anything. `--no-update-check`, or
`IHTTP_NO_UPDATE_CHECK=true`, turns that off.

Installed through a package manager - Homebrew, a `.deb` - update through
that instead: this writes over the file in place and the manager will not
know.

## ihttp mcp

| Flag            | Default          |                                            |
|-----------------|------------------|--------------------------------------------|
| `--addr`        | `127.0.0.1:6081` | the console of the ihttp to serve          |
| `--allow-write` | off              | also offer the tools that change something |
| `--no-redact`   | off              | show credentials to the agent as captured  |

See [MCP](mcp.md).

## The API

The console is a web page over an HTTP API on the same address, and the
API is the whole product - the page has no privilege the API lacks. So
scripting against it is a first-class way to use the tool:

```sh
curl -s localhost:6081/api/info
curl -sG localhost:6081/api/request-logs --data-urlencode 'search=res.statusCode >= 500'
curl -s localhost:6081/api/request-logs/export.har -o capture.har
```

`GET /api/events` is a server-sent event stream of everything that
happens - a new request, a response filled in, an intercept hold, an
automation result. The console reads nothing by polling.

There is no authentication. The console listener binds to loopback by
default and whoever reaches it operates the tool - so think before
changing `--addr` to something reachable.
