# Request log

**Proxy > Request log** is every exchange the open project saw, live.
Two panes: the list on the left, the selected exchange on the right. The
selection lives in the URL, so a link to one entry opens at it.

Press **?** anywhere for the keys the page you are on offers.

## The list

Newest first, a hundred at a time, with **Load older** for the next
page. Each row carries the colour mark, the method, the host, up to
three tags, the status, the path, and a meta line with the clock, the
duration, the response size and either `ws N` for a WebSocket or
`stream` for a streamed body.

Above it: the **Log / Saved** switch, the filter box, a **view picker**
and **Filter options**.

## Reading an exchange

**Request** and **Response** tabs, and **Messages** for a WebSocket. Each
side shows Body, Headers and - when there are any - Trailers.

A body is shown for what it is:

|                 |                                                                                                                    |
|-----------------|--------------------------------------------------------------------------------------------------------------------|
| JSON            | pretty-printed, with a Raw toggle so a signed body can be read as sent                                             |
| HTML            | a Render / Source toggle. Rendered inside a sandboxed frame with no scripts                                        |
| images, PDFs    | inline                                                                                                             |
| gRPC            | frame by frame, decoded from the wire format, or with field names and JSON when a schema is loaded                 |
| protobuf        | the same field-by-field view for a bare `application/x-protobuf` body, and for a binary WebSocket frame on request |
| a form          | `x-www-form-urlencoded` and `multipart/form-data` as their fields, with a Source toggle                            |
| GraphQL         | the document and its variables apart; the result with its errors above the data                                    |
| anything binary | a hex dump of the first 64 KiB, with a download for the rest                                                       |

A protobuf body that is **not** gRPC - `application/x-protobuf`,
`application/protobuf`, anything ending in `+protobuf` - gets the same
field-by-field view: it is the same wire format, only without the
framing, so it reads as one message rather than a stream of frames.
There is no service, no method and so no schema to attach: a `.proto`
is looked up by the gRPC method, and a plain HTTP body has none.

A **binary WebSocket frame** can be read the same way. It carries no
content type, so nothing decides for you - the payload opens as a hex
dump with a **Protobuf** button beside it. Bytes that are not protobuf
say so rather than showing an empty message.

A **form** body is split into its fields, percent-decoded one field at a
time so a bad escape loses only its own value. A multipart part that
carried a file is listed by the name it was uploaded under, what it said
it was and how long it is - the bytes themselves come from Download,
since the copy in the JSON has been coerced to text.

Every form shows the media type and the size, and offers **Download** on
the raw endpoint - because the JSON copy of a body has been coerced to
text and is not the bytes.

### GraphQL

A GraphQL exchange is recognised by its **body**, not by its path:
`/graphql` is a convention, plenty of services answer GraphQL elsewhere,
and plenty of things at `/graphql` are not GraphQL. What identifies one
is a JSON body carrying a `query` string, or a body of
`application/graphql`.

The row and the facts line then name the operation - `query Viewer`,
`mutation SignIn` - because a log where every row reads `POST /graphql`
is no log at all. A result carrying errors says how many, in red.

The request is shown as **the document and its variables apart**, which
is how they were written and how they are read; every client sends the
document escaped into one JSON string, which is unreadable. The result
puts its **errors above the data**: a failed GraphQL call is usually a
200, and errors below a screenful of data are errors nobody sees. Each
error expands to its full JSON, `extensions` and all.

**Source** shows the body as sent, for when the problem is in the
envelope rather than in the operation.

In the [filter language](filter-language.md):

```
req.gqlType = mutation
req.gqlName = SignIn
res.gqlErrors > 0
```

`res.gqlErrors` answers **nothing** rather than 0 when the response is
not a GraphQL result, so `res.gqlErrors = 0` finds GraphQL calls that
worked rather than every request in the log.

Two limits worth knowing. A batched request - several operations in one
body - is named by its first, with a count beside it. And the scan is
bounded to 256 KiB of body, so a request with megabytes of variables
reads as ordinary JSON: a query document is never that big, and a page
of a hundred rows must not turn into a hundred megabyte-sized scans.

### Cookies

Under the headers, not beside them: a cookie is a header, and the
tables only save you reading a line of semicolons.

**Cookies sent** splits the request's `Cookie` header into one row per
cookie. A pair with no `=` is shown as a name with no value rather than
dropped, because a malformed cookie is worth seeing. Values are shown as
they were sent - a cookie value is opaque, so nothing is decoded.

