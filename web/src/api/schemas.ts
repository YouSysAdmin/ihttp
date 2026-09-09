import api from './client'
import type { Schema } from './types'

export const schemasApi = {
  list: () => api.get<{ schemas: Schema[] }>('/project/schemas'),
  // The file is the body, its name in the query.
  upload: (file: File) =>
    api.post<{ schema: Schema }>('/project/schemas', file, {
      params: { name: file.name },
      headers: { 'Content-Type': 'application/octet-stream' },
    }),
  remove: (id: string) => api.delete(`/project/schemas/${id}`),
}
