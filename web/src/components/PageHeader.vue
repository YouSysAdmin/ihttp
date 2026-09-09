<script setup lang="ts">
// The title row of a page, and the group its controls live in.
//
// `.page-header` is space-between and expects exactly two children, so a
// control added as a third one lands in the middle of the page - which
// is how a Refresh button ends up between the title and the button
// beside it. Rendering the group here means a slotted control cannot be
// anywhere else.
//
// The title slot is for pages that carry a subtitle under the heading.
// It has to be one element, or the count is three again.
withDefaults(defineProps<{ title?: string }>(), { title: '' })
</script>

<template>
  <div class="page-header">
    <!-- One element on the title side, see the rule above. -->
    <div>
      <slot name="title">
        <h1>{{ title }}</h1>
      </slot>
    </div>
    <div v-if="$slots.default" class="page-actions"><slot /></div>
  </div>
</template>
