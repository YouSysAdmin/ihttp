# Intercept

**Proxy > Intercept** holds an exchange at the proxy and waits for you.
A held request has stopped a real client, so the queue is not somewhere
to leave things.

Use intercept when you do not yet know what you want to send. When you
do know, a [rule](rules.md) does it every time without asking.

## Switches and filters

Two switches: **Hold requests** and **Hold responses**. Either can be on
alone.

Two filters in the same [query language](filter-language.md) decide what
is held, so you are not stopping everything:

```
req.host = api.example.com AND req.method = POST
```

holds the writes to one API and lets the rest through. An empty filter
holds everything that is switched on.

The request filter is decided on the request, and so may only use
`req.*` fields. The response filter sees both halves, so `res.*` works
there.

The [scope](scope.md) does not come into it: what is held is these two
filters and nothing else.

## Answering

The queue is on the left, oldest first, with the count in the sidebar.
Select one and it becomes an editable copy.

For a **held request** you can change the method, the URL, the headers
and the body, then **Forward** or **Drop**. There is also a `Then`
choice for the response of this one exchange: follow the project
setting, hold it too, or let it through - useful when you want to see
what your edited request produced without holding every response.

For a **held response**: the status code, the reason phrase, the headers
and the body.

**Drop** answers the client with a 502.

From the keyboard: `j` and `k` move through the queue, **Ctrl/Cmd+Enter** forwards and **Ctrl/Cmd+Backspace** drops.
Those two
carry a modifier on purpose - they send or destroy somebody's request,
and a bare letter is how the wrong one goes. The modifier is also why
they work from inside the URL field, a header or the body editor, where
`j` and `k` do not. `?` lists the keys.

A **binary body is shown and passed through untouched**. The JSON copy
the console holds has been coerced to text, and editing that would
corrupt the bytes - so it is displayed and forwarded as it was.
Forwarding recomputes `Content-Length` and drops `Content-Encoding` on a
response, so an edited body is framed correctly.

## What is never held

- A body larger than `--max-body-mb`. It passes through with a debug
  line rather than being held with only its prefix available to edit.
- A **streamed** response. The frames are already going to the client.
- Anything, when no project is open.

## Getting out of it

- Turning a switch off releases everything waiting, unmodified.
- Switching or closing the project does the same.
- Stopping the proxy releases the queue.
- A client that gives up removes its own item - nothing is sent upstream
  later.

Nothing is persisted: a held exchange is a blocked goroutine, not a row.
