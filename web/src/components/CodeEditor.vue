<script setup lang="ts">
// An editable body. The model is the text - what the person typed is
// what gets sent, byte for byte as UTF-8.
import { onMounted, watch } from 'vue'
import { useCodeMirror } from '../composables/useCodeMirror'
import type { BodyLanguage } from '../composables/http'

const props = withDefaults(defineProps<{ language?: BodyLanguage; placeholder?: string }>(), {
  language: 'text',
  placeholder: '',
})

const model = defineModel<string>({ default: '' })

const editor = useCodeMirror({
  language: props.language,
  placeholder: props.placeholder,
  onEdit: () => (model.value = editor.text.value),
})
const host = editor.host

onMounted(() => editor.mount(model.value))

// A change from outside - loading a different request - replaces the
// document. A change from typing already IS the document.
watch(model, (next) => {
  if (next !== editor.text.value) editor.set(next)
})

watch(
  () => props.language,
  (next) => editor.setLanguage(next),
)
</script>

<template>
  <div ref="host" class="code-host code-host--edit"></div>
</template>
