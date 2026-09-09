import api from './client'
import type { Snippets } from './reqlogs'
import type {
  AutomationJob,
  AutomationPayload,
  AutomationResult,
  AutomationResultSummary,
  AutomationStopCondition,
  AutomationSummary,
  Header,
} from './types'

// body null on a rewrite keeps the stored bytes, so a binary body that
// is shown but not edited survives a save. '' clears them.
export interface SaveAutomationJob {
  id?: string
  name: string
  method: string
  url: string
  proto: string
  headers: Header[]
  body: string | null
  placeholder: string
  url_encode: boolean
  payload: AutomationPayload
  concurrency: number
  stop_match: string
  stop_on: AutomationStopCondition[]
}

export const automationApi = {
  list: (params: { search?: string }) =>
    api.get<{ jobs: AutomationSummary[] }>('/automation/jobs', { params }),
  get: (id: string) => api.get<{ job: AutomationJob }>(`/automation/jobs/${id}`),
  save: (body: SaveAutomationJob) => api.post<{ job: AutomationJob }>('/automation/jobs', body),
  start: (id: string) => api.post<{ job: AutomationJob }>(`/automation/jobs/${id}/start`),
  stop: (id: string) => api.post(`/automation/jobs/${id}/stop`),
  results: (id: string) =>
    api.get<{ results: AutomationResultSummary[] }>(`/automation/jobs/${id}/results`),
  result: (id: string, rid: string) =>
    api.get<{ result: AutomationResult }>(`/automation/jobs/${id}/results/${rid}`),
  snippets: (id: string, rid: string) =>
    api.get<Snippets>(`/automation/jobs/${id}/results/${rid}/snippets`),
  harUrl: (id: string) => `/api/automation/jobs/${id}/export.har`,
  clone: (logId: string) => api.post<{ job: AutomationJob }>(`/automation/clone/${logId}`),
  remove: (id: string) => api.delete(`/automation/jobs/${id}`),
  clear: () => api.delete('/automation/jobs'),
}
