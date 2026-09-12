# iHTTP

The HTTP/WebSocket/gRPC toolkit for development and security research: a
machine-in-the-middle proxy with a searchable request log, request and
response interception, rewrite rules, a request sender, request automation
and per-project scope, behind a web console.

![ihttp ui](docs/ihttp-ui.png)

**[Documentation](docs/)** - getting started, routing traffic in, the
filter language, and a page per feature. This file is the short version.

## Install

Every GitHub release ships archives for Linux, macOS and Windows on
amd64 and arm64, a container image and a Homebrew cask:

```sh
brew install --cask yousysadmin/apps/ihttp
```

```sh
# In the future :)
docker run --rm -p 6080:6080 -p 6081:6081 -v ihttp:/data ghcr.io/yousysadmin/ihttp
```

or build from source:

```sh
task build          # console into web/dist, then bin/ihttp
bin/ihttp serve
```

A binary installed from an archive updates itself:

```sh
ihttp update           # install the latest release
ihttp update --check   # only say whether a newer one exists
```

The download is checked against the release checksums before the binary
is replaced. `serve` asks the same question on start and logs one line
when a newer release exists, which `--no-update-check` (or
`IHTTP_NO_UPDATE_CHECK=true`) turns off. Installed through Homebrew,
update through Homebrew instead - see [Command line](docs/cli.md#ihttp-update).

## Getting started

The proxy listens on `:6080` and the console on `http://127.0.0.1:6081`.
Point a browser or a tool at the proxy, open the console, create a project
and open it - nothing is logged until one is.

HTTPS sites need the CA certificate trusted:

```sh
ihttp cert install            # system trust store, asks for a password
ihttp cert install --firefox  # Firefox keeps its own
```

or download `ca.pem` from the console's Settings page, or from
`http://ihttp.proxy/` in a browser that uses the proxy.

`ihttp serve --browser chrome` (or `--chrome`) launches a Chromium-family
browser with its own profile, already pointed at the proxy and ignoring
certificate errors. `ihttp serve --browser firefox` (or `--firefox`) does
the same with a throwaway Firefox profile: the proxy is written into its
preferences and the CA is imported into the profile with `certutil` when
that tool is installed (`nss` on Homebrew and most distributions).
Without certutil the profile trusts the system store instead, so run
`ihttp cert install` first. Both profiles are quiet: telemetry, updates,
remote settings, safe browsing, push and the new-tab feeds are off, so
the log holds what you did and not what the browser did. `ihttp browser
chrome|firefox` launches one against an instance that is already
running.

A tool that ignores the system proxy is pointed at ihttp by its
environment instead:

```sh
eval "$(ihttp env)"                  # sh, bash, zsh
ihttp env --shell fish | source      # fish
ihttp env --shell powershell | iex   # PowerShell
ihttp env --shell env > .env         # for docker --env-file
ihttp env --unset                    # undo it
```

The command only prints, and the shell applies it. It sets `HTTP_PROXY`
and `HTTPS_PROXY` in both cases, leaves the console's own address out of
`NO_PROXY` targets while keeping the rest of loopback proxied, and hints
the CA additively: `NODE_EXTRA_CA_CERTS` and `JAVA_TOOL_OPTIONS` proxy
properties, appended to what is already there and not stacked on a
second run. The variables that REPLACE a runtime's whole trust store -
`SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`,
`GIT_SSL_CAINFO` - are left alone unless `--replace-ca-bundle` asks for
them, so a corporate anchor is never thrown away by accident. Routing is
all it prepares: Python verifies against its own bundle, Node's `fetch`
reads the variables only from Node 24, and a JVM needs `ihttp cert
install --java`. `ihttp env --help` says so.

## Flags

Every flag is also an environment variable: `--proxy-addr` is
`IHTTP_PROXY_ADDR`, `--data-dir` is `IHTTP_DATA_DIR`.

| Flag                            | Default          |                                                                                                 |
|---------------------------------|------------------|-------------------------------------------------------------------------------------------------|
| `--proxy-addr`                  | `:6080`          | where the proxy listens                                                                         |
| `--addr`                        | `127.0.0.1:6081` | where the console and API listen                                                                |
| `--data-dir`                    | `~/.ihttp`       | database and CA key pair                                                                        |
| `--db`, `--ca-cert`, `--ca-key` | in `--data-dir`  | database file and CA pair, when they live elsewhere                                             |
| `--max-body-mb`                 | `16`             | largest body kept for the log and intercept                                                     |
| `--body-blob-mb`                | `1`              | bodies this size or larger go to files beside the database, `0` keeps them in it                |
| `--insecure`                    | off              | accept any upstream certificate, in the proxy and the tools                                     |
| `--h2`                          | on               | speak HTTP/2 to clients inside CONNECT tunnels                                                  |
| `--ws-extensions`               | off              | pass `Sec-WebSocket-Extensions` through - compression is then negotiated and frames are opaque  |
| `--browser`                     | none             | `chrome` or `firefox`, launched with a throwaway profile                                        |
| `--chrome`, `--firefox`         | off              | same as `--browser chrome` or `firefox`                                                         |
| `--upstream-proxy`              | none             | proxy ihttp itself goes out through: `http`, `https`, `socks5`                                  |
| `--no-upstream-proxy`           | none             | host globs reached directly, `*.internal` - localhost always is                                 |
| `--client-cert`                 | none             | present a client certificate to an upstream that asks: `<host glob>=<cert>[,<key>]`, repeatable |
| `--shutdown-timeout`            | `10`             | seconds to wait for in-flight requests on exit                                                  |
| `--no-update-check`             | off              | do not ask GitHub on start whether a newer release exists                                       |

Logging flags work on every command, not only `serve`:

| Flag           | Default |                                                                |
|----------------|---------|----------------------------------------------------------------|
| `--log-level`  | `info`  | `trace`, `debug`, `info`, `warn` or `error`                    |
| `--log-format` | `text`  | `text` or `json`, on stderr, colored when stderr is a terminal |
| `--log-file`   | none    | also append JSON lines to this file, at the same level         |

`info` is the lifecycle: listeners up, project reopened, browser launched,
shutdown. `debug` adds one line per proxied exchange, tunnel and WebSocket,
every intercept hold and release, every matched rule, every send from the
tools, and the console's own API calls.

## Upstream proxy

In a lab or a corporate network where nothing reaches the internet
directly, ihttp goes out through a proxy of its own. `http://`,
`https://` and `socks5://` all work, and the same way out is used by the
proxy, the Sender and the Automation, so a replayed request leaves the
machine the way the proxied original did.

**Workspace > Proxies** is the instance's list of them, each with a
name. **Projects** is where a project picks one, so switching project
switches the way out - no restart, no rebuilt transport. A project can
also pick **Direct**, which means out on our own whatever the default
is, or leave it at **Instance default**.

A project holds only a *reference* to a proxy, never a URL, so exporting
a project carries none of your credentials to whoever you shared it
with. A reference that means nothing on another machine - or names one
you have since deleted - falls back to that machine's default and says
so once in the log.

`--upstream-proxy http://proxy:3128` is that default, for a machine with
one way out and nothing to choose between. `--no-upstream-proxy
'*.internal,api.local'` is its **No proxy** list - the `NO_PROXY`
analogue, as host globs - and each proxy in the list has one of its own.
`localhost`, `127.0.0.1` and `::1` are always in it, since a proxy
elsewhere cannot route to a host only this machine can see. With no
default configured the `HTTP_PROXY` family of environment variables is
honoured as before.

Credentials go in the URL as `http://user:pass@proxy:3128`. A proxy in
the list keeps them as typed - the database already holds every captured
request, so a password beside them changes little - but they are only
ever *shown* in that proxy's editor. Every list, picker and log line
carries the **name** and a host with the credentials stripped out
entirely, and for the command-line default, which has no name, the
password is replaced by a mark. Put that one in `IHTTP_UPSTREAM_PROXY`
rather than on a command line, where a process listing would show it.

Each proxy has a **Test** button: one short-lived request through it,
reporting what answered and how long it took - and saying so plainly
when the target was on that proxy's No proxy list, since that proves
nothing about the proxy.

## Setup

Workspace > Setup answers "how do I get *this* client through the
proxy", with this instance's real proxy address and real certificate
path already in the snippet. Nine targets: curl, any shell, Node,
Python, Go, the JVM, Docker, a browser, and an API client such as
Postman.

Routing and trust are kept apart on the page, because they fail
differently: a client that ignores the proxy leaves the log empty, and
one that uses it and refuses the certificate gives a TLS error. Knowing
which you have is most of the work.

**Check it** prints a command with a fresh token and watches the log for
it. The target, `http://ihttp.probe/`, is a host the proxy answers
itself - so the check needs no network and reaches no real server, while
still going through the whole path and turning up in the log. It says
plainly what that does not prove: which process sent it, that HTTPS to a
real host will work, or that your upstream is reachable.

## Request log

Proxy > Request log is every exchange the open project saw, live. Select
an entry to read the request and the response with headers, trailers and
the body shown for what it is: JSON pretty-printed, HTML rendered in a
sandboxed frame on request, images and PDFs inline, anything else as a
hex dump. The filter box takes the query language below, the color menu
narrows by mark, and Only in scope keeps to the scope rules.

Every entry can carry **tags, a note and a color**, all searchable. The **Actions** menu copies the URL or the request
as `curl` or `fetch()`,
sends the entry to the Sender or the Automation, pins it as A for a
comparison, or deletes it (Delete and Backspace do the same). **Save**
moves an entry into the Saved view, where Clear log does not reach. The
Log and Saved switch sits beside the filter, and each view exports as **HAR** with the current filter applied.

**HAR import** reads a file back, whether ihttp wrote it or Chrome
DevTools, Charles or mitmproxy did, and then the filter language,
Compare, the Sender and the Automation all work on somebody else's
capture. Entries are added to the open project, tagged `imported`, so
`NOT req.tag = imported` is what the proxy itself saw. Each one is keyed
by the time it happened rather than the time it was read, so an imported
log sorts alongside a live one. Ids are not preserved: the rows are new
rows, and a row from a file must never overwrite one already there.

**Streams** such as event streams and ndjson reach the client as they
arrive and are marked streamed in the log. **WebSocket** connections are
relayed frame by frame and every message is recorded with its direction,
opcode and payload, readable in the entry's Messages tab while the
connection is still open. Clients get **HTTP/2** inside the CONNECT tunnel
when they offer it, and the log shows what the client spoke next to what
the upstream spoke. **gRPC** bodies are cut into frames and decoded from
the wire format, field by field, or with names and JSON when a `.proto`
or a descriptor set is uploaded through the Schemas button.

**Slowest exchanges** and **Largest responses** in the Log menu rank the
current filter, twenty at most, and clicking one opens it - a report
rather than a column sort, since sorting one page of a log would lie
across pages.

**Keyboard**: `?` lists what the current page binds. On the log, `j`/`k`
move, `f` focuses the filter, `h` shows the host counts and `Delete`
removes the entry. On the intercept queue, `Ctrl/Cmd+Enter` forwards
and `Ctrl/Cmd+Backspace` drops, from inside the editor too. A bare key
does nothing while you are typing in a field or an editor, and nothing
fires while a dialog is open.

**Copy as** spells any request six ways - curl, HTTPie, `fetch()`,
Python, Go and PowerShell - shown in a dialog before you copy it. Each
one sends the same bytes, with a binary body rebuilt from base64 rather
than pasted into a shell.

A bare **protobuf** body - `application/x-protobuf` and its kin - gets
the same field-by-field view as a gRPC frame, since it is the same wire
format without the framing. A binary **WebSocket frame** can be read as
one too, on request: it has no content type, so the payload opens as
hex with a Protobuf button beside it.

A **Raw** tab shows either half in wire form with a Copy button - CRLF,
so what you copy is a request `nc` will send. It is honest about being a
reconstruction: an h2 exchange had no text start line, a proxied request
arrived in absolute form, and chunked framing is gone.

**GraphQL** is recognised by its body rather than its path, so the row
says `query Viewer` or `mutation SignIn` instead of another
`POST /graphql`. The request is shown as its document and its variables
apart, and the result puts its errors above the data, since a failed
GraphQL call is usually a 200. `req.gqlType`, `req.gqlName` and `res.gqlErrors`
are filter fields.

A **form** body - urlencoded or multipart - is shown as its fields, with
a Source toggle and the file parts listed by name, type and size. **Cookies** are split out under the headers they came
from, a
`Set-Cookie`'s attributes with it.

**Hosts** folds out the log counted by host, busiest first, with the
5xx count beside each. Clicking one filters to it. **Mute** hides a host
from the list while it goes on being captured - the opposite of the
ignore filter, which never writes it down, and the reason both exist.

The response's duration opens the **timing breakdown** it is the sum of:
blocked, DNS, connect, TLS, send, wait and receive as a proportional
bar, with the upstream's address, TLS version and negotiated protocol
beside it. A reused connection did not resolve, connect or handshake at
all, so those phases are left out and the line says so rather than
drawing three instant bars, and a streamed body was never read, so its
receive is unknown rather than zero. A response a rule answered locally
never reached the network and is marked not measured. The HAR export
carries the same breakdown, so a file from ihttp reads in any tool that
draws a waterfall.

**Pause recording** stops the log while the proxy keeps forwarding, so a
client under test never loses its connection while what was already
captured is read. A paused log is marked in the sidebar, since a paused
log and a quiet one look alike.

**Views** beside the filter box are the filters worth keeping: save the
current query, the scope switch and which of the two lists is open under
a name, and pick it back later. They belong to the project, so they
survive a restart and travel with a settings-only export. The picker
shows which view you are on by what the filter actually is, so typing
over one leaves it behind rather than lying about it.

**Filter options** is one dialog for every other reason an exchange is
not in front of you, in two halves that are kept apart because their
reach differs: what this view shows - the scope switch and the color
mark, yours alone - and what the proxy logs - the ignore filter and the
out-of-scope switch, a project setting whose drops are never written. It
also holds **Keep at most**, the entry cap: past it the oldest entries
go as new ones arrive, and saved entries are never dropped. Empty is no
cap, which is what the log did before. The count is checked every so
often rather than per request, so the log sits a little over the number
between passes.

## Intercept

Proxy > Intercept holds requests, responses or both before they go on.
Two filters in the same query language decide what is held, so
`req.host = api.example.com AND req.method = POST` stops the writes to
one API and lets everything else through. A held request can be edited
in every part, including a binary body kept as it was, then forwarded or
dropped. A held response the same. The sidebar shows how many are
waiting.

## Scope

Proxy > Scope is what the project is about: URL, header and body
patterns, any of which puts a request in scope. The log and the sender
history can be narrowed to it, and the request log can refuse to write
what is out of it. The intercept does not read it - what it holds is
decided by its own two filters.

The same page carries the **do not decrypt** list: host patterns
relayed as they are, so the client sees the real server's certificate.
That is the answer to an app that pins its server's certificate, and to
traffic that has no business being in a log. Nothing inside those
connections is seen, so they cannot be intercepted, changed by a rule or
searched - but they are still logged, as a `CONNECT` row with the host,
how long it stood and how many bytes went each way, present from the
moment the tunnel opens rather than only when it closes. Passthrough is
not the same as invisible: `req.tunnel`, `tunnel.bytesOut` and
`tunnel.bytesIn` are filter fields like any other.

## Rules

Proxy > Rules is what the proxy does to matching traffic for you, so a
frontend can be worked on without the whole backend running:

- **Mock response** - answer with a status, headers and body, the backend is not called.
- **Serve local file** - answer with a file from disk, content type by extension.
- **Rewrite URL** - point a production host at localhost.
- **Set / remove request or response header**, **set response status**, **replace in response body** (regex, `$1`
  groups).
- **Block** - answer with a status instead of calling the backend, 502 by
  default, with a line saying which rule did it. For making a dependency
  fail on purpose.
- **Delay request** - a slow backend on demand. **Delay response** holds
  the answer back instead, which is what a slow link looks like.
- **Throttle response** - pace the answer at so many bytes a second. It
  wraps what the client receives, so unlike a delay it works on a
  streamed body: an event stream can be made to trickle.
- **Allow CORS** - answers preflights and adds permissive CORS headers.
- **Capture a value** - read a field of an exchange and remember it under
  a name, for `${name}` in a later rule's header, URL, mock body or body
  replacement. This is what carries a token from a sign-in into the
  requests that need it: every other action writes the text you typed,
  this one writes what the traffic said. `${uuid}`, `${timestamp}`,
  `${timestamp_ms}`, `${isotime}` and `${random}` need no capture at all.
  Captured values appear on the Rules page as they are read, and are
  never stored - they go when the project closes.

A rule matches when its method, its URL regex and its **filter** all
agree - each left empty is no condition. The filter is a query in the
same language as the log, so a rule can say what a URL pattern cannot:
`req.header.authorization exists`, `req.body contains password`,
`req.size > 1mb`. It is decided on the request alone, since a rule runs
before the response exists, so `res.*` keys are refused when the rule is
saved. Rules run in order, and mocked and blocked exchanges are logged
like real ones.

## Tools

### Sender

Tools > Sender is a request editor with a history. Write a request from
scratch or take one from the log through Send to sender, edit any part,
pick HTTP/1.1 or HTTP/2, send it and read the answer beside it. Every
send is kept, the history is searchable with the filter language, and a
request copies out in any of the **Copy as** spellings.

### Automation

Tools > Automation repeats one request over a set of values, for
enumeration, fuzzing and rate checks. A job is a request template with a
placeholder, `AUTO` by default, in the URL, the headers or the body,
and a payload that fills it:

- **List** - values one per line, typed in or read from a file on disk.
  With a separator each line is columns reached as `$1`, `$2` and so
  on, and `AUTO` is `$1`.
- **Numbers** - a range with a step.
- **Random** - strings of a length from a charset: alnum, alpha,
  digits, hex or printable.
- **Library** - a built-in set of awkward inputs a correct handler should
  survive: empty and long values, unicode, path and template shapes.

A run fires up to 50 requests at once and stops early on a **stop
condition**: a status code, a response header or a body match, any or
all of them. Results list status, size and time per value, open into the
full exchange, copy as `curl` and export as HAR. A run is bounded by
10000 iterations.

### Decoder

Tools > Decoder does base64, URL, HTML, HEX, JSON, JWT, gzip, SHA-1,
SHA-256, SHA-512 and Unix timestamps in the browser. Nothing leaves the
page.

### Compare

Pin any exchange as A from the log, the sender or an automation result,
then choose Compare with A on another. The two are shown side by side or
as a unified diff, status and headers first, then the bodies, with JSON
pretty-printed on both sides. Binary bodies compare by size and SHA-256.

## Projects

Everything above belongs to a project, and the proxy forwards without
logging when none is open. Workspace > Projects creates, opens, exports
and imports them. An export is one JSON file with the settings, the log,
the WebSocket messages, the sender history, the automation jobs with
their results and the gRPC schemas. Importing makes a new project and
never touches the open one.

Export offers **Settings only** beside Everything: the same file with
the settings and no traffic, for handing a team a set of rules, a scope
and the filters without handing over anybody's captured exchanges. It
imports through the same path. Filter text can itself be telling, so
read the file before sharing it.

## MCP

`ihttp mcp` serves a running instance to an AI agent over the Model
Context Protocol, on stdin and stdout:

```sh
claude mcp add ihttp -- ihttp mcp
```

What makes it worth having is the query language: an agent asks
`res.statusCode >= 500 AND req.host = api.example.com AND res.ttfb > 1s`
instead of paging through a log. The tools are `info`, `projects_list`,
`log_search`, `log_get`, `log_body`, `log_curl`, `rules_list`,
`intercept_list` and `filter_help` - which hands over the whole filter
vocabulary, so a query is written rather than guessed.

It is a separate process talking to the console API, so it works against
an instance in a container or on another host, and an agent's client
starting and stopping it cannot take the proxy down.

This is the one place captured traffic can leave the machine, since the
agent may be a model on somebody else's hardware:

- credential headers - `Authorization`, `Cookie`, `Set-Cookie`,
  `X-Api-Key` and their kind - and query values whose name looks like a
  secret are masked, unless `--no-redact`. **Bodies are not scanned**: a
  token under an unusual name in a body goes out as it is. `log_curl` is
  never redacted, because a command with a masked token would not run,
  and it says so in its own output.
- nothing changes without `--allow-write`, which adds opening a project,
  re-sending a request and answering a held exchange.

## Filter language

The same query narrows the log, the sender history, what the intercept
holds and what the log ignores:

```
login                                      any field contains "login", any case
req.host = api.example.com                 equals, or !=
req.body contains password                 a word in the request body, any case
res.body contains "password"               a phrase in the response body
req.method in (POST, PUT, PATCH)           one of several values
req.header.authorization exists            the header is present
req.cookie.session exists                  a cookie by name
req.form.user = admin                      a field of a urlencoded body
req.query.id > 100                         a query parameter, as a number
res.statusCode >= 400                      numbers compare as numbers
res.type in (json, html)                   json html xml js css text image font audio video pdf binary
res.size > 1mb AND res.duration > 2s       units: kb mb gb ms s m
res.ttfb > 1s AND res.receive < 50ms       the server was slow, not the network
req.url =~ "/api/v[0-9]+"                  regular expression, or !~
NOT req.ext in (png, css, woff2)           drop static assets
req.tag = todo AND req.color = red         marks on a log entry
req.websocket = true AND ws.messages > 10
res.type = grpc AND res.grpcStatus != 0
```

Fields: `req.id`, `req.method`, `req.url`, `req.scheme`, `req.host`,
`req.port`, `req.path`, `req.query`, `req.ext`, `req.proto`, `req.body`,
`req.size`, `req.timestamp`, `req.headers`, `req.header.<name>`,
`req.cookie.<name>`, `req.query.<name>`, `req.form.<name>`,
`req.websocket`, `req.grpcService`, `req.grpcMethod`, `req.gqlType`,
`req.gqlName`, `req.tunnel`, and on a log entry `req.tag`, `req.note`,
`req.color`.

On the response side `res.statusCode`, `res.statusReason`, `res.proto`,
`res.body`, `res.size`, `res.duration`, `res.type`, `res.mime`,
`res.streamed`, `res.grpcStatus`, `res.gqlErrors`, `res.headers`,
`res.header.<name>`, `res.trailer.<name>`, `res.cookie.<name>`, for a
WebSocket `ws.messages`, `ws.subprotocol`, `ws.closeCode`, and for a
passthrough tunnel `tunnel.bytesOut`, `tunnel.bytesIn`.

From the timing breakdown `res.ttfb`, `res.blocked`, `res.dns`,
`res.connect`, `res.tls`, `res.send`, `res.wait`, `res.receive`,
`res.reused`, `res.serverIP`, `res.tlsVersion`, `res.alpn`. An exchange
that was not measured answers nothing for these rather than zero, so
`res.tls > 200ms` never matches a request that did no handshake.

Adjacent terms are ANDed, parentheses group, a value with spaces or `=`
goes in quotes. Methods, hosts, schemes and types compare without regard
to case.

The request log's **ignore filter** (Request log > Filter options) drops
what matches before it is written, and may only test `req.*` fields.

Every stored filter - the ignore filter, the two intercept filters and a
view's query - is checked when it is saved, not when it runs: a field
the vocabulary does not have is refused there and then, since
`req.nothing = 1` is a well-formed query and would otherwise fail
quietly every time it was used.

## Development

```sh
task run        # proxy + API with a dev data dir
task web-dev    # vite on :5173 proxying /api to :6081
task check      # vet, lint, tests, gofmt, vue-tsc, prettier
```
