<script setup lang="ts">
// Headers as a two-column table, read-only. Order is the order the
// server keeps, which is the order they arrived in.
import type { Header } from '../api/types'

defineProps<{ headers: Header[] }>()
</script>

<template>
  <div v-if="headers.length" class="table-wrapper">
    <table class="headers-table">
      <tbody>
        <tr v-for="(h, i) in headers" :key="i">
          <td class="header-name">
            <code>{{ h.name }}</code>
          </td>
          <td class="header-value">{{ h.value }}</td>
        </tr>
      </tbody>
    </table>
  </div>
  <p v-else class="viewer-muted">No headers.</p>
</template>
