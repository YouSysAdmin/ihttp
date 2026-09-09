<script setup lang="ts">
// A gRPC body read without a schema: each frame, and inside it the
// fields the wire format shows, with a hex dump one click away. The
// server does the cutting and the decoding, this shows what it found.
import { onMounted, ref, watch } from 'vue'
import type { GrpcBody, GrpcField, GrpcFrame } from '../api/types'
import { fromBase64, hexdump } from '../composables/hexdump'
import { humanSize } from '../composables/humanSize'
import CodeView from './CodeView.vue'
import SchemaDialog from './SchemaDialog.vue'
import TabStrip, { type TabItem } from './TabStrip.vue'

const props = defineProps<{
  // The decode endpoint for this side of the exchange.
  url: string
}>()

const body = ref<GrpcBody | null>(null)
const loading = ref(true)
const error = ref('')
const showSchemas = ref(false)

// Per frame: 'json' when the schema read it, else 'fields', or 'hex'.
type View = 'json' | 'fields' | 'hex'
const views = ref<Map<number, View>>(new Map())

function viewOf(f: GrpcFrame): View {
  return views.value.get(f.index) ?? (f.json ? 'json' : 'fields')
}

function setView(f: GrpcFrame, v: string) {
  const next = new Map(views.value)
  next.set(f.index, v as View)
  views.value = next
}

function viewTabs(f: GrpcFrame): TabItem[] {
  const tabs: TabItem[] = []
  if (f.json) tabs.push({ id: 'json', label: 'JSON' })
  tabs.push({ id: 'fields', label: 'Fields' }, { id: 'hex', label: 'Hex' })
  return tabs
}

// An answer for a url that is no longer shown is dropped.
async function load() {
  const url = props.url
  loading.value = true
  error.value = ''
  try {
    const res = await fetch(url)
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
    const decoded = (await res.json()) as GrpcBody
    if (url !== props.url) return
    body.value = decoded
    views.value = new Map()
  } catch (e) {
    if (url !== props.url) return
    error.value = (e as Error).message
    body.value = null
  } finally {
    if (url === props.url) loading.value = false
  }
}

// The name of a Go package is not something a reader needs to see.
function plain(message: string): string {
  return message.replace(/^grpcmsg:\s*/, '')
}

function dump(f: GrpcFrame): string {
  return hexdump(fromBase64(f.raw)).join('\n')
}

function statusLabel(b: GrpcBody): string {
  if (b.status === undefined || b.status === '') return ''
  const name = GRPC_STATUS[Number(b.status)] ?? ''
  return `grpc-status ${b.status}${name ? ' ' + name : ''}${b.message ? ': ' + b.message : ''}`
}

const GRPC_STATUS: Record<number, string> = {
  0: 'OK',
  1: 'CANCELLED',
  2: 'UNKNOWN',
  3: 'INVALID_ARGUMENT',
  4: 'DEADLINE_EXCEEDED',
  5: 'NOT_FOUND',
  6: 'ALREADY_EXISTS',
  7: 'PERMISSION_DENIED',
  8: 'RESOURCE_EXHAUSTED',
  9: 'FAILED_PRECONDITION',
  10: 'ABORTED',
  11: 'OUT_OF_RANGE',
  12: 'UNIMPLEMENTED',
  13: 'INTERNAL',
  14: 'UNAVAILABLE',
  15: 'DATA_LOSS',
  16: 'UNAUTHENTICATED',
}

// The JSON box is as tall as its lines, within reason: a short message
// takes little room and a long one scrolls inside.
function jsonHeight(json: string): string {
  const lines = json.split('\n').length
  return Math.min(Math.max(lines * 19 + 44, 96), 520) + 'px'
}

// Fields flattened with their depth, so one table shows the nesting.
function rows(fields: GrpcField[] | undefined, depth = 0): { f: GrpcField; depth: number }[] {
  const out: { f: GrpcField; depth: number }[] = []
  for (const f of fields ?? []) {
    out.push({ f, depth })
    if (f.nested) out.push(...rows(f.nested, depth + 1))
  }
  return out
}

