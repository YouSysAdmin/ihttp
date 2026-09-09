<script setup lang="ts">
// The sender: the history on the left, an editor on the right. Save
// keeps a request without sending it, Send does both. A request cloned
// from the log lands here already filled in.
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { senderApi } from '../../api/sender'
import { apiErrorMessage } from '../../api/client'
import type { Header, SenderRequest, SenderSummary } from '../../api/types'
import { METHODS, PROTOS } from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useLiveEvents } from '../../composables/useLiveEvents'
import { useRouteSelection } from '../../composables/useRouteSelection'
import { useSnippets } from '../../composables/useSnippets'
import { formatClock } from '../../composables/formatDate'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import EmptyState from '../../components/EmptyState.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import SearchBar from '../../components/SearchBar.vue'
import ListRow from '../../components/ListRow.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import MessageEditor from '../../components/MessageEditor.vue'
import MessagePane from '../../components/MessagePane.vue'
import CopyButton from '../../components/CopyButton.vue'
import SnippetsDialog from '../../components/SnippetsDialog.vue'
import CompareButtons from '../../components/CompareButtons.vue'

const projects = useProjectStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()
const { selectedId, select, clear: clearSelection } = useRouteSelection('/sender')

const gated = computed(() => projects.loaded && !projects.active)

const loading = ref(true)
const requests = ref<SenderSummary[]>([])
const search = ref('')

const current = ref<SenderRequest | null>(null)

// The saved request spelled as curl and fetch(). Refetched when the
// stored request changes, since the editor may hold unsaved edits.
const snippets = useSnippets(
  () => (current.value ? `${current.value.id}@${current.value.updated_at}` : ''),
  () => senderApi.snippets(current.value!.id),
)

// The other spellings of this request, read before copying.
const showSnippets = ref(false)

// The editor. `dirty` says the form differs from what is stored, which
// is what makes Save mean something.
const method = ref('GET')
const url = ref('')
const proto = ref<string>('HTTP/2.0')
const headers = ref<Header[]>([])
const body = ref('')
const dirty = ref(false)
const busy = ref(false)
const saving = ref(false)
const pane = ref<string>('request')

// Methods that carry a body. The editor stays for any other method once
// a body is there, so nothing is hidden that would still be sent.
const BODY_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])
const withBody = computed(() => BODY_METHODS.has(method.value) || body.value !== '')

const paneTabs = computed<TabItem[]>(() => [
  { id: 'request', label: 'Request' },
  { id: 'response', label: 'Response', disabled: !current.value?.response },
])

function fill(r: SenderRequest | null) {
  method.value = r?.method ?? 'GET'
  url.value = r?.url ?? ''
  proto.value = r?.proto ?? 'HTTP/2.0'
  headers.value = (r?.headers ?? []).map((h) => ({ ...h }))
  body.value = r?.body ?? ''
  pane.value = r?.response ? 'response' : 'request'

  // The watcher below runs after this tick and would mark the fill as an
  // edit, so it is told to stay quiet until the fill has settled.
  suppress = true
  dirty.value = false
  void nextTick(() => (suppress = false))
}

let suppress = false

watch(
  [method, url, proto, headers, body],
  () => {
    if (!suppress) dirty.value = true
  },
  { deep: true },
)

async function load() {
  if (!projects.active) return
  loading.value = true
  try {
    const res = await senderApi.list({ search: search.value || undefined })
    requests.value = res.data.requests ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the history'))
  } finally {
    loading.value = false
  }
}

async function loadCurrent(id: string) {
  if (!id || !projects.active) {
    current.value = null
    fill(null)
    return
  }
  try {
    const res = await senderApi.get(id)
    // A slower answer must not replace what was selected since.
    if (selectedId.value !== id) return
    current.value = res.data.request
    fill(current.value)
  } catch (e) {
    if (selectedId.value !== id) return
    current.value = null
    notify.error(apiErrorMessage(e, 'Failed to load the request'))
  }
}

function startNew() {
  clearSelection()
  current.value = null
  fill(null)
}

async function save(): Promise<SenderRequest | null> {
  if (saving.value) return null
  saving.value = true
  try {
    const res = await senderApi.save({
      id: current.value?.id,
      method: method.value,
      url: url.value,
      proto: proto.value,
      headers: headers.value,
      // A binary body is shown, not edited: null keeps the stored bytes.
      body: current.value?.body_binary ? null : body.value,
    })
    current.value = res.data.request
    dirty.value = false
    select(res.data.request.id)
    return res.data.request
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not save the request'))
    return null
  } finally {
    saving.value = false
  }
}

async function send() {
  busy.value = true
  try {
    const saved = dirty.value || !current.value ? await save() : current.value
    if (!saved) return
    const res = await senderApi.send(saved.id)
    current.value = res.data.request
    dirty.value = false
    pane.value = 'response'
  } catch (e) {
    notify.error(apiErrorMessage(e, 'The request failed'))
  } finally {
    busy.value = false
  }
}

