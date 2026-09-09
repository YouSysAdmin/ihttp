<script setup lang="ts">
// What is slowest, and what is largest, over the WHOLE log rather than
// over the page in front of you.
//
// A report rather than a sort. Sorting the list would need an index the
// log does not have, and sorting one page of it would lie the moment
// there were two pages. This asks the server for the ranking, shows it,
// and clicking a row opens that entry.
import { onMounted, ref, watch } from 'vue'
import { reqlogsApi, type LogSelection } from '../api/reqlogs'
import { apiErrorMessage } from '../api/client'
import type { LogSummary } from '../api/types'
import { humanSize } from '../composables/humanSize'
import { formatClock } from '../composables/formatDate'
import BaseModal from './BaseModal.vue'
import MethodBadge from './MethodBadge.vue'
import StatusBadge from './StatusBadge.vue'
import TabStrip, { type TabItem } from './TabStrip.vue'

const props = defineProps<{
  // What the log is currently narrowed to, so the ranking answers the
  // same question the list does.
  selection: LogSelection
  // Whether that narrowing is anything, for the line that says so.
  filtered: boolean
  by: 'duration' | 'size'
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'open', id: string): void
}>()

const TABS: TabItem[] = [
  { id: 'duration', label: 'Slowest' },
  { id: 'size', label: 'Largest' },
]

const by = ref<'duration' | 'size'>(props.by)
const entries = ref<LogSummary[]>([])
const loading = ref(true)
const error = ref('')

async function load() {
  loading.value = true
  error.value = ''

  try {
    const res = await reqlogsApi.top({ ...props.selection, by: by.value, limit: 20 })
    entries.value = res.data.entries ?? []
  } catch (e) {
    error.value = apiErrorMessage(e, 'Could not rank the log')
    entries.value = []
  } finally {
    loading.value = false
  }
}

function score(e: LogSummary): string {
  return by.value === 'size' ? humanSize(e.response_size) : `${e.duration_ms ?? 0} ms`
}

onMounted(load)
watch(by, load)
</script>

<template>
  <BaseModal title="Top of the log" size="modal-w720" @close="emit('close')">
    <TabStrip v-model="by" :tabs="TABS" class="mb-3" />

    <p class="text-sm text-muted mb-3">
      <template v-if="filtered">The current filter, ranked - twenty at most.</template>
      <template v-else>The whole log, ranked - twenty at most.</template>
      An exchange still waiting for its response is not ranked, and a muted host still counts: a
      ranking is a question about the log, not about the list.
    </p>

    <p v-if="loading" class="viewer-muted">Ranking...</p>
    <p v-else-if="error" class="viewer-muted">{{ error }}</p>
    <p v-else-if="entries.length === 0" class="viewer-muted">Nothing with a response yet.</p>

    <div v-else class="table-wrapper">
      <table>
        <tbody>
          <tr v-for="e in entries" :key="e.id" class="top-row" @click="emit('open', e.id)">
            <td class="top-score code-font">{{ score(e) }}</td>
            <td><MethodBadge :method="e.method" /></td>
            <td class="top-url code-font" :title="e.url">{{ e.url }}</td>
            <td><StatusBadge :code="e.status_code" :reason="e.status" /></td>
            <td class="text-xs text-muted">{{ formatClock(e.created_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>
