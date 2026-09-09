<script setup lang="ts">
// One row of a reader list: method, host, a status pill, the path, and
// a meta line. The log, the intercept queue and the sender history all
// list exchanges this way.
import MethodBadge from './MethodBadge.vue'
import StatusBadge from './StatusBadge.vue'
import { shortURL } from '../composables/http'
import { formatClock } from '../composables/formatDate'

withDefaults(
  defineProps<{
    method: string
    url: string
    // When it happened, shown as a clock in the meta line.
    time: string
    selected?: boolean
    // The status pill. Omit both to show no pill at all.
    statusCode?: number
    statusReason?: string
    // A pill in place of the status, for a row that has none yet - a
    // request waiting in the queue. Uses the badge classes.
    badge?: string
    badgeKind?: 'warning' | 'info' | 'neutral'
    // A color mark, one of the palette names, as a stripe on the left.
    color?: string
    // Tags, shown as chips after the pill, three at most.
    tags?: string[]
  }>(),
  {
    selected: false,
    statusCode: undefined,
    statusReason: '',
    badge: '',
    badgeKind: 'neutral',
    color: '',
    tags: () => [],
  },
)

defineEmits<{ (e: 'select'): void }>()
</script>

<template>
  <button type="button" class="log-row" :class="{ selected }" @click="$emit('select')">
    <span v-if="color" class="log-row-stripe" :class="`mark-${color}`" aria-hidden="true"></span>
    <div class="log-row-top">
      <MethodBadge :method="method" />
      <span class="log-host truncate">{{ shortURL(url).host }}</span>
      <template v-if="tags.length">
        <span v-for="t in tags.slice(0, 3)" :key="t" class="chip chip-sm">{{ t }}</span>
        <span v-if="tags.length > 3" class="chip chip-sm">+{{ tags.length - 3 }}</span>
      </template>
      <span v-if="badge" class="badge" :class="`badge-${badgeKind}`">{{ badge }}</span>
      <StatusBadge v-else :code="statusCode" :reason="statusReason" />
    </div>
    <div class="log-path truncate" :title="url">{{ shortURL(url).path || '/' }}</div>
    <div class="log-row-meta">
      <span>{{ formatClock(time) }}</span>
      <slot name="meta"></slot>
    </div>
  </button>
</template>
