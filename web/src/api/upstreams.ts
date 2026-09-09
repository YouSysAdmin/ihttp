import api from './client'
import type { UpstreamServer, UpstreamSummary, UpstreamTest } from './types'

export const upstreamsApi = {
  // Summaries: a name and the host, never a credential.
  list: () => api.get<{ servers: UpstreamSummary[] }>('/upstreams'),

  // The one call that returns the stored URL as it was typed. Only the
  // editor asks, and only for what it is editing.
  get: (id: string) => api.get<{ server: UpstreamServer }>(`/upstreams/${id}`),

  save: (in_: { id?: string; name: string; url: string; bypass?: string[] }) =>
    api.post<{ server: UpstreamServer }>('/upstreams', in_),

  remove: (id: string) => api.delete(`/upstreams/${id}`),

  // An empty id tests the instance default.
  test: (id = '', target = '') =>
    api.post<UpstreamTest>('/upstreams/test', {
      ...(id ? { id } : {}),
      ...(target ? { target } : {}),
    }),
}
