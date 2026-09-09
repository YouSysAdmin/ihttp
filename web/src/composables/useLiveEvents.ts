// Subscribe a page to the event stream for as long as it is mounted.
// The handlers are registered on mount and dropped on unmount, so a
// page left behind cannot keep reloading itself.
import { onBeforeUnmount, onMounted } from 'vue'
import { useEventsStore, type EventListener } from '../stores/events'

export function useLiveEvents(handlers: Record<string, EventListener>) {
  const events = useEventsStore()
  const unsubscribe: (() => void)[] = []

  onMounted(() => {
    for (const [type, fn] of Object.entries(handlers)) {
      unsubscribe.push(events.on(type, fn))
    }
  })

  onBeforeUnmount(() => unsubscribe.forEach((fn) => fn()))
}
