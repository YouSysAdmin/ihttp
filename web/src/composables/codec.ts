// Encoders and decoders for the Decoder page, all in the browser. Each
// throws on input it cannot handle, and the page shows the message.

const enc = new TextEncoder()
const dec = new TextDecoder()

// Typed over a plain ArrayBuffer: the lib types for BufferSource and Blob
// refuse the SharedArrayBuffer-capable default, and TextEncoder never
// hands one out.
function bytesOf(s: string): Uint8Array<ArrayBuffer> {
  return enc.encode(s) as Uint8Array<ArrayBuffer>
}

function textOf(b: Uint8Array): string {
  return dec.decode(b)
}

export function base64Encode(s: string, urlSafe = false): string {
  let out = btoa(String.fromCharCode(...bytesOf(s)))
  if (urlSafe) out = out.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
  return out
}

export function base64Decode(s: string): string {
  const std = s.trim().replace(/-/g, '+').replace(/_/g, '/')
  const padded = std + '='.repeat((4 - (std.length % 4)) % 4)
  const bin = atob(padded)
  return textOf(Uint8Array.from(bin, (c) => c.charCodeAt(0)))
}

export function urlEncode(s: string): string {
  return encodeURIComponent(s)
}

export function urlDecode(s: string): string {
  return decodeURIComponent(s.replace(/\+/g, ' '))
}

export function htmlEncode(s: string): string {
  return s.replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!,
  )
}

export function htmlDecode(s: string): string {
  const doc = new DOMParser().parseFromString(s, 'text/html')
  return doc.documentElement.textContent ?? ''
}

export function hexEncode(s: string): string {
  return Array.from(bytesOf(s), (b) => b.toString(16).padStart(2, '0')).join('')
}

export function hexDecode(s: string): string {
  const clean = s.replace(/[^0-9a-fA-F]/g, '')
  if (clean.length % 2) throw new Error('odd number of hex digits')
  const bytes = new Uint8Array(clean.length / 2)
  for (let i = 0; i < bytes.length; i++) bytes[i] = parseInt(clean.slice(i * 2, i * 2 + 2), 16)
  return textOf(bytes)
}

// A JWT's header and payload as pretty JSON. The signature is not checked.
export function jwtDecode(s: string): string {
  const parts = s.trim().split('.')
  if (parts.length < 2) throw new Error('not a JWT: expected header.payload.signature')
  const pretty = (p: string) => JSON.stringify(JSON.parse(base64Decode(p)), null, 2)
  return `// header\n${pretty(parts[0]!)}\n\n// payload\n${pretty(parts[1]!)}${
    parts[2] ? `\n\n// signature (${parts[2].length} chars, not verified)` : ''
  }`
}

export function jsonPretty(s: string): string {
  return JSON.stringify(JSON.parse(s), null, 2)
}

export function jsonMinify(s: string): string {
  return JSON.stringify(JSON.parse(s))
}

async function digest(algo: string, s: string): Promise<string> {
  const hash = await crypto.subtle.digest(algo, bytesOf(s))
  return Array.from(new Uint8Array(hash), (b) => b.toString(16).padStart(2, '0')).join('')
}

export const sha1 = (s: string) => digest('SHA-1', s)
export const sha256 = (s: string) => digest('SHA-256', s)
export const sha512 = (s: string) => digest('SHA-512', s)

async function pipe(bytes: Uint8Array<ArrayBuffer>, stream: GenericTransformStream) {
  const out = new Blob([bytes])
    .stream()
    .pipeThrough(stream as unknown as ReadableWritablePair<Uint8Array, Uint8Array>)
  return new Uint8Array(await new Response(out).arrayBuffer())
}

// gzip, then base64 so the result is text.
export async function gzipEncode(s: string): Promise<string> {
  const z = await pipe(bytesOf(s), new CompressionStream('gzip'))
  return btoa(String.fromCharCode(...z))
}

// base64 of a gzip stream back to text.
export async function gzipDecode(s: string): Promise<string> {
  const bin = atob(s.trim())
  const z = Uint8Array.from(bin, (c) => c.charCodeAt(0)) as Uint8Array<ArrayBuffer>
  return textOf(await pipe(z, new DecompressionStream('gzip')))
}

export function timestampDecode(s: string): string {
  const n = Number(s.trim())
  if (!Number.isFinite(n)) throw new Error('not a number')
  const ms = n > 1e12 ? n : n * 1000
  const d = new Date(ms)
  return `${d.toISOString()}\n${d.toLocaleString()}`
}
