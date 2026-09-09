// TypeScript mirrors of the Go wire types, keyed by json tag name.

export interface Header {
  name: string
  value: string
}

export interface ScopeRule {
  url: string
  header_key: string
  header_value: string
  body: string
}

export interface RequestLogSettings {
  paused: boolean
  bypass_out_of_scope: boolean
  ignore_filter: string
  // 0 is no cap. Past it the oldest entries go as new ones arrive, and
  // saved entries are never dropped.
  max_entries?: number
  // Hosts hidden from the log VIEW while still being captured, kept and
  // exported. The opposite of ignore_filter, which never writes.
  muted_hosts?: string[]
}

// What a GraphQL exchange is doing, derived by the server on read. Its
// presence is the signal that the exchange IS GraphQL - the request
// line only ever says POST /graphql.
export interface GraphQLInfo {
  type?: string
  name?: string
  // Set only when several operations came in one request.
  batch?: number
  // Errors in the result. A failed GraphQL call is usually a 200.
  errors?: number
}

// One host in the log: how many entries, how many were answered with a
// 5xx, and whether the project has muted it.
export interface HostCount {
  host: string
  count: number
  errors: number
  muted?: boolean
}

// A named way of looking at the log. The query is source text, the same
// filter language the bar takes. A view narrows what is shown and never
// decides what is logged.
export interface View {
  id: string
  name: string
  query: string
  only_in_scope: boolean
  saved: boolean
}

export interface InterceptSettings {
  requests_enabled: boolean
  responses_enabled: boolean
  request_filter: string
  response_filter: string
}

export type RuleActionType =
  | 'mock'
  | 'map_local'
  | 'rewrite_url'
  | 'set_request_header'
  | 'remove_request_header'
  | 'set_response_header'
  | 'remove_response_header'
  | 'set_status'
  | 'replace_body'
  | 'replace_request_body'
  | 'delay'
  | 'delay_response'
  | 'block'
  | 'throttle'
  | 'allow_cors'
  | 'capture'

// One regular expression and what replaces its matches.
export interface Replacement {
  pattern: string
  replace: string
}

export interface RuleAction {
  type: RuleActionType
  // A single header, kept for compatibility. The server folds it into
  // headers on save, so nothing else reads it.
  header?: string
  value?: string
  status?: number
  // Mock: response headers. Set actions: headers to set. Remove actions:
  // the names to drop.
  headers?: Header[]
  body?: string
  path?: string
  // Rewrite URL: the pattern, empty for the rule's URL match, and its
  // replacement. For the body actions, a single replacement kept for
  // compatibility and folded into replacements on save.
  pattern?: string
  replace?: string
  replacements?: Replacement[]
  // Delay and delay_response.
  delay_ms?: number
  // Throttle: bytes a second.
  rate_bps?: number
  // Capture: the field to read - a key of the filter language - and
  // what to remember it as. `pattern` above narrows the value.
  from?: string
  name?: string
}

// One value a capture rule has read, for ${name} to put back. Runtime
// state: it lives while the project is open and is never stored.
export interface RuleVar {
  name: string
  value: string
  rule?: string
  from?: string
  at: string
}

export interface Rule {
  id: string
  enabled: boolean
  name: string
  url: string
  method: string
  // A query in the same filter language, decided on the request alone -
  // a rule runs before the response exists, so res.* is refused. It
  // narrows further: url, method and filter all have to agree.
  filter?: string
  action: RuleAction
}

export interface Settings {
  request_log: RequestLogSettings
  intercept: InterceptSettings
  scope: ScopeRule[]
  rules: Rule[]
  views?: View[]
  // Host globs relayed WITHOUT being decrypted: nobody in the middle
  // sees the bytes, and the connection is logged as a CONNECT alone.
  no_decrypt?: string[]
  // Which of the instance's proxies this project goes out through:
  // absent to inherit the instance default, UPSTREAM_DIRECT to go out
  // on our own, or the id of one in the list.
  upstream?: string
}

export interface Project {
  id: string
  name: string
  settings: Settings
  created_at: string
  updated_at: string
  is_active: boolean
}

export interface ResponseLog {
  proto: string
  status_code: number
  status: string
  headers: Header[]
  trailers?: Header[]
  body: string
  body_truncated: boolean
  body_streamed: boolean
  body_binary: boolean
  body_size: number
  received_at: string
  duration_ms: number
  // Absent when the exchange was not measured: an entry logged before
  // the proxy timed phases, or a response a rule answered locally.
  // Absent is not zero - say "not measured" rather than draw nothing.
  timings?: Timings
}

