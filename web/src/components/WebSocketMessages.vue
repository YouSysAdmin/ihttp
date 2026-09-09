<script setup lang="ts">
// The messages of a WebSocket connection: the list on the left, newest
// first because a connection is watched as it goes, the selected payload
// on the right. A `lead` slot renders the page's own tab strip, so this
// sits under the same row as Request and Response.
import { computed, onMounted, ref, watch } from 'vue'
import { reqlogsApi } from '../api/reqlogs'
import type { WebSocketInfo, WsMessage, WsMessageSummary } from '../api/types'
import { useLiveEvents } from '../composables/useLiveEvents'
import { formatClock } from '../composables/formatDate'
import { humanSize } from '../composables/humanSize'
import BodyView from './BodyView.vue'
import EmptyState from './EmptyState.vue'

const props = defineProps<{
  entryId: string
  info: WebSocketInfo | null
}>()

// The count the page shows on its tab: what is loaded here is the truth
// while the connection is open and the stored figure lags.
const emit = defineEmits<{ (e: 'count', n: number): void }>()

const messages = ref<WsMessageSummary[]>([])
const more = ref(false)
const loading = ref(false)
const selected = ref<WsMessage | null>(null)

// The newest is first, the oldest loaded is last.
const newestSeq = computed(() => messages.value[0]?.seq ?? 0)
const oldestSeq = computed(() => messages.value.at(-1)?.seq ?? 0)

watch(newestSeq, (n) => emit('count', n))

// A page for an entry that is no longer shown is dropped.
async function load(reset = false) {
  const id = props.entryId
  loading.value = true
  try {
    const res = await reqlogsApi.messages(id, { before: reset ? 0 : oldestSeq.value })
    if (id !== props.entryId) return
    messages.value = reset ? res.data.messages : [...messages.value, ...res.data.messages]
    more.value = res.data.more
  } catch {
    if (reset && id === props.entryId) messages.value = []
  } finally {
    if (id === props.entryId) loading.value = false
  }
}

async function open(m: WsMessageSummary) {
  try {
    const res = await reqlogsApi.message(props.entryId, m.seq)
    selected.value = res.data.message
  } catch {
    selected.value = null
  }
}

const contentType = computed(() =>
  selected.value?.opcode === 'text' && !selected.value.compressed
    ? 'text/plain'
    : 'application/octet-stream',
)

// While the connection is open the list is the count: it grows with the
// live events, the stored figure catches up behind it.
const footer = computed(() => {
  const n = props.info?.closed_at
    ? (props.info.messages ?? messages.value.length)
    : Math.max(messages.value.length, props.info?.messages ?? 0)
  const count = `${n} ${n === 1 ? 'message' : 'messages'}`
  if (!props.info?.closed_at) return `${count}, open`
  const code = props.info.close_code ? `closed ${props.info.close_code}` : 'closed'
  const reason = props.info.close_reason ? ` ${props.info.close_reason}` : ''
  const dropped = props.info.dropped ? `, ${props.info.dropped} not recorded` : ''
  return `${count}, ${code}${reason}${dropped}`
})

// A live message goes on top. Ones that arrived while the first page was
// loading may already be in it, so the seq decides.
useLiveEvents({
  'reqlog.message': (m: WsMessageSummary) => {
    if (m.entry_id !== props.entryId) return
    if (m.seq <= newestSeq.value) return
    messages.value = [m, ...messages.value]
  },
})

watch(
  () => props.entryId,
  () => {
    selected.value = null
    void load(true)
  },
)
onMounted(() => void load(true))
</script>

<template>
  <div class="message-pane message-pane--fill ws-pane">
    <div class="strip">
      <slot name="lead"></slot>
      <div class="strip-end">
        <span class="text-xs text-muted">{{ footer }}</span>
      </div>
    </div>

    <div class="ws-split">
      <div class="ws-list">
        <EmptyState
          v-if="messages.length === 0 && !loading"
          title="No messages yet"
          text="Frames appear here as they pass."
        />
        <button
          v-for="m in messages"
          :key="m.seq"
          type="button"
          class="ws-row"
          :class="{ selected: selected?.seq === m.seq, in: m.direction === 'in' }"
          @click="open(m)"
        >
          <span
            class="ws-dir"
            :title="m.direction === 'in' ? 'from the server' : 'from the client'"
          >
            {{ m.direction === 'in' ? '<-' : '->' }}
          </span>
          <span class="badge badge-neutral ws-opcode">{{ m.opcode }}</span>
          <span class="ws-preview code-font truncate">{{
            m.preview ||
            (m.opcode === 'close' && m.close_code ? `${m.close_code} ${m.close_reason ?? ''}` : '')
          }}</span>
          <span class="ws-meta">{{ humanSize(m.size) }}</span>
          <span class="ws-meta">{{ formatClock(m.timestamp) }}</span>
        </button>
        <div v-if="more" class="list-more">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            @click="load()"
          >
            {{ loading ? 'Loading...' : 'Load older' }}
          </button>
        </div>
      </div>

      <div class="ws-body">
        <EmptyState
          v-if="!selected"
          title="Select a message"
          text="Its payload is shown here, as text or as a hex dump."
        />
        <BodyView
          v-else
          :key="selected.seq"
          :body="selected.payload"
          :content-type="contentType"
          :binary="selected.payload_binary"
          :size="selected.payload_size"
          :truncated="selected.payload_truncated"
          :raw-url="`/api/request-logs/${entryId}/messages/${selected.seq}/body`"
          :decode-url="
            selected.payload_binary
              ? `/api/request-logs/${entryId}/messages/${selected.seq}/protobuf`
              : ''
          "
          fill
        />
      </div>
    </div>
  </div>
</template>
