import api from './client'
import type { Header, InterceptItem } from './types'

// body: null keeps the original bytes - what a binary body the console
// cannot edit is sent as. "" replaces them with nothing.
export interface ForwardRequestBody {
  method?: string
  url?: string
  headers?: Header[]
  body?: string | null
  intercept_response?: boolean | null
}

export interface ForwardResponseBody {
  status_code?: number
  status?: string
  headers?: Header[]
  body?: string | null
}

export const interceptApi = {
  items: () => api.get<{ items: InterceptItem[] }>('/intercept/items'),
  forwardRequest: (id: string, body?: ForwardRequestBody) =>
    api.post(`/intercept/requests/${id}/forward`, body ?? {}),
  dropRequest: (id: string) => api.post(`/intercept/requests/${id}/drop`),
  forwardResponse: (id: string, body?: ForwardResponseBody) =>
    api.post(`/intercept/responses/${id}/forward`, body ?? {}),
  dropResponse: (id: string) => api.post(`/intercept/responses/${id}/drop`),
}
