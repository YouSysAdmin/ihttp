// The curl and fetch() spellings of whatever exchange a page shows,
// fetched when the shown one changes, with the stale-answer guard once.
import { ref, watch } from 'vue'
import type { Snippets } from '../api/reqlogs'

// key names what is on show and changes when the snippets must be
// refetched, '' when nothing is. load fetches for the current key. An
// answer that arrives after the key moved on is dropped.
export function useSnippets(key: () => string, load: () => Promise<{ data: Snippets }>) {
  const snippets = ref<Snippets | null>(null)

  watch(
    key,
    async (k) => {
      snippets.value = null
      if (!k) return
      try {
        const res = await load()
        if (key() === k) snippets.value = res.data
      } catch {
        if (key() === k) snippets.value = null
      }
    },
    { immediate: true },
  )

  return snippets
}
