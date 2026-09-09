<script setup lang="ts">
// A remark that stays on the page, as opposed to a toast that goes away.
// The heading is a prop, the body is the slot. The severities are the
// badge vocabulary and read from the same tokens.
withDefaults(
  defineProps<{
    kind?: 'info' | 'success' | 'warning' | 'danger'
    // A line above the body, in the notice's own colour.
    title?: string
  }>(),
  { kind: 'info', title: '' },
)
</script>

<template>
  <div class="notice" :class="`notice-${kind}`">
    <p v-if="title" class="notice-head">{{ title }}</p>
    <slot />
  </div>
</template>
