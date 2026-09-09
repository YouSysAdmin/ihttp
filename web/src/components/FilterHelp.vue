<script setup lang="ts">
// The filter language, explained once and shown the same way in the
// three places it is typed: the log search, the intercept filters and
// the ignore filter. What differs is which fields a subject has, and
// that is the prop.
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    // What is being matched. A request has only req.* fields, a response
    // has both halves, the log has both plus req.id and req.timestamp.
    subject?: 'log' | 'request' | 'response'
  }>(),
  { subject: 'log' },
)

interface Field {
  name: string
  meaning: string
}

const REQUEST: Field[] = [
  { name: 'req.method', meaning: 'GET, POST, ...' },
  { name: 'req.url', meaning: 'the whole URL' },
  { name: 'req.scheme', meaning: 'http or https' },
  { name: 'req.host', meaning: 'host without the port' },
  { name: 'req.port', meaning: 'port, empty when default' },
  { name: 'req.path', meaning: 'path without the query' },
  { name: 'req.query', meaning: 'the raw query string' },
  { name: 'req.ext', meaning: 'file extension of the path: png, js' },
  { name: 'req.proto', meaning: 'HTTP/1.1, HTTP/2.0 - what the client spoke to the proxy' },
  { name: 'req.body', meaning: 'request body - a GET has none' },
  { name: 'req.size', meaning: 'request body in bytes' },
  { name: 'req.headers', meaning: 'every header as "Name: value", names canonical: X-Testing' },
  { name: 'req.websocket', meaning: 'true when the request asks to become a WebSocket' },
  { name: 'req.grpcService', meaning: 'package.Service of a gRPC call' },
  { name: 'req.grpcMethod', meaning: 'the method of a gRPC call' },
]

const REQUEST_LOG: Field[] = [
  { name: 'req.id', meaning: 'the entry id' },
  { name: 'req.timestamp', meaning: 'when it was logged, RFC 3339' },
  { name: 'req.tag', meaning: 'a tag on the entry, any of them: = in contains =~ exists' },
  { name: 'req.note', meaning: 'the note on the entry' },
  { name: 'req.color', meaning: 'red, orange, yellow, green, blue, purple, gray' },
]

const REQUEST_NAMED: Field[] = [
  { name: 'req.header.<name>', meaning: 'one header by name, any case' },
  { name: 'req.cookie.<name>', meaning: 'one cookie from the Cookie header' },
  { name: 'req.query.<name>', meaning: 'one query parameter' },
  { name: 'req.form.<name>', meaning: 'one field of a urlencoded body' },
]

const RESPONSE: Field[] = [
  { name: 'res.statusCode', meaning: '200, 404, ...' },
  { name: 'res.statusReason', meaning: 'OK, Not Found, ...' },
  { name: 'res.proto', meaning: 'HTTP/1.1, HTTP/2.0 - what the upstream spoke to the proxy' },
  { name: 'res.body', meaning: 'response body - the page, the JSON' },
  { name: 'res.size', meaning: 'response body in bytes' },
  { name: 'res.duration', meaning: 'time to the response, in ms' },
  {
    name: 'res.type',
    meaning: 'grpc, json, html, xml, js, css, text, image, font, audio, video, pdf, binary',
  },
  { name: 'res.mime', meaning: 'media type without parameters' },
  { name: 'res.headers', meaning: 'every header as "Name: value", names canonical: Set-Cookie' },
  { name: 'res.streamed', meaning: 'true when the body went to the client uncaptured' },
  { name: 'res.grpcStatus', meaning: 'the grpc-status of a gRPC call, 0 for OK' },
  { name: 'res.ttfb', meaning: 'time to the first response byte, in ms - the phases below it' },
  { name: 'res.blocked', meaning: 'wait for a free connection, in ms' },
  { name: 'res.dns', meaning: 'name resolution, in ms - 0 on a reused connection' },
  { name: 'res.connect', meaning: 'TCP connect, in ms - 0 on a reused connection' },
  { name: 'res.tls', meaning: 'TLS handshake with the upstream, in ms' },
  { name: 'res.send', meaning: 'writing the request, in ms' },
  { name: 'res.wait', meaning: 'the server thinking, in ms - request written to first byte' },
  { name: 'res.receive', meaning: 'reading the body back, in ms - 0 for a streamed body' },
  { name: 'res.reused', meaning: 'true when the connection was already open' },
  { name: 'res.serverIP', meaning: 'the address the upstream was reached at' },
  {
    name: 'res.tlsVersion',
    meaning: 'TLS 1.3 - known from the handshake, so absent on a reused connection',
  },
  { name: 'res.alpn', meaning: 'h2 - from the handshake too, so absent on a reused connection' },
]

const TUNNEL: Field[] = [
  {
    name: 'req.tunnel',
    meaning: 'true when the connection was relayed without being decrypted',
  },
  { name: 'tunnel.bytesOut', meaning: 'bytes client to upstream' },
  { name: 'tunnel.bytesIn', meaning: 'bytes upstream to client' },
]

