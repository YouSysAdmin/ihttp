<script setup lang="ts">
// The marks on a log entry: tags as chips with a box to add one, a color
// swatch row, and a note behind a button. The page owns the entry and
// the save, this only says what the person changed.
import { computed, ref, useId } from 'vue'
import { MARK_COLORS } from '../api/types'
import FormDialog from './FormDialog.vue'
import FormField from './FormField.vue'

const props = withDefaults(
  defineProps<{
    tags: string[]
    note: string
    color: string
    // Tags seen in this project, offered while typing.
    suggestions?: string[]
    // The server's refusal of the last change, shown in the note dialog.
    error?: string
  }>(),
  { suggestions: () => [], error: '' },
)

const emit = defineEmits<{
  (e: 'update:tags', tags: string[]): void
  (e: 'update:color', color: string): void
  (e: 'update:note', note: string): void
}>()

const draft = ref('')
const showNote = ref(false)
const noteDraft = ref('')

const listId = useId()
const offered = computed(() =>
  props.suggestions.filter((s) => !props.tags.some((t) => t.toLowerCase() === s.toLowerCase())),
)

// Enter or a comma adds what was typed. The server trims and dedupes
// too, this only keeps an obviously empty chip from flashing.
function add() {
  const t = draft.value.replace(/,+$/, '').trim()
  draft.value = ''
  if (!t) return
  if (props.tags.some((x) => x.toLowerCase() === t.toLowerCase())) return
  emit('update:tags', [...props.tags, t])
}

function remove(tag: string) {
  emit(
    'update:tags',
    props.tags.filter((t) => t !== tag),
  )
}

function onInput(e: Event) {
  const v = (e.target as HTMLInputElement).value
  if (v.endsWith(',')) {
    draft.value = v
    add()
  }
}

function openNote() {
  noteDraft.value = props.note
  showNote.value = true
}

function saveNote() {
  emit('update:note', noteDraft.value)
  showNote.value = false
}
</script>

<template>
  <div class="entry-marks">
    <div class="entry-marks-tags">
      <span v-for="t in tags" :key="t" class="chip">
        {{ t }}
        <button type="button" class="chip-remove" :aria-label="`Remove ${t}`" @click="remove(t)">
          x
        </button>
      </span>
      <input
        v-model="draft"
        class="form-input entry-marks-input"
        :list="listId"
        placeholder="Add a tag"
        spellcheck="false"
        @keydown.enter.prevent="add"
        @blur="add"
        @input="onInput"
      />
      <datalist :id="listId">
        <option v-for="s in offered" :key="s" :value="s"></option>
      </datalist>
    </div>

    <div class="entry-marks-colors" role="radiogroup" aria-label="Color">
      <button
        type="button"
        class="mark-swatch mark-none"
        :class="{ selected: !color }"
        title="No color"
        aria-label="No color"
        @click="emit('update:color', '')"
      ></button>
      <button
        v-for="c in MARK_COLORS"
        :key="c"
        type="button"
        class="mark-swatch"
        :class="[`mark-${c}`, { selected: color === c }]"
        :title="c"
        :aria-label="c"
        @click="emit('update:color', c)"
      ></button>
    </div>

    <button type="button" class="btn btn-secondary btn-sm" @click="openNote">
      {{ note ? 'Note *' : 'Note' }}
    </button>

    <FormDialog
      v-if="showNote"
      title="Note"
      :error="error"
      submit-label="Save"
      @close="showNote = false"
      @submit="saveNote"
    >
      <FormField label="Note" hint="What you saw here. Searchable as req.note.">
        <textarea
          v-model="noteDraft"
          class="form-textarea"
          rows="6"
          placeholder="The token refresh answers 500 when..."
        ></textarea>
      </FormField>
    </FormDialog>
  </div>
</template>
