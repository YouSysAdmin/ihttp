# iHTTP documentation

An HTTP, WebSocket and gRPC toolkit for development and security
research: a machine-in-the-middle proxy with a searchable request log,
interception, rewrite rules, a request sender, request automation and
per-project scope, behind a web console.

Two listeners, on purpose. The **proxy** on `:6080` is what your clients
point at. The **console** on `127.0.0.1:6081` is what you look at. They
are separate so a browser under test cannot reach the console by
accident and the console is never in the traffic you are reading.

## Start here

|                                       |                                                                            |
|---------------------------------------|----------------------------------------------------------------------------|
| [Getting started](getting-started.md) | Install, run, trust the CA, see the first request                          |
| [Routing traffic in](clients.md)      | Browsers, shells, runtimes, containers - getting a client to use the proxy |

**Workspace > Setup** in the console is the same guide with this
instance's own address and certificate path filled in, and a probe that
checks the result rather than leaving you to hope.

## Reading traffic

|                                       |                                                         |
|---------------------------------------|---------------------------------------------------------|
| [Request log](request-log.md)         | The log, reading an exchange, timing, marks, views, HAR |
| [Filter language](filter-language.md) | The query every list takes, field by field              |

## Changing traffic

|                           |                                                                    |
|---------------------------|--------------------------------------------------------------------|
| [Intercept](intercept.md) | Hold a request or a response and edit it before it goes on         |
| [Rules](rules.md)         | Mock, block, rewrite, throttle, delay - by pattern, without asking |
| [Scope](scope.md)         | What this project is about, and what the log may refuse            |

## Tools

|                                                  |                                        |
|--------------------------------------------------|----------------------------------------|
| [Sender, Automation, Decoder, Compare](tools.md) | Compose and replay, fuzz, decode, diff |

## Workspace

|                         |                                                    |
|-------------------------|----------------------------------------------------|
| [Projects](projects.md) | Projects, retention, export and import             |
| [Proxies](proxies.md)   | The ways out ihttp itself uses, chosen per project |
| [MCP](mcp.md)           | Serving the log to an AI agent                     |

## Reference

|                                       |                                                           |
|---------------------------------------|-----------------------------------------------------------|
| [Command line](cli.md)                | Every command, every flag, every environment variable     |
| [Troubleshooting](troubleshooting.md) | Nothing is logged, HTTPS fails, a client will not connect |

## Keyboard

`?` shows the keys the page you are on offers, read from what the page
actually binds rather than a table someone has to keep up to date. On
the request log: `j` and `k` move, `f` focuses the filter, `h` folds
the host counts, `Delete` removes the selected entry. On the intercept
queue: `j` and `k` move, `Ctrl/Cmd+Enter` forwards,
`Ctrl/Cmd+Backspace` drops.

A bare key does nothing while you are typing in a field or an editor.
A key with `Ctrl/Cmd` works there too, so the intercept editor forwards
from where the cursor is. Nothing fires while a dialog is open.

## How the pieces fit

Everything belongs to a **project**. Open one and the proxy logs into
it, applies its rules, holds what its intercept filters say to hold, and
goes out the way its chosen proxy says. With no project open the proxy
still forwards - it just writes nothing down.

One **filter query language** runs over all of it: the log, the saved
view, the sender history, what the intercept holds, what the log
ignores, and which requests a rule matches. Learn it once and it works
everywhere. That is [filter-language.md](filter-language.md), and it is
the page worth reading first.

## Development

[ROADMAP.md](ROADMAP.md) is the working plan: what is built, what is
next, and the reasoning behind decisions that were not obvious. It is a
developer document rather than a user one.
