# Filter language

One query language, used everywhere a list is narrowed:

- the request log and the saved view
- the sender history
- what the intercept holds, on each side
- what the request log **ignores** before writing it
- which requests a **rule** matches
- `log_search` over [MCP](mcp.md)

A field name means the same thing in all of them. Learn it once.

## Shape

Terms joined by `AND`, `OR` and `NOT`, with parentheses to group. Two
terms side by side are ANDed - `req.method = POST res.statusCode >= 400`
is the same as putting `AND` between them.

A bare word with no operator matches it as a substring anywhere in the
exchange, ignoring case: the method, the URL, both bodies, both sets of
headers, the note and the tags. `login` finds every exchange with
"login" anywhere in it.

A value with spaces or an `=` goes in double quotes. There are no escape
sequences inside quotes - the closing quote is the next one.

## Operators

|                   |                                                                    |
|-------------------|--------------------------------------------------------------------|
| `=` `!=`          | equality. Methods, hosts, schemes, protocols and types ignore case |
| `>` `<` `>=` `<=` | ordering, numeric when both sides are numbers, otherwise text      |
| `=~` `!~`         | regular expression, Go syntax, compiled when the query is parsed   |
| `contains`        | substring, ignoring case                                           |
| `in (a, b, c)`    | any of. Commas are optional - `in (a b)` is the same               |
| `exists`          | the key is present                                                 |

Numbers take units: `ms` `s` `m` for time, `kb` `mb` `gb` for size.

```
res.size > 1mb
res.duration > 2s
res.ttfb > 500ms
```

An **unknown field is refused** rather than quietly matching nothing.
That applies when you type a query and when you save one - a stored
filter with a typo is rejected where it was typed, not on the hundredth
request that would have used it.

## Request fields

|                                     |                                                                 |
|-------------------------------------|-----------------------------------------------------------------|
| `req.id`                            | the exchange id                                                 |
| `req.method`                        | GET, POST, ...                                                  |
| `req.url`                           | the whole URL                                                   |
| `req.scheme`                        | http or https                                                   |
| `req.host`                          | the host, without the port                                      |
| `req.port`                          | the port                                                        |
| `req.path`                          | the path, without the query                                     |
| `req.query`                         | the raw query string                                            |
| `req.ext`                           | the path's extension, without the dot: `png`, `css`             |
| `req.proto`                         | what the **client** spoke: HTTP/1.1, HTTP/2.0                   |
| `req.body`                          | the request body as text                                        |
| `req.size`                          | the request body in bytes                                       |
| `req.timestamp`                     | when it started, RFC3339                                        |
| `req.headers`                       | every header as `Name: value`, matched line by line             |
| `req.header.<name>`                 | one header, name ignoring case                                  |
| `req.cookie.<name>`                 | one cookie of the `Cookie` header                               |
| `req.query.<name>`                  | one query parameter                                             |
| `req.form.<name>`                   | one field of a urlencoded body                                  |
| `req.tag`                           | a tag on the entry. `req.tags` is the same key                  |
| `req.note`                          | the note on the entry                                           |
| `req.color`                         | the colour mark: red, orange, yellow, green, blue, purple, gray |
| `req.websocket`                     | true when the request asked to upgrade                          |
| `req.grpcService`, `req.grpcMethod` | the gRPC service and method                                     |

## Response fields

On an exchange with no response yet these answer nothing rather than
failing, so one query runs over a log where some rows are still waiting.

|                      |                                                                  |
|----------------------|------------------------------------------------------------------|
| `res.statusCode`     | 200, 404, ...                                                    |
| `res.statusReason`   | OK, Not Found, ...                                               |
| `res.proto`          | what the **upstream** spoke                                      |
| `res.body`           | the response body as text                                        |
| `res.size`           | the response body in bytes                                       |
| `res.duration`       | the whole exchange in milliseconds                               |
| `res.type`           | grpc json html xml js css text image font audio video pdf binary |
| `res.mime`           | the media type without parameters                                |
| `res.headers`        | every header as `Name: value`                                    |
| `res.header.<name>`  | one response header                                              |
| `res.trailer.<name>` | one trailer                                                      |
| `res.cookie.<name>`  | one `Set-Cookie` value                                           |
| `res.streamed`       | true when the body went to the client uncaptured                 |
| `res.grpcStatus`     | the grpc-status of a gRPC call, 0 for OK                         |