**Cookies set** does the same for each `Set-Cookie`, with its attributes
under it: path, domain, expiry, `Secure`, `HttpOnly`, `SameSite` and
anything else the server sent, in the order it sent them.

The [filter language](filter-language.md) reads the same cookies:
`req.cookie.session exists`, `res.cookie.session contains abc`.

### Raw

The **Raw** tab shows the message in wire form - start line, headers, a
blank line, the body - with a Copy button. It is the fastest way to
eyeball a protocol-level problem and the shape to paste into a bug
report; what Copy puts on the clipboard uses CRLF, so it is also a
request `nc` will send.

It is a **reconstruction** and the tab says so. It is built from what
was stored, so it is not byte-for-byte what crossed the wire:

- an HTTP/2 exchange never had a text start line or header casing at
  all - the first line and the capitals are ours,
- a request reached the proxy in absolute form and is shown in origin
  form, with the `Host` line Go keeps outside the header map put back,
- chunked framing and content encodings are gone: the body shown is the
  decoded one,
- a binary body is left out, and a truncated or streamed one is not
  there to show.

### Timing

The response's duration is a button. Click it and the **timing
breakdown** opens: blocked, DNS, connect, TLS, send, wait and receive as
a proportional bar, with the upstream's address, the TLS version and the
negotiated protocol beside it.

It is honest about what it does not know:

- A **reused connection** did not resolve, connect or handshake at all.
  Those phases are left out and the line says "reused connection" rather
  than drawing three instant bars.
- A **streamed body** was never read here, so receive is unknown rather
  than zero.
- A response a **rule answered locally** never reached the network and is
  marked "not measured".

`res.ttfb`, `res.wait` and the rest are filterable - see
[filter-language.md](filter-language.md).

### Copy as

**Copy as curl** is in the Actions menu, and **Copy as...** opens the
same request spelled six ways: curl, HTTPie, `fetch()`, Python (requests), Go (a whole program) and PowerShell. The
dialog shows the
command before you copy it and remembers which language you were last
in.

Each spelling sends the same bytes. Content-Length is left to the
client in all of them, and Accept-Encoding is dropped where the client
negotiates it itself, so a snippet never asks for an encoding it will
not decode. A binary body is rebuilt from base64 rather than pasted
into a shell.

The awkward parts are handled where they bite: quotes inside header
values, a newline inside a body, `Host` set the way `net/http` reads it
rather than as a header it ignores, `--ignore-stdin` for HTTPie, and
Content-Type and User-Agent as their own PowerShell parameters, which
is what PowerShell accepts.

### Actions

