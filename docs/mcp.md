# MCP

`ihttp mcp` serves a running instance to an AI agent over the Model
Context Protocol, on stdin and stdout.

```sh
claude mcp add ihttp -- ihttp mcp
```

Then ask the agent to look. What makes this worth having is the
[query language](filter-language.md): an agent asks

```
res.statusCode >= 500 AND req.host = api.example.com AND res.ttfb > 1s
```

instead of paging through a log.

## How it fits

A **separate process** talking to the console API, not a mode of
`serve`. So it works against an instance in a container or on another
host, and an agent's client starting and stopping it - which they do
freely - cannot take the proxy down.

It needs an ihttp already running. Without one, every tool says so in
those words rather than reporting a connection error.

```sh
ihttp mcp --addr 127.0.0.1:8081   # a different console
```

## Tools

Read-only by default:

|                  |                                                                              |
|------------------|------------------------------------------------------------------------------|
| `info`           | version, proxy address, data directory, which project is open, which way out |
| `projects_list`  | the projects, and which one is open                                          |
| `log_search`     | the log, by filter query. One line per exchange                              |
| `log_get`        | one exchange in full: headers, timing breakdown, bodies                      |
| `log_body`       | one side's body, up to 256 KiB, when `log_get` cut it short                  |
| `log_curl`       | the exchange as a `curl` command and a `fetch()` call                        |
| `rules_list`     | what the proxy is doing to traffic on your behalf                            |
| `intercept_list` | what is held and waiting                                                     |
| `filter_help`    | the whole filter vocabulary                                                  |

`filter_help` is a tool rather than documentation for a reason. An agent
that has the vocabulary writes a query. One that does not guesses, and a
guessed field is refused - so handing it over up front is cheaper than
the round trip.

With `--allow-write`:

|                     |                                                                             |
|---------------------|-----------------------------------------------------------------------------|
| `project_open`      | open a project, which changes what the proxy logs from that moment          |
| `sender_send`       | re-send a captured exchange, optionally edited. The request really goes out |
| `intercept_forward` | let a held exchange through                                                 |
| `intercept_drop`    | refuse one. The client gets a 502                                           |

## The privacy boundary

This is **the one place captured traffic can leave the machine**, since
the agent may be a model on somebody else's hardware. So the defaults
are the careful ones and loosening them is an explicit flag.

**Masked by default:** credential headers - `Authorization`,
`Proxy-Authorization`, `Cookie`, `Set-Cookie`, `X-Api-Key`,
`X-Auth-Token` and their kind - and query values whose name looks like a
secret: anything containing `token`, `secret`, `password`, `apikey`,
`auth`, `signature`, `session`, `credential`. The parameter's **name**
survives, since which one it was is the useful half.

**Not masked, and you should know it:**

- **Bodies are not scanned.** A token under an unusual name in a JSON
  body goes out as it is. Masking headers stops the everyday accident.
  It is not a guarantee.
- **`log_curl` is never redacted**, because a command with a masked
  token would not run. Its own output says so.

`--no-redact` turns masking off. Every tool result carries a line saying
which mode it is in, so a masked value is never read as the real one.

**Nothing changes without `--allow-write`.** The write tools are not
merely refused - they are absent from `tools/list`, so an agent given a
read-only server does not see them and does not try.

## Notes

Written against the protocol directly rather than with an SDK, because
MCP over stdio is newline-delimited JSON-RPC 2.0 and the surface used
here is four methods - `initialize`, `tools/list`, `tools/call`, `ping`.
Versions `2025-11-25`, `2025-06-18`, `2025-03-26` and `2024-11-05` are
spoken. A client asking for one of those gets it back, anything else
gets the newest.

A tool that fails reports it as an error **result**, not a protocol
error, so the agent reads what went wrong and tries something else
instead of losing the connection.
