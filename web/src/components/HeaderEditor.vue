<script setup lang="ts">
// An editable header list: one row per header, a blank row at the end
// to add to, and a remove control per row. The model is the ordered
// list the API takes, so no conversion happens in the views.
import { computed } from 'vue'
import type { Header } from '../api/types'
import { getIcon } from '../layouts/icons'

const model = defineModel<Header[]>({ default: () => [] })

// namesOnly is for a list that names headers without giving them a value,
// such as the ones a rule removes.
const props = defineProps<{ namesOnly?: boolean }>()

// The blank row is rendered, not stored: typing into it adds a header.
const rows = computed(() => [...model.value, { name: '', value: '' }])

function update(i: number, field: keyof Header, v: string) {
  const next = rows.value.map((h) => ({ ...h }))
  next[i]![field] = v
  model.value = next.filter((h, at) => at < next.length - 1 || h.name !== '' || h.value !== '')
}

function remove(i: number) {
  model.value = model.value.filter((_, at) => at !== i)
}
</script>

<template>
  <div class="header-editor">
    <div
      v-for="(h, i) in rows"
      :key="i"
      class="header-row"
      :class="{ 'names-only': props.namesOnly }"
    >
      <input
        class="form-input code-font"
        :value="h.name"
        placeholder="Name"
        spellcheck="false"
        @input="update(i, 'name', ($event.target as HTMLInputElement).value)"
      />
      <input
        v-if="!props.namesOnly"
        class="form-input code-font"
        :value="h.value"
        placeholder="Value"
        spellcheck="false"
        @input="update(i, 'value', ($event.target as HTMLInputElement).value)"
      />
      <button
        v-if="i < model.length"
        type="button"
        class="icon-btn"
        title="Remove header"
        aria-label="Remove header"
        @click="remove(i)"
      >
        <span class="glyph" aria-hidden="true" v-html="getIcon('x')"></span>
      </button>
      <span v-else class="icon-btn-space"></span>
    </div>
  </div>
</template>
