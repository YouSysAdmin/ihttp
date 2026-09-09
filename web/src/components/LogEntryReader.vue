<script setup lang="ts">
// One log entry, read: the URL line with the Actions menu, the facts of
// the exchange, its marks, and the Request, Response and Messages tabs.
// The page owns the list and the selection, this owns everything about
// the entry on show and tells the page when the entry changed.
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { reqlogsApi } from '../api/reqlogs'
import { senderApi } from '../api/sender'
import { automationApi } from '../api/automation'
import { apiErrorMessage } from '../api/client'
import type { Header, LogEntry } from '../api/types'
import { useNotificationStore } from '../stores/notification'
import { type PinnedExchange, useCompareStore } from '../stores/compare'
import { useSnippets } from '../composables/useSnippets'
import { formatClock } from '../composables/formatDate'
import { humanSize } from '../composables/humanSize'
import TabStrip, { type TabItem } from './TabStrip.vue'
import MethodBadge from './MethodBadge.vue'
import StatusBadge from './StatusBadge.vue'
import MessagePane from './MessagePane.vue'
import EntryMarks from './EntryMarks.vue'
import TimingBar from './TimingBar.vue'
import WebSocketMessages from './WebSocketMessages.vue'
import DropdownMenu from './DropdownMenu.vue'
import Notice from './Notice.vue'
import SnippetsDialog from './SnippetsDialog.vue'

const props = defineProps<{
  entry: LogEntry
  // Tags seen in this project, offered while adding one.
  knownTags: string[]
}>()

const emit = defineEmits<{
  // The entry as the server now has it, after a mark or a save.
  (e: 'update:entry', entry: LogEntry): void
  // The person asked for the entry to go. The page confirms and deletes.
  (e: 'delete', id: string): void
  // The tags changed, so the project's tag list may have too.
  (e: 'tags-changed'): void
}>()

const router = useRouter()
const notify = useNotificationStore()
const compare = useCompareStore()

// The entry spelled as curl and fetch(), for the Actions menu.
const snippets = useSnippets(
  () => props.entry.id,
  () => reqlogsApi.snippets(props.entry.id),
)

const side = ref<string>('request')

// The other spellings of this request, read before copying.
const showSnippets = ref(false)

// Folded away by default: the facts line is read at a glance, the
// breakdown is asked for.
const showTiming = ref(false)

const sideTabs = computed<TabItem[]>(() => [
  { id: 'request', label: 'Request' },
  { id: 'response', label: 'Response', disabled: !props.entry.response },
  {
    id: 'messages',
    label: 'Messages',
    count: props.entry.websocket?.messages,
    disabled: !props.entry.websocket,
  },
])

// The half on show. Both tab groups live in the pane's strip, so the
// pane must always render: an entry without a response falls back to
// the request rather than leaving the reader with no tabs at all.
const half = computed(() => {
  const e = props.entry
  const r = side.value === 'response' ? e.response : null
  if (r) {
    return {
      key: 'res-' + e.id,
      headers: r.headers,
      trailers: r.trailers ?? [],
      body: r.body,
      truncated: r.body_truncated,
      streamed: r.body_streamed,
      binary: r.body_binary,
      size: r.body_size,
      rawUrl: `/api/request-logs/${e.id}/body/response`,
      decodeUrl: reqlogsApi.grpcUrl(e.id, 'response'),
      startLine: `${r.proto} ${r.status_code}${r.status ? ' ' + r.status : ''}`,
      proto: r.proto,
      host: '',
    }
  }

  return {
    key: 'req-' + e.id,
    headers: e.headers,
    trailers: [] as Header[],
    body: e.body,
    truncated: e.body_truncated,
    streamed: false,
    binary: e.body_binary,
    size: e.body_size,
    startLine: requestLine.value,
    proto: e.proto,
    host: requestHost.value,
    rawUrl: `/api/request-logs/${e.id}/body/request`,
    decodeUrl: reqlogsApi.grpcUrl(e.id, 'request'),
  }
})

// The request line in origin form, which is what a server reads: a
// request reaches the proxy in absolute form and this is not what went
// over the wire byte for byte, which RawMessage says out loud.
const requestLine = computed(() => {
  const e = props.entry
  let target = e.url

  try {
    const u = new URL(e.url)
    target = u.pathname + u.search
  } catch {
    // Not a URL we can split. What was logged is better than nothing.
  }

  return `${e.method} ${target || '/'} ${e.proto}`
})

// The host of the request, for the Host line a reconstruction needs.
const requestHost = computed(() => {
  try {
    return new URL(props.entry.url).host
  } catch {
    return ''
  }
})

