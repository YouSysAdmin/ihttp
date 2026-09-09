# Scope

**Proxy > Scope** is what the project is about. A request is in scope
when **any** rule matches, and a rule matches when **every** expression
it carries matches.

Each rule has up to four regular expressions, and an empty one is not
tested:

|                                      |                                             |
|--------------------------------------|---------------------------------------------|
| **URL**                              | over the whole URL                          |
| **Header name** and **Header value** | both must match on the **same** header line |
| **Body**                             | over the request body                       |

An empty scope matches nothing, which is why the switches that depend on
it do nothing until you have added a rule.

## What scope is for

Two things read it, and they are worth telling apart.

**Narrowing what you look at.** The **Only entries in scope** switch in
the log's [Filter options](request-log.md#filter-options), and the same
option on the sender history. Nothing is deleted - it is a view.

**Refusing to write.** **Do not log requests outside the scope**, in the
same dialog, is a project setting: what is out of scope is never written
to the log at all. That is the one with consequences, since what was
never written cannot be recovered by clearing a filter.

The **[intercept](intercept.md) does not read the scope.** What it holds
is decided by its own two filters, which are the same query language and
more expressive than four regular expressions.

## Do not decrypt

The second card on the page is the other half of the same question -
what this project is allowed to see - answered by refusing rather than
by narrowing.

A host on the **Do not decrypt** list is relayed as it is: the bytes go
between the client and the real server, and nobody in the middle looks
at them. The client sees the **real** server's certificate, which is the
point.

Two reasons to use it:

- **A client that pins its server's certificate** will refuse ihttp's,
  and it is right to. Exclude that host and the rest of the project
  stays decrypted.
- **Traffic that has no business being in a log** - a banking session,
  someone's mail - while the project is open for something else.

A pattern is a host, and `*` spans a whole name, so `*.example.com`
covers `a.b.example.com` as well as `a.example.com`. The port is not
part of it. A pattern that is not a host pattern is refused when you
save it, naming itself.

What you give up on those hosts is everything that needs the plaintext:
no bodies, no headers, no [intercept](intercept.md), no
[rules](rules.md), no searching inside them.

What you keep is that it happened. The connection is logged as a
`CONNECT` row - the host, when it opened, how long it stood, how many
bytes went each way - and it is there **while the connection is open**,
not only after it closes. A tunnel held open for an hour is visible for
that hour. Passthrough is not the same as invisible:

```
req.tunnel = true AND tunnel.bytesIn > 1mb
```

Changing the list affects **new** connections. One already open is not
touched.

## Scope or the ignore filter?

They overlap, and the difference is which way round you think:

- **Scope** says what you care about. Everything else is other people's
  traffic.
- The **ignore filter** says what you do not care about. Everything else
  is yours.

The ignore filter is also more expressive, since it is the full
[query language](filter-language.md) rather than four regular
expressions:

```
req.ext in (png, css, woff2, svg) OR req.host contains telemetry
```

Use scope when a project is about one system. Use the ignore filter to
drop noise from a project that is about everything.
