# Proxies

**Workspace > Proxies** is the ways out ihttp itself uses - for a lab or
a corporate network where nothing reaches the internet directly.

Not to be confused with the proxy ihttp *is*. This page is about where
its own outbound traffic goes after it has captured, decrypted and
applied rules to yours.

The same way out is used by the proxy, the [Sender](tools.md#sender) and
the [Automation](tools.md#automation), so a replay leaves the machine the
way the original did.

## The default

`--upstream-proxy http://proxy:3128` is the instance's default, for a
machine with one way out and nothing to choose between. `https://` and
`socks5://` work too.

`--no-upstream-proxy '*.internal,api.local'` is its **No proxy** list -
the `NO_PROXY` analogue, as host globs.

With no default configured the `HTTP_PROXY` family of environment
variables is honoured as it always was.

## The named list

Add proxies with a **name**, a URL and a **No proxy** list of their own.
A project then picks one on the [Projects](projects.md) page, so **switching project switches the way out** - no
restart, no rebuilt
transport.

A project can also pick:

- **Instance default** - whatever `--upstream-proxy` says
- **Direct** - out on our own, whatever the default is

`localhost`, `127.0.0.1` and `::1` are always reached directly, whatever
a No proxy list says: a proxy elsewhere cannot route to a host only this
machine can see. Note this is the opposite of the decision on the client
side, where loopback **is** proxied so a local app under test can be
debugged. Different direction, different question.

## Credentials

Credentials go in the URL as `http://user:pass@proxy:3128`.

A proxy in the list keeps them as typed. This is a local tool and the
database already holds every captured request, so a password beside them
changes little - but they are only ever **shown** in that proxy's own
editor. Everywhere else - the list, the project picker, a log line - you
see the **name** and a host with the credentials stripped out entirely,
not masked. A row says "signed in" when a credential is stored, without
showing it.

The command-line default has no name, so its password is replaced by a
mark wherever it appears: the startup line, `GET /api/info`, the page.
Put that one in `IHTTP_UPSTREAM_PROXY` rather than on a command line,
where a process listing would show it.

## Test

Each proxy, and the default, has a **Test** button: one short-lived
request through it, reporting what answered and how long it took.

It says so plainly when the target was on that proxy's No proxy list,
since a bypassed target proves nothing about the proxy - a green result
you cannot rely on is worse than a red one.

## Client certificates (mTLS)

Some services ask the **client** for a certificate. Without one ihttp
cannot be in the path at all: the handshake fails before a request is
ever written, and every exchange with that host is a 502.

Instance-level, configured at start and repeatable:

```sh
ihttp serve \
  --client-cert 'api.example.com=/keys/client.pem,/keys/client-key.pem' \
  --client-cert '*.internal.example=/keys/internal.pem'
```

One file rather than two means that file holds the key as well, which is
how most exports arrive. The host is a glob, the same syntax as the
[do not decrypt](scope.md#do-not-decrypt) list and the No proxy list,
and the port is not part of it.

The certificate is presented **only** to a host a pattern names, and
only when the upstream asks for one. Everything else is offered nothing.

It applies to the proxy, the [Sender](tools.md) and the
[Automation](tools.md) alike, so a replay reaches the service the way the
proxied original did.

The files stay on disk, named by path, and are read once at start: a
wrong path or a key that does not match its certificate is a startup
error naming the file, rather than a failed handshake in the middle of
your work. **Settings > Client certificates** shows what was loaded -
the hosts, the subject, the expiry and the path - and nothing else,
because ihttp is not a key store and a tool that captures traffic is the
last place a private key should be kept.

## PAC

Not supported. It needs a JavaScript interpreter, which the tool does
not have. Point `--upstream-proxy` at whatever the PAC file would have
chosen for the hosts you care about.
