// What carries identity in a message, picked out of its headers.
//
// Nothing here is decoded and nothing is asked of the server. The point
// is the shortlist: a Cookie is easy to lose among thirty headers and an
// X-Api-Key is easier, and both are usually what a request is being read
// for. Everything stays visible under Headers as well - this narrows the
// view, it never hides anything.
//
// The built-in list is the CROSS-PLATFORM spellings only. A header one
// vendor invented is a house header like any other and is named by the
// person who works with it, in Settings, which is where AUTH_HEADER_NAMES
// gets its extras from.
import type { Cookie, SetCookie } from './cookies'
import { cookiesOf } from './cookies'

export interface AuthHeader {
  name: string
  value: string
  // The scheme of an Authorization-style value - "Bearer", "Basic",
  // "Digest" - or '' when the value is the credential itself.
  scheme: string
  // What follows the scheme, or the whole value when there is none.
  credential: string
}

// AUTH_HEADER_NAMES are the headers that carry a credential whoever is
// serving, in their usual spelling - the Settings page shows this list,
// and matching is done without case. Cookie and Set-Cookie are absent on
// purpose: they get their own tables, split one cookie per row.
export const AUTH_HEADER_NAMES = [
  'Authorization',
  'Proxy-Authorization',
  'WWW-Authenticate',
  'Proxy-Authenticate',
  'Authentication-Info',
  'API-Key',
  'X-API-Key',
  'X-Auth-Token',
  'X-Access-Token',
  'X-Refresh-Token',
  'X-ID-Token',
  'CSRF-Token',
  'X-CSRF-Token',
  'XSRF-Token',
  'X-XSRF-Token',
]

const BUILT_IN = new Set(AUTH_HEADER_NAMES.map((n) => n.toLowerCase()))

// isAuthHeader says whether a header name carries a credential. `extra`
// are the instance's own names, matched without case like every header.
export function isAuthHeader(name: string, extra: string[] = []): boolean {
  const lower = name.toLowerCase()
  if (lower === 'cookie' || lower === 'set-cookie') return false
  if (BUILT_IN.has(lower)) return true

  return extra.some((e) => e.toLowerCase() === lower)
}

// splitScheme reads "Bearer xyz" as a scheme and a credential. A value
// with no leading token, or nothing after it, is the credential whole.
function splitScheme(value: string): { scheme: string; credential: string } {
  const at = value.indexOf(' ')
  if (at < 0) return { scheme: '', credential: value }

  const head = value.slice(0, at)
  const rest = value.slice(at + 1).trim()
  if (!rest || !/^[A-Za-z][A-Za-z0-9-]*$/.test(head)) {
    return { scheme: '', credential: value }
  }

  return { scheme: head, credential: rest }
}

// authHeadersOf pulls the credential headers out of an ordered header
// list, in the order they arrived.
export function authHeadersOf(
  headers: { name: string; value: string }[] | undefined,
  extra: string[] = [],
): AuthHeader[] {
  const out: AuthHeader[] = []

  for (const h of headers ?? []) {
    if (!isAuthHeader(h.name, extra)) continue

    out.push({ name: h.name, value: h.value, ...splitScheme(h.value) })
  }

  return out
}

export interface Credentials {
  headers: AuthHeader[]
  sent: Cookie[]
  set: SetCookie[]
  // Rows the view would draw, which is what the tab counts.
  count: number
}

// credentialsOf reads both halves of the Auth view - the credential
// headers and the cookies - out of one header list.
export function credentialsOf(
  headers: { name: string; value: string }[] | undefined,
  extra: string[] = [],
): Credentials {
  const auth = authHeadersOf(headers, extra)
  const cookies = cookiesOf(headers)

  return {
    headers: auth,
    sent: cookies.sent,
    set: cookies.set,
    count: auth.length + cookies.sent.length + cookies.set.length,
  }
}
