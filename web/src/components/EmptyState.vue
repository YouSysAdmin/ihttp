<script setup lang="ts">
// The "there is nothing here yet" block. Text goes in the props, the
// default slot is for markup. The heading is optional: inside a card
// that already carries the title, a second one repeats it.
withDefaults(
  defineProps<{
    title?: string
    text?: string
  }>(),
  { title: '', text: '' },
)
</script>

<template>
  <div class="empty-state">
    <h3 v-if="title">{{ title }}</h3>
    <slot>
      <p v-if="text">{{ text }}</p>
    </slot>
  </div>
</template>
