// What a status code and a method look like, decided once.

// The badge class for an HTTP status: the class decides the colour.
export function statusClass(code?: number): string {
  if (!code) return 'badge-neutral'
  if (code < 200) return 'badge-info'
  if (code < 300) return 'badge-success'
  if (code < 400) return 'badge-info'
  if (code < 500) return 'badge-warning'
  return 'badge-danger'
}

// The method chip class. Reads are quiet, writes carry a hue.
export function methodClass(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET':
    case 'HEAD':
    case 'OPTIONS':
      return 'method-read'
    case 'POST':
      return 'method-post'
    case 'PUT':
    case 'PATCH':
      return 'method-write'
    case 'DELETE':
      return 'method-delete'
    default:
      return 'method-other'
  }
}

// Which highlighter a body wants, from its content type and a peek.
export type BodyLanguage = 'json' | 'html' | 'xml' | 'javascript' | 'text'

export function bodyLanguage(contentType: string | undefined, body: string): BodyLanguage {
  const ct = (contentType ?? '').toLowerCase()
  if (ct.includes('json')) return 'json'
  if (ct.includes('html')) return 'html'
  if (ct.includes('xml')) return 'xml'
  if (ct.includes('javascript') || ct.includes('ecmascript')) return 'javascript'

  const head = body.trimStart().slice(0, 1)
  if (head === '{' || head === '[') return 'json'
  if (head === '<') return body.toLowerCase().includes('<html') ? 'html' : 'xml'
  return 'text'
}

// Pretty-print a JSON body, or return it unchanged when it is not one.
export function prettyJSON(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

// A bare protobuf body over plain HTTP: the same wire format gRPC
// carries, without the framing. The list matches
// grpcmsg.IsProtobuf on the Go side, which is what actually decodes it.
export function isProtobufType(contentType: string | undefined): boolean {
  const media = (contentType ?? '').split(';')[0]!.trim().toLowerCase()

  return (
    media === 'application/x-protobuf' ||
    media === 'application/protobuf' ||
    media === 'application/vnd.google.protobuf' ||
    media === 'application/octet-stream+protobuf' ||
    media === 'application/x-google-protobuf' ||
    media.endsWith('+protobuf')
  )
}

// The Content-Type header out of an ordered header list.
export function contentType(headers: { name: string; value: string }[] | undefined): string {
  return headers?.find((h) => h.name.toLowerCase() === 'content-type')?.value ?? ''
}

// Host and path of a URL, for a list row that cannot afford the whole thing.
export function shortURL(url: string): { host: string; path: string } {
  try {
    const u = new URL(url)
    return { host: u.host, path: u.pathname + u.search }
  } catch {
    return { host: '', path: url }
  }
}
