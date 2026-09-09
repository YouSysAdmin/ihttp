import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { useEventsStore } from './events'
import { useProjectStore } from './project'

// One side of a comparison: where an exchange lives and how to name it.
export interface PinnedExchange {
  kind: 'log' | 'sender' | 'automation'
  id: string
  // The job an automation result belongs to.
  jobId?: string
  label: string
}

// The exchange pinned as A. Comparing is two clicks on two pages, so the
// first pick has to survive navigation. It lives in sessionStorage per
// project and is dropped when the project changes, since an id means
// nothing in another one.
export const useCompareStore = defineStore('compare', () => {
  const projects = useProjectStore()
  const events = useEventsStore()

  const pinned = ref<PinnedExchange | null>(null)

  function key() {
    return 'ihttp_compare_' + (projects.active?.id ?? '')
  }

  function load() {
    try {
      const raw = sessionStorage.getItem(key())
      pinned.value = raw ? (JSON.parse(raw) as PinnedExchange) : null
    } catch {
      pinned.value = null
    }
  }

  function persist() {
    try {
      if (pinned.value) sessionStorage.setItem(key(), JSON.stringify(pinned.value))
      else sessionStorage.removeItem(key())
    } catch {
      // Storage refused: the pin lives for this page only.
    }
  }

  function pin(x: PinnedExchange) {
    pinned.value = x
    persist()
  }

  function unpin() {
    pinned.value = null
    persist()
  }

  function isPinned(x: Pick<PinnedExchange, 'kind' | 'id'>) {
    return pinned.value?.kind === x.kind && pinned.value?.id === x.id
  }

  // The route that compares the pin with b.
  function hrefFor(b: PinnedExchange) {
    if (!pinned.value) return ''
    return `/compare?a=${encodeRef(pinned.value)}&b=${encodeRef(b)}`
  }

  load()
  watch(() => projects.active?.id, load)

  return { pinned, pin, unpin, isPinned, hrefFor }
})

// `kind:id`, or `automation:jobId:id`.
export function encodeRef(x: PinnedExchange): string {
  return x.kind === 'automation' ? `automation:${x.jobId}:${x.id}` : `${x.kind}:${x.id}`
}

export function decodeRef(s: string): Omit<PinnedExchange, 'label'> | null {
  const parts = s.split(':')
  if (parts[0] === 'automation' && parts.length === 3) {
    return { kind: 'automation', jobId: parts[1]!, id: parts[2]! }
  }
  if ((parts[0] === 'log' || parts[0] === 'sender') && parts.length === 2) {
    return { kind: parts[0], id: parts[1]! }
  }
  return null
}