// Timings is where one exchange spent its time, in milliseconds. A
// phase that did not happen is zero, and zero is not the same as fast:
// on a reused connection the three connection phases never ran, and
// receive is zero for a streamed body the proxy never read.
export interface Timings {
  blocked_ms: number
  dns_ms: number
  connect_ms: number
  tls_ms: number
  send_ms: number
  wait_ms: number
  receive_ms: number
  reused?: boolean
  server_addr?: string
  tls_version?: string
  alpn?: string
}

export interface LogSummary {
  id: string
  created_at: string
  method: string
  url: string
  proto: string
  status_code?: number
  status?: string
  duration_ms?: number
  response_size?: number
  request_size?: number
  streamed?: boolean
  tags?: string[]
  color?: string
  websocket?: boolean
  messages?: number
  // The row is a connection relayed without decryption, so a reader
  // does not expect a body to open.
  tunnel?: boolean
  graphql?: GraphQLInfo | null
  saved?: boolean
}

// One protobuf field as the wire shows it, without a schema.
export interface GrpcField {
  number: number
  wire: string
  value: string
  nested?: GrpcField[]
}

// One length-prefixed gRPC frame. `raw` is base64 of the message bytes.
export interface GrpcFrame {
  index: number
  compressed: boolean
  trailer: boolean
  length: number
  raw: string
  fields?: GrpcField[]
  trailers?: Header[]
  error?: string
  // The message read with its schema, as JSON.
  json?: string
}

export interface GrpcBody {
  // 'protobuf' is a bare protobuf message over plain HTTP, or a
  // WebSocket frame read as one: the same wire format with no framing
  // around it, so it comes back as a single frame.
  kind: 'grpc' | 'grpc-web' | 'grpc-web-text' | 'protobuf'
  service: string
  method: string
  status?: string
  message?: string
  frames: GrpcFrame[]
  more: boolean
  error?: string
  // A project schema named the message.
  has_schema: boolean
  message_type?: string
}

// A gRPC schema the project holds: a .proto or a descriptor set.
export interface Schema {
  id: string
  name: string
  kind: 'proto' | 'descriptor_set'
  uploaded_at: string
  size: number
  files: string[]
  services: string[]
}

// What an entry knows about the WebSocket connection it became.
export interface WebSocketInfo {
  subprotocol?: string
  messages: number
  dropped?: number
  closed_at?: string
  close_code?: number
  close_reason?: string
  error?: string
}

export interface WsMessageSummary {
  id: string
  entry_id: string
  seq: number
  direction: 'in' | 'out'
  opcode: string
  timestamp: string
  size: number
  preview?: string
  truncated?: boolean
  compressed?: boolean
  close_code?: number
  close_reason?: string
}

export interface WsMessage extends WsMessageSummary {
  payload: string
  payload_truncated: boolean
  payload_binary: boolean
  payload_size: number
  frames: number
}

// What is known about a connection the proxy relayed WITHOUT looking
// inside it. closed_at is absent while it is still open.
export interface TunnelInfo {
  bytes_out: number
  bytes_in: number
  closed_at?: string
  error?: string
}

// The palette an entry can be marked with.
export const MARK_COLORS = ['red', 'orange', 'yellow', 'green', 'blue', 'purple', 'gray'] as const
export type MarkColor = (typeof MARK_COLORS)[number]

export interface LogEntry {
  id: string
  project_id: string
  created_at: string
  method: string
  url: string
  proto: string
  headers: Header[]
  body: string
  body_truncated: boolean
  body_binary: boolean
  body_size: number
  tags?: string[]
  note?: string
  color?: string
  saved?: boolean
  saved_at?: string
  response: ResponseLog | null
  websocket?: WebSocketInfo | null
  // Set on an entry that was relayed without being decrypted, in which
  // case there is no message to read - only that it happened.
  tunnel?: TunnelInfo | null
  graphql?: GraphQLInfo | null
}

export type InterceptKind = 'request' | 'response'

export interface InterceptRequest {
  method: string
  url: string
  proto: string
  headers: Header[]
  body: string
  body_binary: boolean
  body_size: number
}

export interface InterceptResponse {
  proto: string
  status_code: number
  status: string
  headers: Header[]
  body: string
  body_binary: boolean
  body_size: number
}

export interface InterceptItem {
  id: string
  kind: InterceptKind
  received_at: string
  request: InterceptRequest
  response?: InterceptResponse
}

export interface SenderSummary {
  id: string
  source_log_id?: string
  created_at: string
  updated_at: string
  method: string
  url: string
  proto: string
  status_code?: number
  status?: string
  duration_ms?: number
}

