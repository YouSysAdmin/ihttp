<script setup lang="ts">
// A button that opens a menu under itself. The items are the slot, each
// a `.menu-item` button, and any click inside closes the menu. Outside
// clicks and Escape close it too.
import { onBeforeUnmount, onMounted, ref } from 'vue'

withDefaults(
  defineProps<{
    label?: string
    // Any of the stylesheet's button classes.
    variant?: string
    // Where the menu hangs from the button.
    align?: 'left' | 'right'
    // The button's accessible name, when its content is not text.
    buttonLabel?: string
  }>(),
  { label: 'Actions', variant: 'btn btn-secondary btn-sm', align: 'right', buttonLabel: '' },
)

const open = ref(false)
const root = ref<HTMLElement | null>(null)

function onDocumentDown(e: MouseEvent) {
  if (!root.value?.contains(e.target as Node)) open.value = false
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('mousedown', onDocumentDown)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocumentDown)
  document.removeEventListener('keydown', onKey)
})
</script>

<template>
  <div ref="root" class="menu">
    <button
      type="button"
      :class="variant"
      :aria-expanded="open"
      aria-haspopup="menu"
      :aria-label="buttonLabel || undefined"
      :title="buttonLabel || undefined"
      @click="open = !open"
    >
      <slot name="button">{{ label }}</slot>
      <span class="menu-caret" aria-hidden="true">&#9662;</span>
    </button>
    <div
      v-if="open"
      class="menu-list"
      :class="`menu-list--${align}`"
      role="menu"
      @click="open = false"
    >
      <slot></slot>
    </div>
  </div>
</template>