The Actions menu on an entry: copy the URL, copy as `curl`, copy as
`fetch()`, save it, send it to the [Sender](tools.md#sender) or the
[Automation](tools.md#automation), pin it as A for a
[comparison](tools.md#compare), or delete it. `Delete` and `Backspace`
do the same, unless you are typing or a dialog is open.

## Marks

Every entry can carry **tags**, a **note** and a **colour**, edited
inline, and all three are searchable: `req.tag = todo`,
`req.note contains repro`, `req.color = red`. Tags are offered from the
ones this project has already used.

**Save** moves an entry into the Saved view, which **Clear log** does not
reach and retention never evicts. That is what saving is for.

## Top of the log

The **Log** menu has **Slowest exchanges** and **Largest responses**:
the current filter ranked, twenty at most, in a dialog. Clicking a row
opens that entry.

A report rather than a sort, on purpose. Sorting the list would need an
index the log does not have, and sorting the page in front of you would
lie the moment there were two pages. So the ranking is over everything
the filter matches, computed when you ask for it.

An exchange still waiting for its response is not ranked - it has
nothing to rank yet - and a muted host still counts, because a ranking
is a question about the log rather than about the list.

## Hosts

**Hosts** beside Filter options folds out a count of the log by host,
busiest first: how many entries, and how many of those were answered
with a 5xx. A 4xx is not counted as an error - it is usually the answer
the request was after.

Clicking a host writes `req.host = <host>` into the filter. Clicking the
same host again takes it out, so the click undoes itself.

**mute** on a row hides that host from the list while it goes on being
captured. This is the opposite of the
[ignore filter](#filter-options), and the difference is worth being
clear about:

|                   |                                                             |
|-------------------|-------------------------------------------------------------|
| **Muted host**    | written, kept, exported, found by a filter - just not shown |
| **Ignore filter** | never written, so there is nothing to get back              |

Mute the noisy host you are not working on today. Ignore what you never
want to see again.

A muted host's entries come back with **Show muted hosts** in
[Filter options](#filter-options), and an export or a Clear with a
filter always acts on what the filter says, muted or not - muting is
about what you are looking at, not about what you have.

## Views

The filter worth keeping gets a name. **Save this filter as...** in the
view picker keeps the query, the scope switch and which of the two lists
is open. Views belong to the project, so they survive a restart and
travel with a settings-only [export](projects.md#export-and-import).

The picker shows which view you are on by comparing the filter you
actually have - so typing over a view's query leaves the view behind and
the label goes back to **All traffic**, rather than lying about it.

## Filter options

One dialog for every other reason an exchange is not in front of you, in
two halves that are kept apart because their reach differs.

**What this view shows** - yours alone, remembered for this project:

- **Only entries in scope** - see [scope.md](scope.md)
- **Colour mark** - shows only entries carrying it

**What the proxy logs** - a project setting, so it holds for everyone
who opens the project:

- **Do not log requests outside the scope**
- **Ignore filter** - what matches is never written. Decided on the
  request alone, so `res.*` keys are refused. Nothing it drops can be
  got back by clearing a filter.
- **Keep at most** - the entry cap. Past it the oldest go as new ones
  arrive, and saved entries are never dropped. Empty is no cap. The
  count is checked every so often rather than per request, so the log
  sits a little over the number between passes.

The dot on the button is on when any of those is set, including the
project half - a log that has gone quiet should never be a mystery.

## Pause recording

The switch in the page header stops the log while the proxy keeps
forwarding, so a client under test never loses its connection while you
read what you already have. A paused log is marked in the sidebar,
because a paused log and a quiet one look identical.

## Streams, WebSockets, HTTP/2, gRPC

**Streams** - an event stream, ndjson and the like - reach the client as
they arrive and are marked `streamed` in the log. Their bodies are not
captured, by design: buffering a feed to inspect it would change the
thing being inspected.

`application/grpc` is deliberately buffered instead, so the log can read
`grpc-status` from the trailers.

**WebSocket** connections are relayed frame by frame and every message is
recorded with its direction, opcode and payload, readable in the entry's **Messages** tab while the connection is still
open.
`Sec-WebSocket-Extensions` is stripped from the handshake so no
compression is negotiated and messages stay readable.

**HTTP/2** reaches the client inside the CONNECT tunnel when it offers
it. In the log `req.proto` is what the client spoke and `res.proto` what
the upstream spoke - they are often different, and that is the point of
showing both.

**gRPC** bodies are cut into frames and decoded from the wire format,
field by field. Upload a `.proto` or a descriptor set through the **Schemas** button and they come back with names and
as JSON. Unary
calls are complete. Streaming RPCs wait for the body to end, and
client-streaming or bidi calls hang, because the request body is
buffered to EOF before it is forwarded.

## Undecrypted tunnels

A host on the project's [do not decrypt](scope.md#do-not-decrypt) list
is logged as a `CONNECT` row marked `tunnel`, with the bytes each way
and how long the connection stood. There is nothing to open: the reader
says so rather than showing an empty body, and the actions that need a
message - copy as curl, send to the sender or the automation, compare -
are not offered.

The row appears when the connection opens, so a tunnel that is still
open reads as pending and fills in its byte counts when it closes.

## HAR

**Export HAR** in the Log menu writes what the current filter selects, so
you can hand over exactly the exchanges that matter. The timing
breakdown goes with it, which is what makes the file readable in any tool
that draws a waterfall.

**Import HAR** reads a file back, whether ihttp wrote it or Chrome
DevTools, Charles or mitmproxy did. Then the filter language, Compare,
the Sender and the Automation all work on somebody else's capture.

Imported entries are tagged `imported`, so `NOT req.tag = imported` is
what the proxy itself saw. Each is keyed by the time it happened rather
than the time it was read, so an imported log sorts alongside a live one.
Ids are not preserved - the rows are new rows, and a row from a file must
never overwrite one already there.

## Body limits

`--max-body-mb` (16 by default) is the largest body kept for the log and
for intercept. A larger one is marked truncated: the prefix is stored
and the rest streams through to the client untouched. Nothing is ever
withheld from the client to make the log tidier.
