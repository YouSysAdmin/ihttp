# Getting started

## Install

Every release ships archives for Linux, macOS and Windows on amd64 and
arm64, a container image and a Homebrew cask:

```sh
brew install --cask yousysadmin/apps/ihttp
```

Or build it:

```sh
task build     # the console into web/dist, then bin/ihttp
bin/ihttp serve
```

## Run it

```sh
ihttp serve
```

The proxy listens on `:6080` and the console on
<http://127.0.0.1:6081>. The `info` line on startup says both.

Open the console. **Nothing is logged until a project is open** - the
proxy forwards regardless, but a project is what a log belongs to. Go to **Workspace > Projects**, create one, and open
it. Creating and opening
are two steps on purpose: creating a project while another is open must
not silently switch what is being recorded.

## Trust the CA

HTTPS needs the certificate authority trusted, or a client will refuse
the certificate ihttp mints for each host.

```sh
ihttp cert install            # the system trust store, asks for a password
ihttp cert install --firefox  # Firefox keeps its own
ihttp cert install --java     # so does a JVM
```

`ihttp cert path` says where the certificate is. `ihttp cert uninstall`
undoes it.

Two other ways to the same file, for a client you cannot run a command
on:

- **Settings > Download ca.pem** in the console.
- `http://ihttp.proxy/` from a browser already using the proxy. The
  proxy answers that host itself with a landing page and the
  certificate, so it works with no network at all.

A CA that signs anything your machine will believe is worth treating
carefully. `ihttp cert uninstall` when you are done for the day is a
reasonable habit, and the key never leaves `~/.ihttp`.

## See the first request

The quickest honest check, with no browser or trust store involved:

```sh
curl -x http://localhost:6080 http://example.com/
```

That request should appear in **Proxy > Request log** within a second.
If it does, the proxy works and everything after this is about pointing
real clients at it - see [Routing traffic in](clients.md).

For HTTPS, once the CA is trusted:

```sh
curl -x http://localhost:6080 https://example.com/
```

If that fails with a certificate error, the trust store step did not
take. [Troubleshooting](troubleshooting.md) covers the usual reasons.

## A browser, already set up

```sh
ihttp serve --chrome     # or --firefox
```

launches a Chromium-family or Firefox browser with a throwaway profile,
already pointed at the proxy, with the certificate handled and the
browser's own chatter switched off - so the log holds what you did
rather than what the browser did. The profile is deleted when the
browser closes.

`ihttp browser chrome` does the same against an instance that is already
running.

## Where things live

`~/.ihttp` by default, changed with `--data-dir`:

|                        |                                                                               |
|------------------------|-------------------------------------------------------------------------------|
| `ihttp.db`             | one bbolt file: projects, logs, sender history, automation runs, gRPC schemas |
| `ca.pem`, `ca_key.pem` | the certificate authority and its key                                         |

One process per database. A second `ihttp serve` on the same data
directory is refused with "is another ihttp running?" rather than
waiting on the lock.

## Next

- [Routing traffic in](clients.md) - the part that actually takes work
- [Filter language](filter-language.md) - the query that makes the log useful
- [Request log](request-log.md) - reading what you captured
