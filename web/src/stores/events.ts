import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useProjectStore } from './project'
import type { Project, Settings } from '../api/types'

// A listener for one event type. The payload is whatever the server sent.
export type EventListener = (data: any) => void

// The one live connection to the server.
//
// Server-sent events rather than polling: the log page shows a request
// the moment the proxy sees it, and the intercept badge counts what is
// waiting without asking every second. The browser reconnects on its
// own, and the `hello` event that opens every connection carries which
// project is open so a tab that was away resyncs.
//
// The project store is fed from here directly, because every page
// depends on it. Everything else subscribes to the event types it
// cares about and is told nothing more.
export const useEventsStore = defineStore('events', () => {
  const connected = ref(false)
  const listeners = new Map<string, Set<EventListener>>()
  let source: EventSource | null = null

  function connect() {
    if (source) return

    source = new EventSource('/api/events')
    source.onopen = () => (connected.value = true)
    source.onerror = () => (connected.value = false)

    const projects = useProjectStore()

    source.addEventListener('hello', (e) => {
      const data = JSON.parse((e as MessageEvent).data) as { active_project_id: string }
      // What the store believes may be stale if the server restarted.
      if (projects.loaded && (projects.active?.id ?? '') !== data.active_project_id) {
        void projects.fetchAll()
      }
      dispatch('hello', data)
    })

    on('project.opened', (p: Project) => projects.setActive(p))
    on('project.closed', () => projects.setActive(null))
    on('project.settings', (p: Project) => projects.setSettings(p.settings as Settings))
    on('project.created', (p: Project) => projects.onCreated(p))
    on('project.deleted', (d: { id: string }) => projects.onDeleted(d.id))

    for (const type of KNOWN_TYPES) {
      source.addEventListener(type, (e) => {
        const raw = (e as MessageEvent).data
        dispatch(type, raw ? JSON.parse(raw) : null)
      })
    }
  }

  function dispatch(type: string, data: any) {
    listeners.get(type)?.forEach((fn) => fn(data))
  }

  // Subscribe to one event type. Returns the unsubscribe.
  function on(type: string, fn: EventListener): () => void {
    if (!listeners.has(type)) listeners.set(type, new Set())
    listeners.get(type)!.add(fn)
    return () => listeners.get(type)?.delete(fn)
  }

  return { connected, connect, on }
})

// Every type the server publishes. EventSource only delivers named
// events that were listened for, so the list has to be spelled out.
const KNOWN_TYPES = [
  'project.created',
  'project.opened',
  'project.closed',
  'project.settings',
  'project.deleted',
  'reqlog.updated',
  'reqlog.deleted',
  'reqlog.message',
  'reqlog.request',
  'reqlog.imported',
  'reqlog.response',
  'reqlog.cleared',
  'intercept.request',
  'intercept.response',
  'intercept.done',
  'sender.saved',
  'sender.sent',
  'sender.deleted',
  'sender.cleared',
  'automation.job',
  'automation.result',
  'automation.deleted',
  'automation.cleared',
  'rules.captured',
]