// An entry that has no response yet puts the Response tab out of reach,
// so the selection moves back to the request. The same for Messages on
// an entry that never upgraded.
watch(
  () => props.entry,
  (e) => {
    if (!e.response && side.value === 'response') side.value = 'request'
    if (!e.websocket && side.value === 'messages') side.value = 'request'
  },
  { immediate: true },
)

// One PATCH per change. The answer is the entry as stored.
const marksError = ref('')

async function setMarks(marks: { tags?: string[]; note?: string; color?: string }) {
  const id = props.entry.id
  marksError.value = ''
  try {
    const res = await reqlogsApi.patch(id, marks)
    if (props.entry.id === id) emit('update:entry', res.data.entry)
    if (marks.tags) emit('tags-changed')
  } catch (err) {
    marksError.value = apiErrorMessage(err, 'The change was refused')
    notify.error(marksError.value)
  }
}

// Saved moves the entry into the saved view, out of Clear's reach.
async function setSaved(saved: boolean) {
  const id = props.entry.id
  try {
    const res = saved ? await reqlogsApi.save(id) : await reqlogsApi.unsave(id)
    if (props.entry.id === id) emit('update:entry', res.data.entry)
    notify.success(saved ? 'Saved' : 'Removed from saved')
  } catch (err) {
    notify.error(apiErrorMessage(err, saved ? 'Could not save' : 'Could not remove from saved'))
  }
}

// The messages pane knows how many it loaded, which is ahead of the
// stored count while the connection is open.
function onMessageCount(n: number) {
  const ws = props.entry.websocket
  if (!ws || ws.closed_at || n <= ws.messages) return
  emit('update:entry', { ...props.entry, websocket: { ...ws, messages: n } })
}

// The menu copies for itself: a menu item cannot be a CopyButton, whose
// report is its own label.
async function copyText(value: string, what: string) {
  try {
    await navigator.clipboard.writeText(value)
    notify.success(`Copied ${what}`)
  } catch {
    notify.error('Could not copy - select the value and copy it by hand')
  }
}

const pinRef = computed<PinnedExchange>(() => ({
  kind: 'log',
  id: props.entry.id,
  label: `${props.entry.method} ${props.entry.url}`,
}))

async function sendToSender() {
  try {
    const res = await senderApi.clone(props.entry.id)
    router.push(`/sender/${res.data.request.id}`)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to copy to the sender'))
  }
}

async function sendToAutomation() {
  try {
    const res = await automationApi.clone(props.entry.id)
    router.push(`/automation/${res.data.job.id}`)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to copy to the automation'))
  }
}
</script>

