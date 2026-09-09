import api from './client'
import type { HostCount, LogEntry, LogSummary, WsMessage, WsMessageSummary } from './types'

export interface LogListParams {
  search?: string
  only_in_scope?: boolean
  //The saved view: only entries that were saved.
  saved?: boolean
  before?: string
  limit?: number
  // Muted hosts are left out of a list unless this is set, which is
  // what the console does when you go looking at one on purpose.
  include_muted?: boolean
}

// What narrows a list, an export or a deleted: the same three switches.
export type LogSelection = Pick<LogListParams, 'search' | 'only_in_scope' | 'saved'>

// One request spelled every way the server knows how to write it. All
// of them arrive together: rendering is cheap and a round trip per
// language is not.
export interface Snippets {
  curl: string
  fetch: string
  httpie: string
  python: string
  go: string
  powershell: string
}

export const reqlogsApi = {
  list: (params: LogListParams) =>
    api.get<{ entries: LogSummary[]; more: boolean }>('/request-logs', { params }),
  get: (id: string) => api.get<{ entry: LogEntry }>(`/request-logs/${id}`),
  snippets: (id: string) => api.get<Snippets>(`/request-logs/${id}/snippets`),

  // A member left out is left as it is on the server.
  patch: (id: string, marks: { tags?: string[]; note?: string; color?: string }) =>
    api.patch<{ entry: LogEntry }>(`/request-logs/${id}`, marks),
  tags: () => api.get<{ tags: { name: string; count: number }[] }>('/request-logs/tags'),
  hosts: () => api.get<{ hosts: HostCount[] }>('/request-logs/hosts'),
  // The slowest or largest of the current filter: what column sorting
  // is for, without an index the log does not have.
  top: (params: LogSelection & { by: 'duration' | 'size'; limit?: number }) =>
    api.get<{ entries: LogSummary[] }>('/request-logs/top', { params }),

  // Newest first: `before` is the cursor, 0 or absent starts at the newest.
  messages: (id: string, params: { before?: number; limit?: number }) =>
    api.get<{ messages: WsMessageSummary[]; more: boolean }>(`/request-logs/${id}/messages`, {
      params: { ...params, order: 'desc' },
    }),
  message: (id: string, seq: number) =>
    api.get<{ message: WsMessage }>(`/request-logs/${id}/messages/${seq}`),

  // The decode endpoint, as a path the body view fetches itself.
  grpcUrl: (id: string, side: 'request' | 'response') => `/api/request-logs/${id}/grpc/${side}`,

  // A download link, not a request: the browser fetches it as a file.
  harUrl: (params: LogSelection) => {
    const q = new URLSearchParams()
    if (params.search) q.set('search', params.search)
    if (params.only_in_scope) q.set('only_in_scope', 'true')
    if (params.saved) q.set('saved', 'true')
    const qs = q.toString()
    return '/api/request-logs/export.har' + (qs ? '?' + qs : '')
  },
  remove: (id: string) => api.delete(`/request-logs/${id}`),

  // Without a search or scope the whole log goes, with one only what
  // matches, and the answer says how many.
  clear: (params: LogSelection = {}) =>
    api.delete<{ deleted: number } | ''>('/request-logs', { params }),
  // The body is the file itself, not a form: one stream from the disk
  // to the parser, so a large HAR never has to be held twice.
  importHar: (file: File) =>
    api.post<{ entries: number }>('/request-logs/import.har', file, {
      headers: { 'Content-Type': 'application/json' },
    }),
  save: (id: string) => api.post<{ entry: LogEntry }>(`/request-logs/${id}/save`),
  unsave: (id: string) => api.delete<{ entry: LogEntry }>(`/request-logs/${id}/save`),
}
