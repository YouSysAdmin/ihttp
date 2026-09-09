<script setup lang="ts">
// The message in wire form: its start line, its headers, a blank line,
// its body. The fastest way to eyeball a protocol-level problem, and
// the shape you paste into a bug report.
//
// This is a RECONSTRUCTION and says so. It is built from what was
// stored, so it is not byte-for-byte what went over the wire: an
// HTTP/2 exchange had no text start line and no header casing at all,
// a request that reached the proxy in absolute form is shown in origin
// form, chunked framing is gone, and a body that was truncated or
// streamed is not here to show. Everything it does show is real.
import { computed } from 'vue'
import type { Header } from '../api/types'
import CopyButton from './CopyButton.vue'

const props = defineProps<{
  // "POST /path HTTP/1.1" or "HTTP/1.1 200 OK".
  startLine: string
  headers: Header[]
  body: string
  binary?: boolean
  truncated?: boolean
  streamed?: boolean
  // What the message actually spoke, so the note can be honest about
  // an h2 exchange having had no text form of this at all.
  proto?: string
  // The host a request was for. Go keeps Host apart from the header
  // map, so the stored headers usually have none - and an HTTP/1.1
  // request without a Host line is not a request at all, which would
  // make this block useless for pasting anywhere.
  host?: string
}>()

const head = computed(() => {
  const lines = [props.startLine]

  if (props.host && !props.headers.some((h) => h.name.toLowerCase() === 'host')) {
    lines.push(`Host: ${props.host}`)
  }

  for (const h of props.headers) {
    lines.push(`${h.name}: ${h.value}`)
  }

  return lines.join('\n')
})

// The body is left out when it is not text: a hex dump pasted into a
// wire form would look like bytes and be neither.
const bodyText = computed(() => (props.binary ? '' : props.body))

// What is COPIED is the wire form, so CRLF: paste it into netcat or a
// scratch file and a server will accept it. What is DISPLAYED keeps
// plain newlines, which is the same thing to read and tidier to paste
// into a bug report.
const text = computed(() => head.value.split('\n').join('\r\n') + '\r\n\r\n' + bodyText.value)

const h2 = computed(() => (props.proto ?? '').startsWith('HTTP/2'))
</script>

<template>
  <div class="raw-message">
    <div class="code-view-bar">
      <span class="text-xs text-muted"
        >Reconstructed from what was stored, not the captured bytes</span
      >
      <CopyButton :value="text" label="Copy" copied-label="Copied" />
    </div>

    <pre class="raw-text code-font">{{ head }}

<template v-if="bodyText">{{ bodyText }}</template></pre>

    <ul class="raw-notes">
      <li v-if="h2">
        This exchange spoke {{ proto }}, which has no text start line and no header casing - the
        first line and the capitals here are ours.
      </li>
      <li v-if="binary">The body is binary and is left out. Download it from the Body tab.</li>
      <li v-if="truncated">The body was cut at the capture limit, so it ends early.</li>
      <li v-if="streamed">The body went to the client as it arrived and was never captured.</li>
      <li>
        Chunked framing, content encodings and the exact header order of an h2 exchange are not
        reproduced.
      </li>
    </ul>
  </div>
</template>
