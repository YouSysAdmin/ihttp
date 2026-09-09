// The selected item of a two-pane page lives in the URL: `base/:id`.
//
// One source of truth, so a deep link, the back button and a click in
// the list all arrive the same way. `replace` rather than `push`,
// because reading down a list is not twenty steps of history.
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export function useRouteSelection(base: string) {
  const route = useRoute()
  const router = useRouter()

  const selectedId = computed(() => (route.params.id ? String(route.params.id) : ''))

  function select(id: string) {
    if (id === selectedId.value) return
    router.replace(id ? `${base}/${id}` : base)
  }

  function clear() {
    if (selectedId.value) router.replace(base)
  }

  return { selectedId, select, clear }
}
