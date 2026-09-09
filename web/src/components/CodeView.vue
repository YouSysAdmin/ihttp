<script setup lang="ts">
// A body, read-only, highlighted for what it is. JSON is pretty-printed
// for reading - the raw bytes are one toggle away, because a body with
// a signature over it must be readable as sent too.
import { computed, ref, watch } from 'vue'
import { useCodeMirror } from '../composables/useCodeMirror'
import { bodyLanguage, prettyJSON } from '../composables/http'

const props = withDefaults(
  defineProps<{
    body: string
    contentType?: string
    truncated?: boolean
    // Fill the parent rather than size to content.
    fill?: boolean
    // The raw endpoint, for Download.
    rawUrl?: string
    // Byte count from the server, when known.
    size?: number
  }>(),
  { contentType: '', truncated: false, fill: false, rawUrl: '', size: 0 },
)

const pretty = ref(true)

const language = computed(() => bodyLanguage(props.contentType, props.body))
const shown = computed(() =>
  pretty.value && language.value === 'json' ? prettyJSON(props.body) : props.body,
)

const editor = useCodeMirror({ language: language.value, readOnly: true })
const host = editor.host

// The host is behind v-if, so the editor follows it: mounted when a
// body appears, destroyed when it goes.
watch(host, (el) => {
  if (el) editor.mount(shown.value)
  else editor.destroy()
})

watch(shown, (next) => editor.set(next))
watch(language, (next) => editor.setLanguage(next))
</script>

<template>
  <div :class="['code-view', { 'code-view--fill': fill }]">
    <div v-if="body" class="code-view-bar">
      <span class="text-xs text-muted">
        {{ (size || body.length).toLocaleString() }} bytes
        <template v-if="truncated"> (truncated)</template>
      </span>
      <div class="flex gap-2">
        <button
          v-if="language === 'json'"
          type="button"
          class="btn btn-secondary btn-sm"
          @click="pretty = !pretty"
        >
          {{ pretty ? 'Raw' : 'Pretty' }}
        </button>
        <a v-if="rawUrl" class="btn btn-secondary btn-sm" :href="rawUrl" download>Download</a>
      </div>
    </div>
    <div v-if="body" ref="host" class="code-host"></div>
    <p v-else class="viewer-muted">No body.</p>
  </div>
</template>
