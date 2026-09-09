import axios from 'axios'

// One base path: the console talks to the API on its own origin, and
// there is no session - the console listener binds to localhost and
// whoever reaches it operates the tool.
const api = axios.create({
  baseURL: '/api',
  headers: {
    'Content-Type': 'application/json',
  },
})

// The backend error envelope is {"error": "message"}.
export function apiErrorMessage(err: unknown, fallback = 'Request failed'): string {
  const e = err as { response?: { data?: { error?: string } }; message?: string }
  return e?.response?.data?.error || e?.message || fallback
}

export default api
