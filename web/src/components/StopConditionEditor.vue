<script setup lang="ts">
// The stop conditions of an automation job: one row per condition, a
// blank row at the end to add to, and how the rows combine. The model is
// the list the API takes. The fields and the comparisons each admits are
// the lists the server validates against, so a row that can be built here
// is a row the server accepts.
import { computed } from 'vue'
import type { AutomationStopCondition } from '../api/types'
import { getIcon } from '../layouts/icons'

const model = defineModel<AutomationStopCondition[]>({ default: () => [] })
const match = defineModel<'any' | 'all'>('match', { default: 'any' })

interface Option {
  value: string
  label: string
}

const FIELDS: Option[] = [
  { value: 'status', label: 'Status code' },
  { value: 'header', label: 'Response header' },
  { value: 'body', label: 'Response body' },
]

const OPS: Record<string, Option[]> = {
  status: [
    { value: 'eq', label: 'equals' },
    { value: 'ne', label: 'not equals' },
    { value: 'gte', label: 'at least' },
    { value: 'lte', label: 'at most' },
  ],
  header: [
    { value: 'exists', label: 'exists' },
    { value: 'contains', label: 'contains' },
    { value: 'eq', label: 'equals' },
  ],
  body: [
    { value: 'contains', label: 'contains' },
    { value: 'not_contains', label: 'does not contain' },
    { value: 'eq', label: 'equals' },
  ],
}

// The blank row is rendered, not stored: choosing its field adds a
// condition. It offers no value box until then, so nothing typed there
// is lost.
const rows = computed<AutomationStopCondition[]>(() => [
  ...model.value,
  { field: '', op: '', header: '', value: '' },
])

function update(i: number, patch: Partial<AutomationStopCondition>) {
  const next = rows.value.map((c) => ({ ...c }))
  const row = next[i]
  if (!row) return
  Object.assign(row, patch)
  if (patch.field !== undefined) {
    row.op = OPS[patch.field]?.[0]?.value ?? ''
    row.header = ''
  }
  model.value = next.filter((c, at) => at < next.length - 1 || c.field !== '')
}

function remove(i: number) {
  model.value = model.value.filter((_, at) => at !== i)
}

function value(e: Event) {
  return (e.target as HTMLInputElement | HTMLSelectElement).value
}
</script>

<template>
  <div>
    <div class="stop-editor-head">
      <label class="form-label">Stop conditions</label>
      <select v-model="match" class="form-select stop-editor-match">
        <option value="any">stop when any matches</option>
        <option value="all">stop when all match</option>
      </select>
    </div>
    <p class="form-hint mb-2">
      End the run early when a response matches. Leave empty to run every payload.
    </p>
    <div v-for="(c, i) in rows" :key="i" class="stop-editor-row">
      <select class="form-select" :value="c.field" @change="update(i, { field: value($event) })">
        <option value="" disabled>Field...</option>
        <option v-for="f in FIELDS" :key="f.value" :value="f.value">{{ f.label }}</option>
      </select>
      <template v-if="c.field">
        <select class="form-select" :value="c.op" @change="update(i, { op: value($event) })">
          <option v-for="o in OPS[c.field] ?? []" :key="o.value" :value="o.value">
            {{ o.label }}
          </option>
        </select>
        <input
          v-if="c.field === 'header'"
          class="form-input code-font"
          placeholder="Header"
          :value="c.header"
          spellcheck="false"
          @input="update(i, { header: value($event) })"
        />
        <input
          v-if="c.op !== 'exists'"
          class="form-input code-font"
          placeholder="Value"
          :value="c.value"
          spellcheck="false"
          @input="update(i, { value: value($event) })"
        />
      </template>
      <button
        v-if="i < model.length"
        type="button"
        class="icon-btn"
        title="Remove condition"
        aria-label="Remove condition"
        @click="remove(i)"
      >
        <span class="glyph" aria-hidden="true" v-html="getIcon('x')"></span>
      </button>
      <span v-else class="icon-btn-space"></span>
    </div>
  </div>
</template>
