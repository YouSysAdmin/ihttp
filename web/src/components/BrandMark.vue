<script setup lang="ts">
// The ihttp mark. The drawing lives in assets/brand-mark.svg and is
// pulled in through an external <use>, so the page carries a reference
// and not the geometry. `?no-inline` keeps it a file: a <use> may not
// point at a data: URI.
import markUrl from '../assets/brand-mark.svg?no-inline'

withDefaults(defineProps<{ size?: number | string }>(), { size: 20 })
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 32 32"
    aria-hidden="true"
    focusable="false"
    class="brand-mark"
  >
    <use :href="`${markUrl}#mark`" />
  </svg>
</template>

<style scoped>
.brand-mark {
  flex: 0 0 auto;
}
</style>