## Timing fields

An exchange that was **not measured** answers nothing for these rather
than zero - so `res.tls > 200ms` never matches a request that did no
handshake, and a mocked response is never mistaken for an instant one.

|                  |                                                       |
|------------------|-------------------------------------------------------|
| `res.ttfb`       | time to the first response byte, in ms                |
| `res.blocked`    | waiting for a free connection                         |
| `res.dns`        | name resolution. 0 on a reused connection             |
| `res.connect`    | TCP connect. 0 on a reused connection                 |
| `res.tls`        | TLS handshake with the upstream                       |
| `res.send`       | writing the request                                   |
| `res.wait`       | the server thinking: request written to first byte    |
| `res.receive`    | reading the body back. 0 for a streamed body          |
| `res.reused`     | true when the connection was already open             |
| `res.serverIP`   | the address the upstream was reached at               |
| `res.tlsVersion` | `TLS 1.3` - from the handshake, so absent when reused |
| `res.alpn`       | `h2` - from the handshake too, so absent when reused  |

## WebSocket fields

Empty for an exchange that never upgraded.

|                  |                                      |
|------------------|--------------------------------------|
| `ws.messages`    | messages relayed over the connection |
| `ws.subprotocol` | the subprotocol the server chose     |
| `ws.closeCode`   | 1000, 1001, ... once it closed       |

## GraphQL fields

Read from the body, since GraphQL over HTTP is one POST to one path.
Empty for a request that is not GraphQL.

|                 |                                          |
|-----------------|------------------------------------------|
| `req.gqlType`   | `query`, `mutation` or `subscription`    |
| `req.gqlName`   | the operation name, empty when anonymous |
| `res.gqlErrors` | how many errors the result carried       |

`res.gqlErrors` answers nothing at all when the response is not a
GraphQL result, so `res.gqlErrors = 0` means "a GraphQL call that
worked" rather than "anything without errors".

A failed GraphQL call is usually a 200, which is what makes
`res.gqlErrors > 0` worth having: no status code will find it.

## Tunnel fields

Set only on a connection relayed without being decrypted, because its
host is on the project's
[do not decrypt](scope.md#do-not-decrypt) list. There is no message to
match on, so these three and the request line are all there is.

|                   |                                                  |
|-------------------|--------------------------------------------------|
| `req.tunnel`      | true when the connection was relayed undecrypted |
| `tunnel.bytesOut` | bytes client to upstream                         |
| `tunnel.bytesIn`  | bytes upstream to client                         |

## Worked examples

```
res.statusCode >= 500                        what failed
req.host = api.example.com AND req.method = POST
res.ttfb > 1s AND res.receive < 50ms         the server was slow, not the network
res.tls > 200ms                              a costly handshake
req.header.authorization exists              authenticated requests
req.body contains password                   a word in a request body
res.body contains "full migration"           a phrase in a response
req.method in (POST, PUT, PATCH)             the writes
NOT req.ext in (png, css, woff2)             drop static assets
req.url =~ "/api/v[0-9]+"                    a regular expression
res.type = grpc AND res.grpcStatus != 0      gRPC calls that failed
req.websocket = true AND ws.messages > 100   busy connections
NOT req.tag = imported                       what this proxy saw itself
res.size > 1mb AND res.duration > 2s         big and slow
req.tag = todo AND req.color = red           what you marked
```

## Where a query may not use everything

Two places decide on the **request alone**, because they run before a
response exists. A `res.*` or `ws.*` key there is refused when you save
it, naming the offending field:

- the request log's **ignore filter**
- a **rule**'s filter

Everywhere else the whole vocabulary applies.

## The one thing to remember

`res.duration` is the whole exchange. `res.ttfb` is everything before
the body started arriving. The difference between them is the transfer,
and the difference between `res.ttfb` and `res.wait` is your network
rather than their server. That pair answers "is it them or is it me?"
faster than anything else in the tool:

```
res.ttfb > 1s AND res.receive < 50ms
```
