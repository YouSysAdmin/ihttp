// Cookies and form bodies, read out of what is already there.
//
// Nothing here is stored or asked of the server: the filter language
// already exposes req.cookie.<name>, res.cookie.<name> and
// req.form.<name>, so the parsing exists on the Go side for matching.
// This is the same reading done for the eye, derived on the spot.

export interface Cookie {
  name: string
  value: string
}

// parseCookieHeader reads a request's Cookie header: name=value pairs,
// one per semicolon. A pair with no "=" is kept as a name with no value
// rather than dropped - a malformed cookie is worth seeing.
export function parseCookieHeader(value: string): Cookie[] {
  const out: Cookie[] = []

  for (const part of value.split(';')) {
    const pair = part.trim()
    if (!pair) continue

    const at = pair.indexOf('=')
    if (at < 0) out.push({ name: pair, value: '' })
    else out.push({ name: pair.slice(0, at).trim(), value: pair.slice(at + 1).trim() })
  }

  return out
}

export interface SetCookie extends Cookie {
  // The attributes as they were sent, in order, so an unknown one is
  // shown rather than swallowed.
  attributes: { name: string; value: string }[]
}

// parseSetCookie reads one Set-Cookie header: the pair first, then its
// attributes.
export function parseSetCookie(value: string): SetCookie {
  const parts = value.split(';')
  const first = (parts.shift() ?? '').trim()
  const at = first.indexOf('=')

  const out: SetCookie = {
    name: at < 0 ? first : first.slice(0, at).trim(),
    value: at < 0 ? '' : first.slice(at + 1).trim(),
    attributes: [],
  }

  for (const raw of parts) {
    const part = raw.trim()
    if (!part) continue

    const eq = part.indexOf('=')
    if (eq < 0) out.attributes.push({ name: part, value: '' })
    else out.attributes.push({ name: part.slice(0, eq).trim(), value: part.slice(eq + 1).trim() })
  }

  return out
}

// cookiesOf pulls both kinds out of an ordered header list: what the
// client sent, and what the server set.
export function cookiesOf(headers: { name: string; value: string }[] | undefined): {
  sent: Cookie[]
  set: SetCookie[]
} {
  const sent: Cookie[] = []
  const set: SetCookie[] = []

  for (const h of headers ?? []) {
    const name = h.name.toLowerCase()
    if (name === 'cookie') sent.push(...parseCookieHeader(h.value))
    else if (name === 'set-cookie') set.push(parseSetCookie(h.value))
  }

  return { sent, set }
}

export interface FormField {
  name: string
  value: string
  // A file part of a multipart body: the name it was uploaded under and
  // what it said it was. The bytes are in the body, not here.
  filename?: string
  contentType?: string
  size?: number
}

// urlencodedFields reads an application/x-www-form-urlencoded body.
// Decoding is per field, so one bad escape does not lose the rest.
export function urlencodedFields(body: string): FormField[] {
  const out: FormField[] = []

  for (const part of body.split('&')) {
    if (!part) continue

    const at = part.indexOf('=')
    const name = at < 0 ? part : part.slice(0, at)
    const value = at < 0 ? '' : part.slice(at + 1)

    out.push({ name: decodeField(name), value: decodeField(value) })
  }

  return out
}

function decodeField(raw: string): string {
  try {
    return decodeURIComponent(raw.replace(/\+/g, ' '))
  } catch {
    // Not valid percent-encoding. What was sent is more useful than an
    // error, since that is what the server had to read too.
    return raw
  }
}

// multipartBoundary reads the boundary out of a Content-Type, or '' when
// there is none.
export function multipartBoundary(contentType: string): string {
  const m = /boundary=("?)([^";]+)\1/i.exec(contentType)

  return m ? m[2]! : ''
}

// multipartFields splits a multipart/form-data body into its parts. The
// body reaching the console has been coerced to text, so a binary part
// is reported by its name, type and length rather than shown.
export function multipartFields(body: string, boundary: string): FormField[] {
  if (!boundary) return []

  const out: FormField[] = []
  const marker = '--' + boundary

  for (const chunk of body.split(marker)) {
    const part = chunk.replace(/^\r?\n/, '')
    if (!part || part.startsWith('--')) continue

    // Headers, a blank line, then the content.
    const split = /\r?\n\r?\n/.exec(part)
    if (!split) continue

    const head = part.slice(0, split.index)
    const value = part.slice(split.index + split[0].length).replace(/\r?\n$/, '')

    const field: FormField = { name: '', value, size: value.length }

    for (const line of head.split(/\r?\n/)) {
      const at = line.indexOf(':')
      if (at < 0) continue

      const name = line.slice(0, at).trim().toLowerCase()
      const header = line.slice(at + 1).trim()

      if (name === 'content-disposition') {
        field.name = quoted(header, 'name') ?? field.name
        field.filename = quoted(header, 'filename') ?? undefined
      } else if (name === 'content-type') {
        field.contentType = header
      }
    }

    out.push(field)
  }

  return out
}

// quoted reads a name="value" parameter out of a header value.
function quoted(header: string, param: string): string | null {
  const m = new RegExp(param + '=("?)([^";]*)\\1', 'i').exec(header)

  return m ? m[2]! : null
}

export type FormKind = 'urlencoded' | 'multipart' | ''

// formKind says whether a body is a form, from its content type alone.
export function formKind(contentType: string): FormKind {
  const media = contentType.split(';')[0]!.trim().toLowerCase()

  if (media === 'application/x-www-form-urlencoded') return 'urlencoded'
  if (media === 'multipart/form-data') return 'multipart'

  return ''
}
