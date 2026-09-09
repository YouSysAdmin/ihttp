package filter

// Help is the query language as prose, for a reader that cannot see the
// console: the MCP tool hands it to an agent, so a query is written from
// the vocabulary rather than guessed at - and a guessed field is a 400.
//
// It lives in this package, beside the switch statements it describes,
// because that is the only place it can be kept honest. TestHelpNamesOnlyRealFields
// walks every field named here and refuses one the subject does not
// answer, so the text cannot drift into advertising something that does
// not exist. The other direction - a field added to the subject and not
// to this text, or to FilterHelp.vue - is still the author's to
// remember.
const Help = `The ihttp filter query language.

A query is terms joined by AND, OR and NOT, with parentheses to group.
Two terms side by side are ANDed. A bare word with no operator matches
it as a substring anywhere in the exchange, ignoring case.

Operators:
  =  !=            equality; methods, hosts, schemes and types ignore case
  >  <  >=  <=     ordering; numeric when both sides are numbers
  =~  !~           regular expression (Go syntax)
  contains         substring, ignoring case
  in (a, b, c)     any of
  exists           the key is present

Numbers take units: ms s m for time, kb mb gb for size.
  res.size > 1mb        res.duration > 2s        res.ttfb > 500ms

Request fields:
  req.id            the exchange id, which log_get takes
  req.method        GET, POST, ...
  req.url           the whole URL
  req.scheme        http or https
  req.host          the host, without the port
  req.port          the port
  req.path          the path, without the query
  req.query         the raw query string
  req.ext           the path's extension, without the dot: png, css
  req.proto         what the CLIENT spoke: HTTP/1.1, HTTP/2.0
  req.body          the request body as text
  req.size          the request body in bytes
  req.timestamp     when it started, RFC3339
  req.headers       every header as "Name: value", one per line
  req.header.<name> one header, name ignoring case
  req.cookie.<name> one cookie of the Cookie header
  req.query.<name>  one query parameter
  req.form.<name>   one field of a urlencoded body
  req.tag           a tag on the entry; req.tags is the same key
  req.note          the note on the entry
  req.color         the colour mark: red, orange, yellow, green, blue, purple, gray
  req.websocket     true when the request asked to upgrade
  req.grpcService   the gRPC service
  req.grpcMethod    the gRPC method
  req.gqlType       query, mutation or subscription of a GraphQL request
  req.gqlName       the GraphQL operation name

Response fields. On an exchange with no response yet these answer
nothing, so one query runs over a log where some rows are still waiting:
  res.statusCode    200, 404, ...
  res.statusReason  OK, Not Found, ...
  res.proto         what the UPSTREAM spoke
  res.body          the response body as text
  res.size          the response body in bytes
  res.duration      the whole exchange in ms
  res.type          grpc json html xml js css text image font audio video pdf binary
  res.mime          the media type without parameters
  res.headers       every header as "Name: value"
  res.header.<name>   one response header
  res.trailer.<name>  one trailer
  res.cookie.<name>   one Set-Cookie value
  res.streamed      true when the body went to the client uncaptured
  res.grpcStatus    the grpc-status of a gRPC call, 0 for OK
  res.gqlErrors     errors in a GraphQL result; nothing when not one

Timing fields. An exchange that was not measured answers nothing for
these rather than zero, so res.tls > 200ms never matches a request that
did no handshake:
  res.ttfb          time to the first response byte, in ms
  res.blocked       waiting for a free connection
  res.dns           name resolution; 0 on a reused connection
  res.connect       TCP connect; 0 on a reused connection
  res.tls           TLS handshake with the upstream
  res.send          writing the request
  res.wait          the server thinking: request written to first byte
  res.receive       reading the body back; 0 for a streamed body
  res.reused        true when the connection was already open
  res.serverIP      the address the upstream was reached at
  res.tlsVersion    TLS 1.3 - from the handshake, so absent when reused
  res.alpn          h2 - from the handshake too

Tunnel fields, for a connection the project said not to decrypt. Empty
for everything else, so a query for them finds only the connections
nobody in the middle saw inside:
  req.tunnel        true when the connection was relayed undecrypted
  tunnel.bytesOut   client to upstream
  tunnel.bytesIn    upstream to client

WebSocket fields, empty for an exchange that never upgraded:
  ws.messages       messages relayed
  ws.subprotocol    the subprotocol the server chose
  ws.closeCode      1000, 1001, ... once closed

Worked examples:
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
  res.gqlErrors > 0                            GraphQL calls that failed, 200 and all
  req.gqlType = mutation                       GraphQL writes
  req.websocket = true AND ws.messages > 100   busy connections
  NOT req.tag = imported                       what this proxy saw itself
  req.tunnel = true                            relayed without decrypting
  res.size > 1mb AND res.duration > 2s         big and slow

A value with spaces or an = goes in double quotes. There are no escapes
inside quotes: the closing quote is the next one.`