const WEBSOCKET: Field[] = [
  { name: 'ws.messages', meaning: 'messages relayed over the connection' },
  { name: 'ws.subprotocol', meaning: 'the subprotocol the server chose' },
  { name: 'ws.closeCode', meaning: '1000, 1001, ... once the connection closed' },
]

const RESPONSE_NAMED: Field[] = [
  { name: 'res.header.<name>', meaning: 'one header by name, any case' },
  { name: 'res.trailer.<name>', meaning: 'one trailer by name, sent after the body' },
  { name: 'res.cookie.<name>', meaning: 'one Set-Cookie by cookie name' },
]

interface Group {
  title: string
  fields: Field[]
}

const groups = computed<Group[]>(() => {
  const request = props.subject === 'log' ? [...REQUEST, ...REQUEST_LOG] : REQUEST
  const out: Group[] = [
    { title: 'Request', fields: request },
    { title: 'Request, by name', fields: REQUEST_NAMED },
  ]

  if (props.subject !== 'request') {
    out.push(
      { title: 'Response', fields: RESPONSE },
      { title: 'Response, by name', fields: RESPONSE_NAMED },
    )
  }

  if (props.subject === 'log') {
    out.push({ title: 'WebSocket', fields: WEBSOCKET }, { title: 'Tunnel', fields: TUNNEL })
  }

  return out
})

const OPERATORS: [string, string][] = [
  ['=  !=', 'equals, not equals'],
  ['<  >  <=  >=', 'compare - as numbers when both sides are numbers, with units kb mb gb ms s m'],
  ['=~  !~', 'matches a regular expression, does not match'],
  ['contains', 'has the text, any case'],
  ['in (a, b, c)', 'is one of the values'],
  ['exists', 'is present - a header, a cookie, a parameter'],
  ['AND  OR  NOT  ( )', 'combine - two terms side by side are ANDed'],
]

const examples = computed<[string, string][]>(() => {
  const rows: [string, string][] = [
    ['login', 'anything containing "login", any case'],
    ['req.host = api.example.com', 'one host'],
    ['req.body contains "reset password"', 'a phrase in the request body - quote spaces'],
    ['req.method in (POST, PUT, PATCH)', 'any write'],
    ['req.header.authorization exists', 'authenticated requests'],
    ['req.cookie.session exists AND req.path =~ "^/admin"', 'a cookie plus a path pattern'],
    ['req.form.user = admin', 'a urlencoded form field'],
    ['req.query.id > 100', 'a query parameter, as a number'],
    ['NOT req.ext in (png, css, woff2)', 'drop static assets'],
  ]

  if (props.subject === 'log') {
    rows.push(
      ['req.tag exists AND res.statusCode >= 500', 'tagged entries that failed'],
      ['req.websocket = true AND ws.messages > 100', 'busy WebSocket connections'],
      ['res.type = grpc AND res.grpcStatus != 0', 'gRPC calls that failed'],
      ['req.tunnel = true', 'connections relayed without decrypting'],
    )
  }

  if (props.subject !== 'request') {
    rows.push(
      ['res.body contains "full migration"', 'a phrase in the response body, the page itself'],
      ['res.statusCode >= 400', 'errors'],
      ['res.type in (json, html)', 'by what came back'],
      ['res.size > 1mb AND res.duration > 2s', 'big and slow'],
      ['res.ttfb > 1s AND res.receive < 50ms', 'the server was slow, not the network'],
      ['res.tls > 200ms', 'a costly handshake'],
      ['res.cookie.sid exists', 'a response that sets a cookie'],
    )
  }

  return rows
})

const note = computed(() => {
  switch (props.subject) {
    case 'request':
      return 'A request filter runs before anything is sent, so there is no response to test.'
    case 'response':
      return 'A response filter sees the request it answers too, so req.* and res.* combine.'
    default:
      return 'An entry without a response yet answers "" for every res.* field.'
  }
})
</script>

<template>
  <div class="filter-help">
    <p class="filter-help-lead">
      A bare word matches any field that contains it. A comparison names a field. A value with
      spaces or <code>=</code> goes in quotes. {{ note }}
    </p>

    <h4 class="filter-help-title">Examples</h4>
    <table class="help-table">
      <tbody>
        <tr v-for="[example, meaning] in examples" :key="example">
          <td>
            <code>{{ example }}</code>
          </td>
          <td>{{ meaning }}</td>
        </tr>
      </tbody>
    </table>

    <h4 class="filter-help-title">Operators</h4>
    <table class="help-table">
      <tbody>
        <tr v-for="[op, meaning] in OPERATORS" :key="op">
          <td>
            <code>{{ op }}</code>
          </td>
          <td>{{ meaning }}</td>
        </tr>
      </tbody>
    </table>

    <template v-for="g in groups" :key="g.title">
      <h4 class="filter-help-title">{{ g.title }}</h4>
      <table class="help-table">
        <tbody>
          <tr v-for="f in g.fields" :key="f.name">
            <td>
              <code>{{ f.name }}</code>
            </td>
            <td>{{ f.meaning }}</td>
          </tr>
        </tbody>
      </table>
    </template>
  </div>
</template>
