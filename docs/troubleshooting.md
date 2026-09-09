# Troubleshooting

Work from the outside in. Most problems are one of three things, and
they need different fixes.

1. **The request never reached ihttp.** The log is empty.
2. **It reached ihttp and the client refused the certificate.** A TLS
   error at the client, a tunnel in the log that produced nothing.
3. **It reached ihttp and was not written down.** Something is set to
   drop it.

## Nothing is in the log

**Is a project open?** With none open the proxy forwards and writes
nothing. The sidebar and **Workspace > Projects** say. This is the most
common answer.

**Is recording paused?** The switch on the log page. A paused log is
marked in the sidebar for exactly this reason.

**Does the proxy work at all?**

```sh
curl -x http://localhost:8080 'http://example.com/?probe=me'
```

If that appears in the log, the proxy is fine and the problem is your
client's routing - see [Routing traffic in](clients.md). If it does not,
check the startup line for the address the proxy actually bound.

**Is something dropping it?** Open **Filter options** on the log page.
The dot on that button is on when anything is set. Look for:

- an **ignore filter** that matches your request
- **Do not log requests outside the scope**, with a scope that does not
  cover it
- **Only entries in scope**, which hides rather than drops
- a **colour** filter left set
- a **view** you are still on. The picker says which

**Is the filter box narrowing it?** An unmatched query and an empty log
look the same. Clear it.

## A client will not connect over HTTPS

**Is the CA trusted?** `ihttp cert install`. Firefox and a JVM keep their
own stores - `--firefox`, `--java`.

**Was it trusted after the client started?** Restart the client. Many
cache TLS sessions and trust decisions.

**Does the client verify against its own bundle?** Python does. So does
Node, so does git. `ihttp env --replace-ca-bundle` points them at
ihttp's certificate, at the cost of discarding their other anchors -
which is why it is not the default.

**Is the app pinning the certificate?** An app that checks the server
certificate against a hard-coded fingerprint will refuse ihttp's, and it
is right to. Put that host on the project's
[do not decrypt](scope.md#do-not-decrypt) list, on the Scope page: the
connection is relayed as it is, the app sees the real certificate and
works, and the log still records that it happened. A quick test for
whether this is your problem: another client on the same host works,
that one app does not.

**Are you testing against something with a bad certificate?**
`--insecure` makes the proxy accept any upstream certificate. It applies
to the proxy and the tools, and it is the flag for a research target
rather than a default.

## Every request to one host is a 502

**Does that upstream want a client certificate?** A service with mutual
TLS refuses the handshake when none is presented, and the proxy reports
that to the client as a 502. Start with
`--client-cert '<host>=<cert file>[,<key file>]'` - see
[Client certificates](proxies.md#client-certificates-mtls). **Settings >
Client certificates** shows what was loaded and whether it has expired.

**Is the host reachable at all from here?** A 502 is the proxy saying it
could not reach the upstream, in the words the upstream or the network
gave it. The log entry carries the reason.

## The traffic is there but the body is not

**"The body was streamed to the client and not captured."** An event
stream, ndjson or an unbounded media response reaches the client as it
arrives and is not buffered - buffering a feed to inspect it would
change the thing being inspected. The headers, the timing and the fact
of it are all logged.

**"(truncated)"** The body was larger than `--max-body-mb`, 16 by
default. The prefix is stored and the rest went to the client
untouched. Raise the flag if you need the whole thing.

**A binary body shows as a hex dump.** By design, and the **Download**
button gives you the real bytes - the JSON copy the console holds has
been coerced to text and is not them.

## Intercept is not holding anything

- Is the matching switch on? **Hold requests** and **Hold responses**
  are separate.
- Does the filter match? An empty filter holds everything that is
  switched on, which is the way to check the switch itself works.
- A body over `--max-body-mb` is **not held** - it passes through.
- A **streamed** response is never held.

## A rule is not applying

- Is it **on**? The checkbox in the table.
- Do the method, the URL pattern and the filter **all** match? Each
  empty one is no condition, so an over-specified rule matches nothing.
  The Matches column shows all three.
- On a **streamed** response, `Set response status` and
  `Replace in response body` are skipped, with a debug line saying so.
  Header rules and throttle still apply.
- A **truncated** body is not replaced.

Run with `--log-level debug` and every matched rule prints one line.

## A saved filter was refused

An unknown field is refused where it was typed. `req.nothing = 1` is a
well-formed query and a nonexistent field, and it is better to hear
about it now than on the hundredth request.

The ignore filter and a rule's filter are decided on the request alone,
so `res.*` and `ws.*` are refused there - the message names the field.

## "is another ihttp running?"

One process per database. Either one already is, or a previous one did
not exit cleanly. Find it, or point `--data-dir` somewhere else.

## Requests fail with 502 through the proxy

The message says which:

- **"request dropped by ihttp"** - you dropped it in the intercept
  queue, or a rule blocked it.
- **"ihttp could not reach the upstream: ..."** - the proxy could not
  get there. If a [proxy](proxies.md) is configured, the **Test** button
  on that page is the fastest way to find out whether it is the way out
  rather than the target.
- **"blocked by ihttp rule ..."** - a block rule, naming itself.

## The timing bar says "not measured"

A response a **rule** answered - a mock, a local file, a CORS preflight -
never reached the network, so there are no phases to show. An entry
logged by an older build carries no breakdown either. Zero would have
been a lie in both cases.

## An imported HAR looks wrong

Ids are not preserved on import: the rows are new rows, so a row from a
file can never overwrite one already there. Everything else - the
timing, the bodies, the headers - comes through.

`NOT req.tag = imported` separates what the proxy saw itself from what
you read in.

## Still stuck

`--log-level debug` prints one line per decision on the request path:
the exchange, the tunnel, each WebSocket, every intercept hold and
release, every matched rule, every reqlog bypass or ignore, and the
console's own API calls. It is the fastest way to find out which of the
three cases at the top you are actually in.