export interface SenderRequest {
  id: string
  project_id: string
  source_log_id?: string
  created_at: string
  updated_at: string
  method: string
  url: string
  proto: string
  headers: Header[]
  body: string
  body_binary: boolean
  body_size: number
  response: ResponseLog | null
}

export interface Info {
  version: string
  proxy_addr: string
  proxy_url: string
  console_url: string
  data_dir: string
  // Where the CA actually is - not always under data_dir, since
  // --ca-cert may put it anywhere, and a setup snippet has to name it
  // exactly.
  ca_path: string
  // The host the proxy answers itself, for checking a setup with no
  // network and no real target.
  probe_url: string
  active_project_id: string
  // The proxy this instance goes out through, with any password shown
  // as a mark. Absent when none is configured.
  upstream_proxy?: string
  // Host globs reached directly. localhost always is and is not listed.
  upstream_bypass?: string[]
  // The certificates this instance presents to an upstream that asks
  // for one. Never any key material.
  client_certs?: ClientCert[]
}

// One client certificate as the console is told about it: which hosts
// it is for and whether it is still valid.
export interface ClientCert {
  host: string
  subject: string
  issuer: string
  not_after: string
  expired?: boolean
  path: string
}

// A row of the upstream-proxy list. Host is the URL with any
// credentials removed entirely, which is what a list, a picker and a
// log line show - the full URL only ever reaches the editor.
export interface UpstreamSummary {
  id: string
  name: string
  host: string
  bypass?: string[]
  has_credentials?: boolean
}

// One server as the editor sees it: the URL as it was typed.
export interface UpstreamServer {
  id: string
  name: string
  url: string
  bypass?: string[]
  created_at: string
  updated_at: string
}

// The value of Settings.upstream that means "out on our own", as
// against an empty value, which means "whatever the instance default
// is".
export const UPSTREAM_DIRECT = 'direct'

// What a test through the upstream proxy found. error is empty when
// nothing went wrong. A proxy that refuses is a result, not a failure of
// the request that asked.
export interface UpstreamTest {
  result: {
    // The proxy's NAME, not its URL: the console knows it by name and a
    // URL here would carry a credential into a message.
    proxy: string
    target: string
    direct: boolean
    took_ms: number
    status?: number
  }
  error?: string
}

export const METHODS = [
  'GET',
  'POST',
  'PUT',
  'PATCH',
  'DELETE',
  'HEAD',
  'OPTIONS',
  'TRACE',
] as const

export const PROTOS = ['HTTP/2.0', 'HTTP/1.1', 'HTTP/1.0'] as const

export interface AutomationStopCondition {
  field: string
  op: string
  header?: string
  value?: string
}

export type AutomationPayloadKind = 'list' | 'numbers' | 'random' | 'library'

export interface AutomationPayload {
  kind: AutomationPayloadKind
  // A list is lines typed here or read from `file` on the server's disk.
  // `separator` splits each into the columns $1, $2 and so on.
  list?: string[]
  file?: string
  separator?: string
  from?: number
  to?: number
  step?: number
  count?: number
  length?: number
  charset?: string
}

export type AutomationStatus = 'draft' | 'running' | 'done' | 'stopped' | 'error'

export interface AutomationSummary {
  id: string
  created_at: string
  updated_at: string
  name: string
  method: string
  url: string
  status: AutomationStatus
  total: number
  completed: number
  error?: string
  payload_kind: AutomationPayloadKind
}

export interface AutomationJob {
  id: string
  project_id: string
  created_at: string
  updated_at: string
  name: string
  method: string
  url: string
  proto: string
  headers: Header[]
  body: string
  placeholder: string
  url_encode: boolean
  payload: AutomationPayload
  concurrency: number
  stop_match: string
  stop_on?: AutomationStopCondition[]
  status: AutomationStatus
  total: number
  completed: number
  error?: string
  started_at?: string
  finished_at?: string
  body_binary: boolean
  body_size: number
  positions: number
  columns: number
}

export interface AutomationResultSummary {
  id: string
  index: number
  payload: string
  status_code?: number
  status?: string
  size?: number
  duration_ms?: number
  error?: string
  matched?: boolean
}

export interface AutomationResult {
  id: string
  job_id: string
  index: number
  payload: string
  req_method?: string
  req_url?: string
  req_headers?: Header[]
  req_body?: string
  req_body_binary: boolean
  req_body_size: number
  matched?: boolean
  status_code?: number
  status?: string
  duration_ms?: number
  error?: string
  proto?: string
  headers?: Header[]
  body?: string
  body_truncated?: boolean
  body_binary: boolean
  body_size: number
}
