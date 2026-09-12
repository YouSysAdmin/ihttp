<script setup lang="ts">
// One HTTP message - a request or a response - as a tab bar over a
// body pane: Body, Headers, Raw and Auth. Every page that shows a message shows
// it this way, which is what stops the log and the intercept queue
// rendering the same thing two ways. A page that switches between two
// messages puts that switch in the `lead` slot, so both tab groups
// share one strip instead of stacking.
import { computed, onMounted, ref } from 'vue'
import type { Header } from '../api/types'
import HeadersTable from './HeadersTable.vue'
import AuthView from './AuthView.vue'
import RawMessage from './RawMessage.vue'
import BodyView from './BodyView.vue'
import TabStrip, { type TabItem } from './TabStrip.vue'
import { contentType } from '../composables/http'
import { credentialsOf } from '../composables/auth'
import { useSettingsStore } from '../stores/settings'

const props = defineProps<{
  headers: Header[]
  // Trailers a response carried after its body.
  trailers?: Header[]
  body: string
  truncated?: boolean
  // The server's verdict: bytes, not text.
  binary?: boolean
  // Byte count from the server.
  size?: number
  // The raw endpoint for this half of the exchange.
  rawUrl?: string
  // The body went to the client as it arrived and was not captured.
  streamed?: boolean
  // The gRPC decode endpoint for this half, when it is a gRPC body.
  decodeUrl?: string
  // The exchange is GraphQL, and whether this half is the request.
  graphql?: boolean
  request?: boolean
  // The wire start line - "POST /path HTTP/1.1", "HTTP/1.1 200 OK".
  // Given it, the pane offers a Raw tab. Without it there is nothing
  // honest to put at the top of one.
  startLine?: string
  // What this half actually spoke, for the Raw tab's own notes.
  proto?: string
  // The host, so a reconstructed request carries the Host line Go
  // keeps outside the header map.
  host?: string
  // Fill the parent pane, scrolling inside.
  fill?: boolean
}>()

type Tab = 'body' | 'headers' | 'trailers' | 'raw' | 'auth'

// Open on the body when there is one, else on the headers.
const tab = ref<Tab>(props.body ? 'body' : 'headers')

const ct = computed(() => contentType(props.headers))

// The cookies and the credential headers, counted for the tab. The tab
// is offered only when there is something on it, and what counts as a
// credential header is the built-in list plus the instance's own.
const settings = useSettingsStore()

onMounted(() => {
  settings.ensure()
})

const creds = computed(() => credentialsOf(props.headers, settings.authHeaders))

const tabs = computed<TabItem[]>(() => {
  const out: TabItem[] = [
    { id: 'body', label: 'Body' },
    { id: 'headers', label: 'Headers', count: props.headers.length },
  ]
  if (props.trailers?.length) {
    out.push({ id: 'trailers', label: 'Trailers', count: props.trailers.length })
  }
  if (props.startLine) {
    out.push({ id: 'raw', label: 'Raw' })
  }
  if (creds.value.count) {
    out.push({ id: 'auth', label: 'Auth', count: creds.value.count })
  }

  return out
})
</script>

<template>
  <div :class="['message-pane', { 'message-pane--fill': fill }]">
    <div class="strip">
      <slot name="lead"></slot>
      <div class="strip-end">
        <TabStrip v-model="tab" :tabs="tabs" />
        <slot name="actions"></slot>
      </div>
    </div>

    <div class="message-body">
      <BodyView
        v-if="tab === 'body'"
        :body="body"
        :content-type="ct"
        :binary="binary"
        :size="size"
        :truncated="truncated"
        :raw-url="rawUrl"
        :streamed="streamed"
        :decode-url="decodeUrl"
        :graphql="graphql"
        :request="request"
        :fill="fill"
      />
      <RawMessage
        v-else-if="tab === 'raw'"
        :start-line="startLine ?? ''"
        :headers="headers"
        :body="body"
        :binary="binary"
        :truncated="truncated"
        :streamed="streamed"
        :proto="proto"
        :host="host"
      />
      <HeadersTable v-else-if="tab === 'trailers'" :headers="trailers ?? []" />
      <AuthView v-else-if="tab === 'auth'" :headers="headers" />
      <HeadersTable v-else :headers="headers" />
    </div>
  </div>
</template>
