<script setup lang="ts">
// A body as bytes: offset, sixteen hex columns, the printable ASCII.
// Fetched from the raw endpoint, because the JSON body has already been
// coerced to text and cannot be turned back into bytes.
import { onMounted, ref, watch } from 'vue'
import { hexdump } from '../composables/hexdump'

const props = defineProps<{ url: string }>()

// Enough to see what a file is. A dump of a whole video is not a page
// anyone reads, and the download is beside it.
const LIMIT = 64 * 1024

const lines = ref<string[]>([])
const total = ref(0)
const loading = ref(true)
const error = ref('')

// An answer for a url that is no longer shown is dropped.
async function load() {
  const url = props.url
  loading.value = true
  error.value = ''
  try {
    const res = await fetch(url)
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
    const buf = new Uint8Array(await res.arrayBuffer())
    if (url !== props.url) return
    total.value = buf.length
    lines.value = hexdump(buf.subarray(0, LIMIT))
  } catch (e) {
    if (url !== props.url) return
    error.value = (e as Error).message
  } finally {
    if (url === props.url) loading.value = false
  }
}

onMounted(load)
watch(() => props.url, load)
</script>

<template>
  <div>
    <p v-if="loading" class="viewer-muted">Loading bytes...</p>
    <p v-else-if="error" class="viewer-muted">Could not load the body: {{ error }}</p>
    <template v-else>
      <pre class="hex-dump">{{ lines.join('\n') }}</pre>
      <p v-if="total > lines.length * 16" class="text-xs text-muted mt-2">
        First {{ (lines.length * 16).toLocaleString() }} of {{ total.toLocaleString() }} bytes.
        Download the body for the rest.
      </p>
    </template>
  </div>
</template>
