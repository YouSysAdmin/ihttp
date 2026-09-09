<script setup lang="ts">
// The row of color marks, Any color first. Used inside ColorMenu, where
// a dropdown keeps it out of the filter bar's room, and directly in a
// dialog, where there is room to show it open.
import { MARK_COLORS } from '../api/types'

defineProps<{
  // The chosen color, '' for none.
  modelValue: string
}>()

const emit = defineEmits<{ (e: 'update:modelValue', color: string): void }>()
</script>

<template>
  <div class="color-menu-row">
    <button
      type="button"
      class="mark-swatch mark-none"
      :class="{ selected: !modelValue }"
      role="menuitemradio"
      :aria-checked="!modelValue"
      title="Any color"
      aria-label="Any color"
      @click="emit('update:modelValue', '')"
    ></button>
    <button
      v-for="c in MARK_COLORS"
      :key="c"
      type="button"
      class="mark-swatch"
      :class="[`mark-${c}`, { selected: modelValue === c }]"
      role="menuitemradio"
      :aria-checked="modelValue === c"
      :title="c"
      :aria-label="c"
      @click="emit('update:modelValue', c)"
    ></button>
  </div>
</template>