<template>
  <div class="exchange">
    <div class="exchange-head">
      <MethodBadge :method="entry.method" />
      <span class="exchange-url code-font" :title="entry.url">{{ entry.url }}</span>
      <span v-if="entry.saved" class="badge badge-info">saved</span>
      <!-- Everything that can be done with the entry, in one menu, so
           the URL line stays a URL line. -->
      <DropdownMenu label="Actions">
        <button type="button" class="menu-item" @click="copyText(entry.url, 'the URL')">
          Copy URL
        </button>
        <template v-if="!entry.tunnel">
          <button
            type="button"
            class="menu-item"
            :disabled="!snippets"
            @click="copyText(snippets?.curl ?? '', 'curl')"
          >
            Copy as curl
          </button>
          <button
            type="button"
            class="menu-item"
            :disabled="!snippets"
            @click="showSnippets = true"
          >
            Copy as...
            <span class="menu-hint">HTTPie, fetch, Python, Go, PowerShell</span>
          </button>
        </template>
        <hr class="menu-sep" />
        <button v-if="!entry.saved" type="button" class="menu-item" @click="setSaved(true)">
          Save
        </button>
        <button v-else type="button" class="menu-item" @click="setSaved(false)">
          Remove from saved
        </button>
        <template v-if="!entry.tunnel">
          <hr class="menu-sep" />
          <button type="button" class="menu-item" @click="sendToSender">Send to sender</button>
          <button type="button" class="menu-item" @click="sendToAutomation">
            Send to automation
          </button>
          <hr class="menu-sep" />
          <!-- The same three states as CompareButtons, as menu items. -->
          <button
            v-if="!compare.pinned"
            type="button"
            class="menu-item"
            @click="compare.pin(pinRef)"
          >
            Pin as A
          </button>
          <button
            v-else-if="compare.isPinned({ kind: 'log', id: entry.id })"
            type="button"
            class="menu-item"
            @click="compare.unpin()"
          >
            Unpin A
          </button>
          <button
            v-else
            type="button"
            class="menu-item"
            @click="router.push(compare.hrefFor(pinRef))"
          >
            Compare with A
          </button>
        </template>
        <hr class="menu-sep" />
        <button type="button" class="menu-item text-danger" @click="emit('delete', entry.id)">
          Delete
          <span class="menu-hint">Del / Backspace</span>
        </button>
      </DropdownMenu>
    </div>

    <div class="exchange-sub">
      <span title="what the client spoke, then what the upstream spoke">
        {{ entry.proto
        }}<template v-if="entry.response && entry.response.proto !== entry.proto">
          -> {{ entry.response.proto }}</template
        >
      </span>
      <span>{{ formatClock(entry.created_at) }}</span>
      <template v-if="entry.graphql">
        <span class="gql-chip" title="GraphQL operation">
          {{ entry.graphql.type || 'graphql' }}
          <template v-if="entry.graphql.name"> {{ entry.graphql.name }}</template>
        </span>
        <span v-if="entry.graphql.batch" class="text-muted">
          {{ entry.graphql.batch }} operations
        </span>
        <span v-if="entry.graphql.errors" class="text-danger">
          {{ entry.graphql.errors }} error<template v-if="entry.graphql.errors > 1">s</template>
        </span>
      </template>

      <template v-if="entry.tunnel">
        <span>not decrypted</span>
        <span v-if="entry.tunnel.closed_at"
          >stood for {{ entry.response?.duration_ms ?? 0 }} ms</span
        >
        <span v-else class="text-muted">still open</span>
      </template>
      <template v-else-if="entry.response">
        <StatusBadge :code="entry.response.status_code" :reason="entry.response.status" />
        <span>{{ entry.response.status }}</span>
        <!-- The duration opens the breakdown it is the sum of, so the
             bar costs no room until it is asked for. -->
        <button
          type="button"
          class="link-button"
          :aria-expanded="showTiming"
          :title="showTiming ? 'Hide the timing breakdown' : 'Where the time went'"
          @click="showTiming = !showTiming"
        >
          {{ entry.response.duration_ms }} ms
        </button>
      </template>
      <span v-else class="text-muted">awaiting response</span>
    </div>

    <TimingBar
      v-if="showTiming && entry.response"
      :timings="entry.response.timings"
      :duration-ms="entry.response.duration_ms"
      :streamed="entry.response.body_streamed"
      class="mb-3"
    />

    <EntryMarks
      :tags="entry.tags ?? []"
      :note="entry.note ?? ''"
      :color="entry.color ?? ''"
      :suggestions="knownTags"
      :error="marksError"
      @update:tags="(tags) => setMarks({ tags })"
      @update:color="(color) => setMarks({ color })"
      @update:note="(note) => setMarks({ note })"
    />

    <WebSocketMessages
      v-if="side === 'messages' && entry.websocket"
      :entry-id="entry.id"
      :info="entry.websocket"
      @count="onMessageCount"
    >
      <template #lead>
        <TabStrip v-model="side" :tabs="sideTabs" class="side-tabs" />
      </template>
    </WebSocketMessages>
    <Notice v-else-if="entry.tunnel" kind="info" class="tunnel-note">
      <p>
        Relayed without being decrypted, so there is no request or response to read.
        {{ humanSize(entry.tunnel.bytes_out) }} went up and
        {{ humanSize(entry.tunnel.bytes_in) }} came back<template v-if="entry.tunnel.closed_at">
          over {{ entry.response?.duration_ms ?? 0 }} ms</template
        >.
      </p>
      <p v-if="entry.tunnel.error">
        It ended with <code>{{ entry.tunnel.error }}</code>
      </p>
      <p>This host is on the project's do-not-decrypt list, which lives on the Scope page.</p>
    </Notice>
    <MessagePane
      v-else
      :key="half.key"
      :headers="half.headers"
      :trailers="half.trailers"
      :body="half.body"
      :truncated="half.truncated"
      :streamed="half.streamed"
      :decode-url="half.decodeUrl"
      :binary="half.binary"
      :size="half.size"
      :raw-url="half.rawUrl"
      :graphql="!!entry.graphql"
      :request="side !== 'response'"
      :start-line="half.startLine"
      :proto="half.proto"
      :host="half.host"
      fill
    >
      <template #lead>
        <TabStrip v-model="side" :tabs="sideTabs" class="side-tabs" />
      </template>
    </MessagePane>

    <SnippetsDialog
      v-if="showSnippets"
      :snippets="snippets"
      :label="`${entry.method} ${entry.url}`"
      @close="showSnippets = false"
    />
  </div>
</template>
