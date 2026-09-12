import api from './client'
import type {
  HostOverride,
  InterceptSettings,
  Project,
  RequestLogSettings,
  Rule,
  ScopeRule,
  Settings,
  View,
} from './types'

export const projectsApi = {
  list: () => api.get<{ projects: Project[] }>('/projects'),
  create: (name: string) => api.post<{ project: Project }>('/projects', { name }),
  active: () => api.get<{ project: Project | null }>('/projects/active'),
  open: (id: string) => api.post<{ project: Project }>(`/projects/${id}/open`),
  close: () => api.post('/projects/close'),
  remove: (id: string) => api.delete(`/projects/${id}`),
  // A download link, the browser fetches it as a file.
  // settingsOnly leaves the traffic out: the settings, the rules, the
  // scope and the filters, and nobody's captured exchanges.
  exportUrl: (id: string, settingsOnly = false) =>
    `/api/projects/${id}/export${settingsOnly ? '?settings_only=1' : ''}`,
  // The file is the body, unchanged, and the answer is the new project.
  importFile: (file: File) =>
    api.post<{ project: Project }>('/projects/import', file, {
      headers: { 'Content-Type': 'application/json' },
    }),

  putScope: (rules: ScopeRule[]) =>
    api.put<{ settings: Settings }>('/project/settings/scope', { rules }),
  putIntercept: (s: InterceptSettings) =>
    api.put<{ settings: Settings }>('/project/settings/intercept', s),
  putRequestLog: (s: RequestLogSettings) =>
    api.put<{ settings: Settings }>('/project/settings/request-log', s),
  putRules: (rules: Rule[]) =>
    api.put<{ settings: Settings }>('/project/settings/rules', { rules }),
  putViews: (views: View[]) =>
    api.put<{ settings: Settings }>('/project/settings/views', { views }),
  // The hosts the OPEN project relays without decrypting them.
  putNoDecrypt: (hosts: string[]) =>
    api.put<{ settings: Settings }>('/project/settings/no-decrypt', { hosts }),
  // Which of the instance's proxies the OPEN project goes out through:
  // '' inherits the instance default, 'direct' forces no proxy, an id
  // picks one from the list.
  putUpstream: (upstream: string) =>
    api.put<{ settings: Settings }>('/project/settings/upstream', { upstream }),
  putHostOverrides: (overrides: HostOverride[]) =>
    api.put<{ settings: Settings }>('/project/settings/host-overrides', { overrides }),
}
