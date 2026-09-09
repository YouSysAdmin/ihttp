import api from './client'
import type { Snippets } from './reqlogs'
import type { Header, SenderRequest, SenderSummary } from './types'

// body null on a rewrite keeps the stored bytes, so a binary body that
// is shown but not edited survives a save. '' clears them.
export interface SaveSenderRequest {
  id?: string
  method: string
  url: string
  proto: string
  headers: Header[]
  body: string | null
}

export const senderApi = {
  list: (params: { search?: string }) =>
    api.get<{ requests: SenderSummary[] }>('/sender/requests', { params }),
  get: (id: string) => api.get<{ request: SenderRequest }>(`/sender/requests/${id}`),
  snippets: (id: string) => api.get<Snippets>(`/sender/requests/${id}/snippets`),
  save: (body: SaveSenderRequest) => api.post<{ request: SenderRequest }>('/sender/requests', body),
  send: (id: string) => api.post<{ request: SenderRequest }>(`/sender/requests/${id}/send`),
  clone: (logId: string) => api.post<{ request: SenderRequest }>(`/sender/clone/${logId}`),
  remove: (id: string) => api.delete(`/sender/requests/${id}`),
  clear: () => api.delete('/sender/requests'),
}
