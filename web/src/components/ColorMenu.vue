<script setup lang="ts">
// A color mark as a dropdown: one button showing the chosen swatch, the
// swatch row under it. Used where a row of swatches would take the
// search box's room. In a dialog use ColorSwatches directly.
import ColorSwatches from './ColorSwatches.vue'
import DropdownMenu from './DropdownMenu.vue'

withDefaults(
  defineProps<{
    // The chosen color, '' for none.
    modelValue: string
    // The button's accessible name.
    label?: string
  }>(),
  { label: 'Filter by color' },
)

const emit = defineEmits<{ (e: 'update:modelValue', color: string): void }>()
</script>

<template>
  <DropdownMenu variant="btn btn-secondary" :button-label="label">
    <template #button>
      <span
        class="mark-swatch color-menu-dot"
        :class="modelValue ? `mark-${modelValue}` : 'mark-none'"
        aria-hidden="true"
      ></span>
    </template>
    <ColorSwatches
      :model-value="modelValue"
      @update:model-value="emit('update:modelValue', $event)"
    />
  </DropdownMenu>
</template>
