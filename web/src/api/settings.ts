import api from './client'
import type { InstanceSettings } from './types'

// The instance settings. Project settings live under /project/settings
// and are a different document.
export const settingsApi = {
  get: () => api.get<{ settings: InstanceSettings }>('/settings'),
  putAuthHeaders: (names: string[]) =>
    api.put<{ settings: InstanceSettings }>('/settings/auth-headers', { auth_headers: names }),
}
