import { defineStore } from 'pinia'
import { ref } from 'vue'
import { settingsApi } from '../api/settings'

// The instance settings, loaded once and kept for the life of the tab.
//
// Server state, like the open project, but it changes only when someone
// edits the Settings page, so there is no event to follow: the page that
// saves updates the store, and another tab picks it up on its next load.
export const useSettingsStore = defineStore('settings', () => {
  const authHeaders = ref<string[]>([])
  const loaded = ref(false)

  let inflight: Promise<void> | null = null

  async function fetchAll() {
    const res = await settingsApi.get()
    authHeaders.value = res.data.settings.auth_headers ?? []
    loaded.value = true
  }

  // ensure loads once. Several panes ask at the same moment - every
  // message on a page asks - so they share one request.
  function ensure(): Promise<void> {
    if (loaded.value) return Promise.resolve()
    if (!inflight) inflight = fetchAll().finally(() => (inflight = null))

    return inflight
  }

  async function saveAuthHeaders(names: string[]) {
    const res = await settingsApi.putAuthHeaders(names)
    authHeaders.value = res.data.settings.auth_headers ?? []
    loaded.value = true
  }

  return { authHeaders, loaded, ensure, fetchAll, saveAuthHeaders }
})
