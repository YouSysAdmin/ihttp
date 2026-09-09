<script setup lang="ts">
// The filter query box every list has, with the syntax one click away.
// Submitted, not typed: a query is evaluated against every entry on
// the server, and a request per keystroke over a large log is a
// request per keystroke too many.
import { ref, watch } from 'vue'
import BaseModal from './BaseModal.vue'
import FilterHelp from './FilterHelp.vue'

const props = withDefaults(
  defineProps<{
    // The box's prompt. The default is the filter query language. A list
    // that only matches a substring passes its own.
    placeholder?: string
    // Whether the filter-syntax help is offered. Off for a plain search.
    help?: boolean
    // The submit button's label. "Filter" for the query language, a
    // plain search says "Search".
    submitLabel?: string
  }>(),
  {
    placeholder: 'Filter, e.g. req.method = POST AND res.statusCode >= 400',
    help: true,
    submitLabel: 'Filter',
  },
)

const model = defineModel<string>({ default: '' })

const emit = defineEmits<{ (e: 'submit'): void }>()

const draft = ref(model.value)

// A filter recalled from storage, or cleared by the page, lands in the
// box too.
watch(model, (next) => (draft.value = next))
const showHelp = ref(false)

function submit() {
  model.value = draft.value.trim()
  emit('submit')
}

function clear() {
  draft.value = ''
  submit()
}
</script>

<template>
  <form class="search-bar" @submit.prevent="submit">
    <input
      v-model="draft"
      class="form-input code-font"
      type="search"
      :placeholder="props.placeholder"
      spellcheck="false"
    />
    <button type="submit" class="btn btn-secondary">{{ props.submitLabel }}</button>
    <button v-if="model" type="button" class="btn btn-secondary" @click="clear">Clear</button>
    <button
      v-if="props.help"
      type="button"
      class="btn btn-secondary"
      title="Filter syntax"
      @click="showHelp = true"
    >
      ?
    </button>

    <BaseModal v-if="showHelp" title="Filter syntax" size="modal-w720" @close="showHelp = false">
      <div class="help-box help-box--full">
        <FilterHelp subject="log" />
      </div>
      <template #footer>
        <button type="button" class="btn btn-primary" @click="showHelp = false">Close</button>
      </template>
    </BaseModal>
  </form>
</template>
