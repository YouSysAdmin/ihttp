# Routing traffic in

Proxy debugging almost never breaks at the inspector. It breaks here: a
client that ignores the proxy setting, or uses it and then refuses the
certificate. Those are two separate problems and it is worth knowing
which one you have.

- **Routing** decides whether the request reaches ihttp at all. If it
  does not, the log stays empty.
- **Trust** decides whether the client accepts the certificate ihttp
  mints for the host. If it does not, you get a TLS error and the log
  shows a tunnel that produced nothing.

Every recipe below is one or both of those.

**The console has this page too.** **Workspace > Setup** carries the same
recipes with *this instance's* real proxy address and real certificate
path already substituted, plus a probe that checks the result - so it is
the better place to work from, and this page is the one to read when you
have no console in front of you. They are two copies of the same
knowledge, and the page is the one that cannot get the values wrong.

## A browser, the easy way

```sh
ihttp serve --chrome
ihttp serve --firefox
ihttp browser chrome     # against an instance already running
```

A throwaway profile, pointed at the proxy, with trust handled and the
browser's own telemetry, updates, safe browsing, push and new-tab feeds
switched off. Measured rather than assumed: a fresh Firefox made 99
requests of its own through the proxy and a fresh Chrome 42, and these
profiles bring that to 0 and 3 - the three being Chrome's account
requests, which no flag reaches.

Two details in there are load bearing and worth knowing if you ever
build your own profile:

- Loopback **is** proxied, deliberately, so an app under test on
  `localhost:3000` can be debugged. Only the console's own address is
  excluded.
- Firefox needs `MOZ_REMOTE_SETTINGS_DEVTOOLS=1` in its environment or a
  release build ignores the settings-server override and phones home
  anyway.

## A shell, and anything started from it

```sh
eval "$(ihttp env)"                  # sh, bash, zsh
ihttp env --shell fish | source      # fish
ihttp env --shell powershell | iex   # PowerShell
ihttp env --shell env > .env         # a file for docker --env-file
ihttp env --unset                    # undo it in this shell
```

The command only prints. The shell is what applies it, so you can read
it first, and nothing outside that shell changes.

What it sets:

|                                          |                                                          |
|------------------------------------------|----------------------------------------------------------|
| `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` | and their lower-case twins                               |
| `NO_PROXY`                               | the console's address only                               |
| `IHTTP_ROOT_CA`                          | where the certificate is                                 |
| `NODE_EXTRA_CA_CERTS`                    | additive - Node keeps its own bundle and trusts this too |
| `NODE_USE_ENV_PROXY=1`                   | Node 24 and later honour the proxy variables with this   |
| `JAVA_TOOL_OPTIONS`                      | the proxy properties, appended to what was there         |

Both cases are set because clients disagree about which they read. curl,
for one, reads only the lower-case `http_proxy` for plain HTTP.

`JAVA_TOOL_OPTIONS` keeps whatever you had and does not stack: sourcing
twice does not add a second set of proxy properties.

**What it deliberately does not set.** `SSL_CERT_FILE`,
`REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE` and `GIT_SSL_CAINFO` *replace* a
runtime's whole trust store. Setting them would throw away the system
and corporate anchors, so they are left alone unless you pass
`--replace-ca-bundle`.

`NO_PROXY` carries the console's host **and port**, which is precise but
not universally read: measured against curl 8.22, a port in a `no_proxy`
entry is ignored, so curl sends its own console requests through the
proxy too. They work, they are just noise in the log. The alternative -
a bare host - would stop a local app under test from being proxied at
all, which is worse.

## Per runtime

Routing is all the environment prepares. Each runtime still decides for
itself what to trust.

### curl

```sh
curl -x http://localhost:6080 https://example.com/
curl -x http://localhost:6080 --cacert ~/.ihttp/ca.pem https://example.com/
```

The `--cacert` form needs no trust store at all, which makes it the best
first test of whether the proxy itself works.

### Python

`urllib` and `requests` follow the proxy variables, and verify against
their own bundle - so HTTPS needs the certificate too:

```sh
eval "$(ihttp env --replace-ca-bundle)"
python3 -c "import urllib.request; print(urllib.request.urlopen('https://example.com/').status)"
```

Without `--replace-ca-bundle` that fails with
`CERTIFICATE_VERIFY_FAILED` - the request went through the proxy and was
refused at the trust step. The tidier alternative is adding the
certificate to `certifi`'s bundle rather than replacing it.

### Node

Node core has never read the proxy variables on its own. Node 24 added
`NODE_USE_ENV_PROXY=1`, which `ihttp env` sets. On earlier versions
`fetch` ignores them entirely and needs an explicit agent:

```js
import {ProxyAgent, setGlobalDispatcher} from 'undici'

setGlobalDispatcher(new ProxyAgent('http://localhost:6080'))
```

`NODE_EXTRA_CA_CERTS`, which `ihttp env` also sets, handles trust
additively either way.

### Go

`http.DefaultTransport` reads `HTTP_PROXY` and `HTTPS_PROXY`, and Go
reads the port in `NO_PROXY` correctly. Trust comes from the system
store, so `ihttp cert install` is enough.

### Java

The JVM reads properties rather than the environment, which is what
`JAVA_TOOL_OPTIONS` is for, and it has its own trust store:

```sh
eval "$(ihttp env)"
ihttp cert install --java
```

An IDE may override the JVM properties with its own proxy setting. If
traffic from a run configuration does not appear, look there first.

### Docker

A container does not share the host's loopback, so `localhost:6080`
inside it is not the proxy:

```sh
ihttp env --shell env > .env
docker run --rm --env-file .env \
  --add-host host.docker.internal:host-gateway \
  -v ~/.ihttp/ca.pem:/usr/local/share/ca-certificates/ihttp.crt:ro \
  your-image
```

and rewrite the proxy host to `host.docker.internal`. On Linux
`--network host` is the other way round the same problem.

### A tool with its own proxy field

Postman, Insomnia and most API clients have a proxy setting of their own
that overrides everything else. Point it at `http://localhost:6080` and
turn off their certificate verification, or trust the CA in the system
store.

## What no setting can fix

Some clients cannot be routed, and it is better to know than to keep
trying:

- **Certificate pinning.** An app that checks the server certificate
  against a hard-coded fingerprint will refuse ihttp's, correctly. The
  answer is to exclude that host, not to fight it.
- **A separate network namespace.** An existing container, a VM, another
  machine - none of them inherit a shell's environment. They need their
  own proxy setting and their own trust.
- **An already-running process.** The environment is read at start.
  Restart it.

## Checking, rather than hoping

**Workspace > Setup** does this for you: it prints a command with a
fresh token in it and watches the log for that token over the event
stream.

By hand, the same thing:

```sh
curl -x http://localhost:6080 http://ihttp.probe/v/anything
```

`ihttp.probe` is a host the proxy answers itself, so this needs no
network and reaches no real server - and it still goes through the whole
path, so the exchange turns up in the log like any other. Filter for it
with `req.host = ihttp.probe`.

A project has to be open or nothing is logged and there is nothing to
find.

What a probe proves and does not prove is worth being clear about. It
proves that **that client** reached the proxy and that the log recorded
it. It does not prove which process sent it, that the same client will
manage HTTPS to a real host, or that your upstream is reachable. It
proves the routing, which is the half that is usually wrong.

If a request from your real client does not show up but the probe does,
the problem is that client's routing and not ihttp.
