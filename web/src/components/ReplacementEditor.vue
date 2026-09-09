<script setup lang="ts">
// An editable list of regular-expression replacements: one row per
// pattern and its replacement, a blank row at the end to add to, and a
// remove control per row. The model is the list the API takes.
import { computed } from 'vue'
import type { Replacement } from '../api/types'
import { getIcon } from '../layouts/icons'

const model = defineModel<Replacement[]>({ default: () => [] })

// The blank row is rendered, not stored: typing into it adds a replacement.
const rows = computed(() => [...model.value, { pattern: '', replace: '' }])

function update(i: number, field: keyof Replacement, v: string) {
  const next = rows.value.map((r) => ({ ...r }))
  next[i]![field] = v
  model.value = next.filter((r, at) => at < next.length - 1 || r.pattern !== '' || r.replace !== '')
}

function remove(i: number) {
  model.value = model.value.filter((_, at) => at !== i)
}
</script>

<template>
  <div class="header-editor">
    <div v-for="(r, i) in rows" :key="i" class="header-row replacement-row">
      <input
        class="form-input code-font"
        :value="r.pattern"
        placeholder="Pattern, e.g. (user)_(\d+)"
        spellcheck="false"
        @input="update(i, 'pattern', ($event.target as HTMLInputElement).value)"
      />
      <input
        class="form-input code-font"
        :value="r.replace"
        placeholder="Replace with, $1 $2 for groups"
        spellcheck="false"
        @input="update(i, 'replace', ($event.target as HTMLInputElement).value)"
      />
      <button
        v-if="i < model.length"
        type="button"
        class="icon-btn"
        title="Remove replacement"
        aria-label="Remove replacement"
        @click="remove(i)"
      >
        <span class="glyph" aria-hidden="true" v-html="getIcon('x')"></span>
      </button>
      <span v-else class="icon-btn-space"></span>
    </div>
  </div>
</template>
