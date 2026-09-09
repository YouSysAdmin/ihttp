<script setup lang="ts">
// The method chip. Fixed width so a column of them lines up, and the
// colour says read or write at a glance.
import { computed } from 'vue'
import { methodClass } from '../composables/http'

const props = defineProps<{ method: string }>()

const cls = computed(() => ['method', methodClass(props.method)])
</script>

<template>
  <span :class="cls">{{ method.toUpperCase() }}</span>
</template>
