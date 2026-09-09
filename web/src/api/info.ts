import api from './client'
import type { Info } from './types'

export const infoApi = {
  get: () => api.get<Info>('/info'),
  caURL: '/api/ca.pem',
}
