import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { interceptApi } from '../api/intercept'
import type { InterceptItem } from '../api/types'
import { useEventsStore } from './events'

// What is waiting in the intercept queue, kept current by the event
// stream. A store because two places read it: the page, and the badge
// in the navigation that says how many are waiting.
export const useInterceptStore = defineStore('intercept', () => {
  const items = ref<InterceptItem[]>([])
  const loaded = ref(false)

  const count = computed(() => items.value.length)

  async function fetchAll() {
    const res = await interceptApi.items()
    items.value = res.data.items ?? []
    loaded.value = true
  }

  function upsert(item: InterceptItem) {
    const at = items.value.findIndex((x) => x.id === item.id && x.kind === item.kind)
    if (at === -1) items.value = [...items.value, item]
    else items.value = items.value.map((x, i) => (i === at ? item : x))
  }

  function remove(id: string, kind: string) {
    items.value = items.value.filter((x) => !(x.id === id && x.kind === kind))
  }

  let wired = false

  // Subscribe once. Called from the layout, which is always mounted.
  function wire() {
    if (wired) return
    wired = true

    const events = useEventsStore()
    events.on('intercept.request', (item: InterceptItem) => upsert(item))
    events.on('intercept.response', (item: InterceptItem) => upsert(item))
    events.on('intercept.done', (d: { id: string; kind: string }) => remove(d.id, d.kind))
    // A reconnect may have missed a done event, so resync on every hello.
    events.on('hello', () => void fetchAll())
    events.on('project.closed', () => void fetchAll())

    void fetchAll()
  }

  return { items, count, loaded, fetchAll, wire }
})
