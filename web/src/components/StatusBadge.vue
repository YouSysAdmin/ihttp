<script setup lang="ts">
// The HTTP status pill. The colour comes from composables/http, which is
// the one place that knows what a status code means. No code yet - a
// request still in flight - reads as a quiet dash.
import { computed } from 'vue'
import { statusClass } from '../composables/http'

const props = defineProps<{ code?: number; reason?: string }>()

const cls = computed(() => ['badge', statusClass(props.code)])
const label = computed(() => (props.code ? String(props.code) : 'pending'))
</script>

<template>
  <span :class="cls" :title="reason || ''">{{ label }}</span>
</template>