onMounted(load)
watch(() => props.url, load)
</script>

<template>
  <div class="grpc-view">
    <p v-if="loading" class="viewer-muted">Decoding frames...</p>
    <p v-else-if="error" class="viewer-muted">Could not decode the body: {{ error }}</p>
    <template v-else-if="body">
      <div class="grpc-head text-xs text-muted">
        <span>{{ body.kind }}</span>
        <span v-if="body.service" class="code-font">{{ body.service }}/{{ body.method }}</span>
        <span v-if="statusLabel(body)">{{ statusLabel(body) }}</span>
        <span v-if="body.kind !== 'protobuf'"
          >{{ body.frames.length }} {{ body.frames.length === 1 ? 'frame' : 'frames' }}</span
        >
        <span v-else>one message, no framing</span>
        <span v-if="body.more">and more</span>
        <span v-if="body.error" class="text-danger">{{ plain(body.error) }}</span>
        <span v-if="body.has_schema" class="code-font">{{ body.message_type }}</span>
        <!-- A schema is looked up by the gRPC method, and a bare
             protobuf body has none - so there is nothing to offer. -->
        <button
          v-if="body.kind !== 'protobuf'"
          type="button"
          class="btn btn-secondary btn-sm grpc-schemas"
          :title="body.has_schema ? 'The schemas of this project' : 'No schema names this call yet'"
          @click="showSchemas = true"
        >
          {{ body.has_schema ? 'Schemas' : 'Add schema' }}
        </button>
      </div>

      <div v-for="f in body.frames" :key="f.index" class="grpc-frame">
        <div class="grpc-frame-head">
          <span v-if="body.kind !== 'protobuf'" class="badge badge-neutral"
            >#{{ f.index + 1 }}</span
          >
          <span v-if="f.trailer" class="badge badge-info">trailer</span>
          <span v-if="f.compressed" class="badge badge-neutral">gzip</span>
          <span class="text-xs text-muted">{{ humanSize(f.length) }}</span>
          <span v-if="f.error" class="text-xs text-danger">{{ plain(f.error) }}</span>
          <TabStrip
            v-if="!f.trailer"
            class="grpc-frame-views"
            :model-value="viewOf(f)"
            :tabs="viewTabs(f)"
            @update:model-value="(v) => setView(f, v)"
          />
        </div>

        <pre v-if="viewOf(f) === 'hex'" class="hex-dump">{{ dump(f) }}</pre>

        <CodeView
          v-else-if="viewOf(f) === 'json' && f.json"
          :body="f.json"
          content-type="application/json"
          :style="{ height: jsonHeight(f.json) }"
          fill
        />

        <table v-else-if="f.trailer && f.trailers?.length" class="grpc-fields">
          <tbody>
            <tr v-for="h in f.trailers" :key="h.name">
              <td class="code-font">{{ h.name }}</td>
              <td class="code-font">{{ h.value }}</td>
            </tr>
          </tbody>
        </table>

        <table v-else-if="f.fields?.length" class="grpc-fields">
          <thead>
            <tr>
              <th>Field</th>
              <th>Wire</th>
              <th>Value</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in rows(f.fields)" :key="i">
              <td class="code-font" :style="{ paddingLeft: 8 + r.depth * 16 + 'px' }">
                {{ r.f.number }}
              </td>
              <td class="text-muted">{{ r.f.wire }}</td>
              <td class="code-font grpc-value">{{ r.f.nested ? 'message' : r.f.value }}</td>
            </tr>
          </tbody>
        </table>

        <!-- Bytes that failed to parse are not an empty message. -->
        <p v-else-if="f.error" class="viewer-muted">
          These bytes do not read as a protobuf message. The hex dump is what there is.
        </p>
        <p v-else class="viewer-muted">An empty message.</p>
      </div>
    </template>

    <SchemaDialog v-if="showSchemas" @close="showSchemas = false" @changed="load" />
  </div>
</template>