async function removeCurrent() {
  if (!current.value) return
  const ok = await confirm({
    title: 'Delete this request?',
    message: 'The saved request and its response are deleted.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await senderApi.remove(current.value.id)
    startNew()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not delete the request'))
  }
}

async function clearAll() {
  const ok = await confirm({
    title: 'Clear the sender history?',
    message: 'Every saved request of this project is deleted.',
    confirmText: 'Clear',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await senderApi.clear()
    startNew()
    notify.success('History cleared')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to clear the history'))
  }
}

useLiveEvents({
  'sender.saved': () => void load(),
  'sender.sent': () => void load(),
  'sender.deleted': () => void load(),
  'sender.cleared': () => (requests.value = []),
})

// On the first resolution honour a deep link that mounted before the
// project store was ready. Reset only on a real project switch.
watch(
  () => projects.active?.id,
  (_id, prev) => {
    if (prev === undefined) {
      void load()
      void loadCurrent(selectedId.value)
      return
    }
    startNew()
    void load()
  },
)

onMounted(() => {
  void load()
  void loadCurrent(selectedId.value)
})

// A save lands its id in the route with the request already loaded, so
// only a selection made elsewhere has to be fetched.
watch(selectedId, (id) => {
  if (current.value?.id !== id) void loadCurrent(id)
})
</script>

<template>
  <div class="reader-page">
    <PageHeader title="Sender">
      <button
        class="btn btn-secondary"
        :disabled="gated || requests.length === 0"
        @click="clearAll"
      >
        Clear history
      </button>
      <button class="btn btn-primary" :disabled="gated" @click="startNew">New request</button>
    </PageHeader>

    <ProjectGate v-if="gated" />

    <template v-else>
      <SearchBar v-model="search" class="mb-4" @submit="load" />

      <div class="card reader-split">
        <div class="list-pane">
          <LoadingBlock v-if="loading" />
          <EmptyState
            v-else-if="requests.length === 0"
            title="No requests yet"
            text="Compose one on the right, or send a log entry here."
          />
          <ListRow
            v-for="r in requests"
            v-else
            :key="r.id"
            :method="r.method"
            :url="r.url"
            :time="r.updated_at"
            :status-code="r.status_code"
            :status-reason="r.status"
            :selected="r.id === selectedId"
            @select="select(r.id)"
          >
            <template #meta>
              <span>{{ r.proto }}</span>
              <span v-if="r.source_log_id">from log</span>
            </template>
          </ListRow>
        </div>

        <div class="reader-pane">
          <div class="exchange">
            <div class="exchange-head">
              <select v-model="method" class="form-select method-select">
                <option v-for="m in METHODS" :key="m" :value="m">{{ m }}</option>
              </select>
              <input
                v-model="url"
                class="form-input code-font"
                placeholder="https://example.com/path"
                spellcheck="false"
                @keydown.enter.prevent="send"
              />
              <select v-model="proto" class="form-select proto-select" title="Protocol">
                <option v-for="p in PROTOS" :key="p" :value="p">{{ p }}</option>
              </select>
              <button class="btn btn-primary" :disabled="busy || !url" @click="send">
                {{ busy ? 'Sending...' : 'Send' }}
              </button>
            </div>
            <div class="exchange-sub">
              <button
                class="btn btn-secondary btn-sm"
                :disabled="!dirty || !url || saving"
                @click="save"
              >
                {{ current ? 'Save' : 'Save without sending' }}
              </button>
              <button
                v-if="current"
                class="btn btn-secondary btn-sm text-danger"
                @click="removeCurrent"
              >
                Delete
              </button>
              <CopyButton
                v-if="current"
                :value="snippets?.curl ?? ''"
                label="Copy as curl"
                :disabled="!snippets"
              />
              <button
                v-if="current"
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="!snippets"
                @click="showSnippets = true"
              >
                Copy as...
              </button>
              <CompareButtons
                v-if="current"
                kind="sender"
                :id="current.id"
                :label="`${current.method} ${current.url}`"
              />
              <span v-if="dirty && current" class="text-muted text-sm">unsaved changes</span>
            </div>

            <TabStrip v-model="pane" :tabs="paneTabs" class="side-tabs">
              <template #label="{ tab }">
                {{ tab.label }}
                <StatusBadge
                  v-if="tab.id === 'response' && current?.response"
                  class="ml-2"
                  :code="current.response.status_code"
                  :reason="current.response.status"
                />
              </template>
            </TabStrip>

            <div v-if="pane === 'request'" class="editor-scroll">
              <MessageEditor
                v-model:headers="headers"
                v-model:body="body"
                :binary="current?.body_binary ?? false"
                :raw-url="current ? `/api/sender/requests/${current.id}/body/request` : ''"
                :with-body="withBody"
              />
            </div>
            <template v-else-if="current?.response">
              <div class="exchange-sub">
                <span>{{ current.response.proto }}</span>
                <span>{{ current.response.status_code }} {{ current.response.status }}</span>
                <span>{{ current.response.duration_ms }} ms</span>
                <span>{{ formatClock(current.response.received_at) }}</span>
              </div>
              <MessagePane
                :key="current.id + current.response.received_at"
                :headers="current.response.headers"
                :body="current.response.body"
                :truncated="current.response.body_truncated"
                :binary="current.response.body_binary"
                :size="current.response.body_size"
                :raw-url="`/api/sender/requests/${current.id}/body/response`"
                fill
              />
            </template>
          </div>
        </div>
      </div>
    </template>

    <SnippetsDialog
      v-if="showSnippets"
      :snippets="snippets"
      :label="current ? `${current.method} ${current.url}` : ''"
      @close="showSnippets = false"
    />
  </div>
</template>
