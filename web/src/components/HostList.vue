<script setup lang="ts">
// The log grouped by host, above the list it narrows: which hosts this
// project has actually talked to, how much, and how much of it failed.
//
// Two things a person does from here. Clicking a host writes
// `req.host = ...` into the filter, the way the color menu writes
// `req.color`. Muting one hides it from the view while it goes on being
// captured - the opposite of the ignore filter, which never writes it
// down at all.
//
// A row is a div holding two buttons: a control inside a control is
// invalid and reachable by neither keyboard nor touch.
import type { HostCount } from '../api/types'

defineProps<{
  hosts: HostCount[]
  loading: boolean
  // The host the filter currently names, so the row can show it.
  activeHost: string
  // Whether muted hosts are being shown, which changes what the note
  // at the bottom needs to say.
  includeMuted: boolean
}>()

const emit = defineEmits<{
  (e: 'pick', host: string): void
  (e: 'mute', host: string, muted: boolean): void
  (e: 'refresh'): void
}>()
</script>

<template>
  <div class="host-list">
    <div class="host-list-head">
      <span class="host-list-title">Hosts</span>
      <button
        type="button"
        class="link-button host-list-refresh"
        :disabled="loading"
        title="Count the log again"
        @click="emit('refresh')"
      >
        {{ loading ? 'Counting...' : 'Refresh' }}
      </button>
    </div>

    <p v-if="!loading && hosts.length === 0" class="host-list-empty">Nothing logged yet.</p>

    <div
      v-for="h in hosts"
      :key="h.host"
      class="host-row"
      :class="{ active: h.host === activeHost, muted: h.muted }"
    >
      <button
        type="button"
        class="host-pick"
        :title="h.host === activeHost ? 'Take this host out of the filter' : 'Show only this host'"
        @click="emit('pick', h.host)"
      >
        <span class="host-name code-font">{{ h.host }}</span>
        <span v-if="h.errors" class="host-errors" :title="h.errors + ' answered with a 5xx'">
          {{ h.errors }}
        </span>
        <span class="host-count">{{ h.count }}</span>
      </button>
      <button
        type="button"
        class="host-mute"
        :title="
          h.muted ? 'Show this host again' : 'Hide this host from the list, keep capturing it'
        "
        @click="emit('mute', h.host, !h.muted)"
      >
        {{ h.muted ? 'unmute' : 'mute' }}
      </button>
    </div>

    <p v-if="hosts.some((h) => h.muted) && !includeMuted" class="host-list-note">
      A muted host is still captured and still exported. Tick "Show muted hosts" in Filter options
      to see its entries again.
    </p>
  </div>
</template>
