// Keyboard shortcuts, in one registry.
//
// Two rules decide everything here. FIRST, a bare key never fires
// while someone is typing: an input, a textarea, a select, anything
// contenteditable or a CodeMirror editor swallows it. A mod+ chord is
// deliberate and fires from inside a field too - the intercept editor
// is nothing but fields, so Cmd+Enter would otherwise never forward.
// An open dialog owns the keyboard and swallows everything. SECOND,
// what a page binds is what the shortcut sheet lists - the sheet reads
// this registry, so it cannot drift from what the keys do.
import { onBeforeUnmount, onMounted, ref } from 'vue'

export interface Shortcut {
  // The key as KeyboardEvent.key reports it, with an optional mod+
  // prefix for Ctrl on Windows and Linux, Cmd on a Mac: 'j',
  // 'ArrowDown', 'Enter', 'mod+Enter'. Several spellings of one action
  // are given as a list, and the sheet shows them together.
  keys: string | string[]

  // What it does, for the sheet. Written as an instruction: "Open the
  // selected entry".
  label: string

  // The heading it appears under in the sheet.
  group?: string

  // Not offered and not fired while this says no.
  when?: () => boolean

  run: (event: KeyboardEvent) => void

  // Left out of the sheet, for a key that is documented elsewhere or is
  // an alias nobody needs to read.
  hidden?: boolean
}

// The bindings of every page currently mounted, in registration order.
// A ref so the sheet re-renders when a page comes or goes.
const active = ref<Shortcut[]>([])

export function shortcuts() {
  return active
}

// typing says someone is writing: the key belongs to the field under
// the cursor.
function typing(event: KeyboardEvent): boolean {
  const target = event.target as HTMLElement | null

  return (
    !!target &&
    !!(target.closest('input, textarea, select, [contenteditable]') || target.closest('.cm-editor'))
  )
}

// inDialog says a dialog owns the keyboard.
function inDialog(): boolean {
  return !!document.querySelector('[role="dialog"]')
}

// matches compares one binding spelling against the event.
function matches(spelling: string, event: KeyboardEvent): boolean {
  const wantMod = spelling.startsWith('mod+')
  const key = wantMod ? spelling.slice(4) : spelling
  const hasMod = event.metaKey || event.ctrlKey

  if (wantMod !== hasMod) return false

  // A shortcut without a modifier is never an Alt or Shift combination:
  // those belong to the browser and to text entry.
  if (!wantMod && (event.altKey || (event.shiftKey && key.length === 1 && key !== '?'))) {
    return false
  }

  return event.key === key || (key.length === 1 && event.key.toLowerCase() === key.toLowerCase())
}

// useShortcuts binds for the life of the calling component.
export function useShortcuts(list: Shortcut[] | (() => Shortcut[])) {
  const own = () => (typeof list === 'function' ? list() : list)

  function onKeydown(event: KeyboardEvent) {
    if (event.isComposing || inDialog()) return

    const writing = typing(event)

    for (const s of own()) {
      if (s.when && !s.when()) continue

      // Over a field only a chord counts.
      const spellings = (Array.isArray(s.keys) ? s.keys : [s.keys]).filter(
        (k) => !writing || k.startsWith('mod+'),
      )
      if (!spellings.some((k) => matches(k, event))) continue

      // Prevented and stopped before running: a shortcut that scrolls
      // the page as well as moving the selection is worse than none,
      // and a chord that forwards a body must not also reach the editor
      // under the cursor, where Mod+Enter inserts a line and
      // Mod+Backspace deletes one.
      event.preventDefault()
      event.stopPropagation()
      s.run(event)

      return
    }
  }

  // Listened for in the capture phase, so a chord is decided here
  // before the field under the cursor sees it.
  onMounted(() => {
    active.value = [...active.value, ...own()]
    document.addEventListener('keydown', onKeydown, true)
  })

  onBeforeUnmount(() => {
    const mine = new Set(own())
    active.value = active.value.filter((s) => !mine.has(s))
    document.removeEventListener('keydown', onKeydown, true)
  })
}

// keyLabel renders a spelling for the sheet: mod+ becomes the symbol
// the reader's own keyboard has.
export function keyLabel(spelling: string): string {
  const mac = navigator.platform.toLowerCase().includes('mac')
  const mod = mac ? '⌘' : 'Ctrl+'

  const key = spelling.startsWith('mod+') ? mod + spelling.slice(4) : spelling

  return key
    .replace('ArrowDown', '↓')
    .replace('ArrowUp', '↑')
    .replace('ArrowLeft', '←')
    .replace('ArrowRight', '→')
    .replace('Backspace', '⌫')
    .replace('Escape', 'Esc')
}
