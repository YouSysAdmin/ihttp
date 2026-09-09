<script setup lang="ts">
// What the keys do, read off the registry rather than a table someone
// has to remember to update. A page that binds nothing shows nothing,
// which is the truth about that page.
import { computed } from 'vue'
import { keyLabel, shortcuts, type Shortcut } from '../composables/useShortcuts'
import BaseModal from './BaseModal.vue'

const emit = defineEmits<{ (e: 'close'): void }>()

// The modifier as the reader's keyboard spells it, for the hint below.
const modLabel = keyLabel('mod+').replace(/\+$/, '')

const groups = computed(() => {
  const out = new Map<string, Shortcut[]>()

  for (const s of shortcuts().value) {
    if (s.hidden) continue
    if (s.when && !s.when()) continue

    const name = s.group ?? 'This page'
    out.set(name, [...(out.get(name) ?? []), s])
  }

  return [...out.entries()]
})

function keysOf(s: Shortcut): string[] {
  return (Array.isArray(s.keys) ? s.keys : [s.keys]).map(keyLabel)
}
</script>

<template>
  <BaseModal title="Keyboard shortcuts" size="modal-w440" @close="emit('close')">
    <p v-if="groups.length === 0" class="viewer-muted">This page has none.</p>

    <template v-for="[name, list] in groups" :key="name">
      <h3 class="dialog-section">{{ name }}</h3>
      <dl class="shortcut-list">
        <template v-for="(s, i) in list" :key="i">
          <dt>
            <kbd v-for="k in keysOf(s)" :key="k">{{ k }}</kbd>
          </dt>
          <dd>{{ s.label }}</dd>
        </template>
      </dl>
    </template>

    <p class="form-hint mt-3">
      A bare key does nothing while you are typing in a field or an editor. A key with
      {{ modLabel }} works there too. Nothing fires while a dialog is open.
    </p>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>
