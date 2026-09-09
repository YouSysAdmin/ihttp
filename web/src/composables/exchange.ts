// One shape for an exchange wherever it came from, so a page that reads
// two of them side by side does not care which tool made each.
import type { AutomationResult, Header, LogEntry, SenderRequest } from '../api/types'

export interface Half {
  // The first line: the request line, or the status line.
  statusLine: string
  headers: Header[]
  body: string
  binary: boolean
  size: number
  rawUrl: string
}

export interface Exchange {
  label: string
  method: string
  url: string
  statusCode?: number
  status?: string
  request: Half
  response: Half | null
}

export function fromLog(e: LogEntry): Exchange {
  return {
    label: `${e.method} ${e.url}`,
    method: e.method,
    url: e.url,
    statusCode: e.response?.status_code,
    status: e.response?.status,
    request: {
      statusLine: `${e.method} ${e.url} ${e.proto}`,
      headers: e.headers,
      body: e.body,
      binary: e.body_binary,
      size: e.body_size,
      rawUrl: `/api/request-logs/${e.id}/body/request`,
    },
    response: e.response
      ? {
          statusLine: `${e.response.proto} ${e.response.status_code} ${e.response.status}`,
          headers: e.response.headers,
          body: e.response.body,
          binary: e.response.body_binary,
          size: e.response.body_size,
          rawUrl: `/api/request-logs/${e.id}/body/response`,
        }
      : null,
  }
}

export function fromSender(r: SenderRequest): Exchange {
  return {
    label: `${r.method} ${r.url}`,
    method: r.method,
    url: r.url,
    statusCode: r.response?.status_code,
    status: r.response?.status,
    request: {
      statusLine: `${r.method} ${r.url} ${r.proto}`,
      headers: r.headers,
      body: r.body,
      binary: r.body_binary,
      size: r.body_size,
      rawUrl: `/api/sender/requests/${r.id}/body/request`,
    },
    response: r.response
      ? {
          statusLine: `${r.response.proto} ${r.response.status_code} ${r.response.status}`,
          headers: r.response.headers,
          body: r.response.body,
          binary: r.response.body_binary,
          size: r.response.body_size,
          rawUrl: `/api/sender/requests/${r.id}/body/response`,
        }
      : null,
  }
}

export function fromAutomation(jobId: string, r: AutomationResult): Exchange {
  const base = `/api/automation/jobs/${jobId}/results/${r.id}/body`
  const method = r.req_method ?? ''
  const url = r.req_url ?? ''
  return {
    label: `#${r.index + 1} ${r.payload}`,
    method,
    url,
    statusCode: r.status_code,
    status: r.status,
    request: {
      statusLine: `${method} ${url}`,
      headers: r.req_headers ?? [],
      body: r.req_body ?? '',
      binary: r.req_body_binary,
      size: r.req_body_size,
      rawUrl: `${base}/request`,
    },
    response:
      r.status_code && !r.error
        ? {
            statusLine: `${r.proto ?? ''} ${r.status_code} ${r.status ?? ''}`,
            headers: r.headers ?? [],
            body: r.body ?? '',
            binary: r.body_binary,
            size: r.body_size,
            rawUrl: `${base}/response`,
          }
        : null,
  }
}

// The first line and the headers, sorted by name, as one document.
export function headersDoc(h: Half): string {
  const lines = [...h.headers]
    .sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))
    .map((x) => `${x.name}: ${x.value}`)
  return [h.statusLine, ...lines].join('\n')
}
