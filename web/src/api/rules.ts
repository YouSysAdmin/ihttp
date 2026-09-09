import api from './client'
import type { RuleVar } from './types'

// The rules themselves are project settings - projectsApi.putRules -
// and this is only the runtime side of them: what the capture rules
// have read so far.
export const rulesApi = {
  variables: () => api.get<{ variables: RuleVar[] }>('/rules/variables'),
  clearVariables: () => api.delete('/rules/variables'),
}
