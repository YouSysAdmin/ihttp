import { defineStore } from 'pinia'
import { computed, ref, watchEffect } from 'vue'

// What the operator chose, which is not the same as what is on screen.
export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'ihttp_theme'
const MODES: ThemeMode[] = ['light', 'dark', 'system']

// The colour mode: the operator's choice, what it resolves to, and the
// `data-theme` attribute the stylesheet reads. Three modes rather than
// a boolean, because "follow the system" is a standing instruction.
export const useThemeStore = defineStore('theme', () => {
  const query = window.matchMedia('(prefers-color-scheme: dark)')

  // The OS preference in a ref: `query.matches` is a plain property Vue
  // cannot track, so a computed reading it would go stale on a flip.
  const systemDark = ref(query.matches)
  query.addEventListener('change', (e) => {
    systemDark.value = e.matches
  })

  // Storage may be blocked, in which case the choice lives for the tab.
  let saved: ThemeMode | null = null
  try {
    saved = localStorage.getItem(STORAGE_KEY) as ThemeMode | null
  } catch {
    saved = null
  }
  const mode = ref<ThemeMode>(saved && MODES.includes(saved) ? saved : 'system')

  // What is actually on screen once `system` is resolved.
  const isDark = computed(() =>
    mode.value === 'system' ? systemDark.value : mode.value === 'dark',
  )

  // One effect for both jobs: it runs on creation, so the attribute is
  // correct before first paint, and re-runs on the choice or the OS.
  watchEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, mode.value)
    } catch {
      // Not remembered, still applied.
    }
    document.documentElement.setAttribute('data-theme', isDark.value ? 'dark' : 'light')
  })

  // Pick a mode explicitly - the three-way control in the sidebar.
  function setMode(next: ThemeMode) {
    mode.value = next
  }

  // Flip to the opposite of what is showing, as an explicit choice.
  function toggle() {
    mode.value = isDark.value ? 'light' : 'dark'
  }

  return { mode, isDark, setMode, toggle }
})
