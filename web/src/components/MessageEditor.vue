<script setup lang="ts">
// The editable half of a message: headers, then the body. The intercept
// editor and the sender both offer exactly this, and a binary body is
// treated the same way in both - shown, never edited.
import { computed } from 'vue'
import type { Header } from '../api/types'
import HeaderEditor from './HeaderEditor.vue'
import CodeEditor from './CodeEditor.vue'
import BodyView from './BodyView.vue'
import { bodyLanguage, contentType } from '../composables/http'

const props = withDefaults(
  defineProps<{
    // The body is bytes the console must not touch.
    binary?: boolean
    // The raw endpoint, for the binary view.
    rawUrl?: string
    // Whether the body is offered at all. A GET carries none, so a form
    // that knows the method hides the editor rather than show an empty
    // box that will not be sent anywhere useful.
    withBody?: boolean
  }>(),
  { binary: false, rawUrl: '', withBody: true },
)

const headers = defineModel<Header[]>('headers', { default: () => [] })
const body = defineModel<string>('body', { default: '' })

const language = computed(() => bodyLanguage(contentType(headers.value), body.value))
</script>

<template>
  <label class="form-label mt-4">Headers</label>
  <HeaderEditor v-model="headers" />

  <template v-if="props.withBody">
    <label class="form-label mt-4">Body</label>
    <BodyView
      v-if="props.binary"
      :body="body"
      :content-type="contentType(headers)"
      binary
      :raw-url="props.rawUrl"
    />
    <CodeEditor v-else v-model="body" :language="language" placeholder="Empty body" />
  </template>
</template>
