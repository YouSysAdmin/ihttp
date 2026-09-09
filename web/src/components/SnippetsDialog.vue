<script setup lang="ts">
// One request, spelled every way the tool knows, in a dialog rather
// than as six menu items: the point is to READ the command before
// copying it, and six of anything in a menu is a wall.
//
// The server writes all six in one answer, so switching between them
// costs nothing and there is nothing to load.
import { computed, ref } from 'vue'
import type { Snippets } from '../api/reqlogs'
import BaseModal from './BaseModal.vue'
import CopyButton from './CopyButton.vue'
import TabStrip, { type TabItem } from './TabStrip.vue'

const props = defineProps<{
  // Null until they have loaded.
  snippets: Snippets | null
  // What the snippets are of, for the dialog's own title line.
  label?: string
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const TABS: TabItem[] = [
  { id: 'curl', label: 'curl' },
  { id: 'httpie', label: 'HTTPie' },
  { id: 'fetch', label: 'fetch()' },
  { id: 'python', label: 'Python' },
  { id: 'go', label: 'Go' },
  { id: 'powershell', label: 'PowerShell' },
]

// Remembered for the session: someone who works in Python wants Python
// the next time too, and it costs a string.
const KEY = 'ihttp_snippet_lang'
const lang = ref<string>(localStorage.getItem(KEY) ?? 'curl')

function pick(id: string) {
  lang.value = id

  try {
    localStorage.setItem(KEY, id)
  } catch {
    // A browser with storage off still switches, it just forgets.
  }
}

const text = computed(() => {
  const s = props.snippets
  if (!s) return ''

  switch (lang.value) {
    case 'httpie':
      return s.httpie
    case 'fetch':
      return s.fetch
    case 'python':
      return s.python
    case 'go':
      return s.go
    case 'powershell':
      return s.powershell
    default:
      return s.curl
  }
})

// What each one needs to be true before it runs, when there is
// something worth saying.
const NOTES: Record<string, string> = {
  fetch: 'A browser sets Content-Length and Accept-Encoding itself, so those are left out.',
  python: 'Needs requests: pip install requests.',
  go: 'A whole program: paste it into main.go and go run it.',
  powershell:
    'Content-Type and User-Agent go in their own parameters, which is what PowerShell accepts.',
  httpie:
    '--ignore-stdin is there because httpie reads a body from stdin when it is not a terminal.',
}

const note = computed(() => NOTES[lang.value] ?? '')
</script>

<template>
  <BaseModal title="Copy as" size="modal-w720" @close="emit('close')">
    <p v-if="label" class="text-sm text-muted mb-3 truncate">{{ label }}</p>

    <TabStrip :model-value="lang" :tabs="TABS" class="mb-3" @update:model-value="pick" />

    <p v-if="!snippets" class="viewer-muted">Loading...</p>
    <pre v-else class="raw-text code-font snippet-text">{{ text }}</pre>

    <p v-if="note" class="form-hint mt-2">{{ note }}</p>

    <template #footer>
      <CopyButton :value="text" :disabled="!snippets" variant="btn btn-primary" />
      <button type="button" class="btn btn-secondary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>
