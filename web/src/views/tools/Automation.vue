<script setup lang="ts">
// Automation. The jobs are a plain table, like the rules. Opening one is a
// reader like the request log: the fired requests on the left, the
// selected request and its response on the right. The template, the
// payload and the stop conditions are edited in the Setup dialog. A
// developer tool for seeing how the app under test handles a range of
// inputs.
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { automationApi } from '../../api/automation'
import { apiErrorMessage } from '../../api/client'
import type {
  AutomationJob,
  AutomationPayloadKind,
  AutomationResult,
  AutomationResultSummary,
  AutomationStatus,
  AutomationStopCondition,
  AutomationSummary,
  Header,
} from '../../api/types'
import { METHODS, PROTOS } from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { useLiveEvents } from '../../composables/useLiveEvents'
import { useRouteSelection } from '../../composables/useRouteSelection'
import { useSnippets } from '../../composables/useSnippets'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import EmptyState from '../../components/EmptyState.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import SearchBar from '../../components/SearchBar.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import StatusBadge from '../../components/StatusBadge.vue'
import MessageEditor from '../../components/MessageEditor.vue'
import MessagePane from '../../components/MessagePane.vue'
import FormDialog from '../../components/FormDialog.vue'
import FormField from '../../components/FormField.vue'
import CodeEditor from '../../components/CodeEditor.vue'
import CopyButton from '../../components/CopyButton.vue'
import SnippetsDialog from '../../components/SnippetsDialog.vue'
import CompareButtons from '../../components/CompareButtons.vue'
import StopConditionEditor from '../../components/StopConditionEditor.vue'

const projects = useProjectStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()
const { selectedId, select, clear: clearSelection } = useRouteSelection('/automation')

const gated = computed(() => projects.loaded && !projects.active)

const loading = ref(true)
const jobs = ref<AutomationSummary[]>([])
const search = ref('')

const creating = ref(false)
const showDetail = computed(() => creating.value || !!selectedId.value)

const current = ref<AutomationJob | null>(null)

// The editor mirror of the current job.
const name = ref('')
const method = ref('GET')
const url = ref('')
const proto = ref<string>('HTTP/2.0')
const headers = ref<Header[]>([])
const body = ref('')
const placeholder = ref('AUTO')
const urlEncode = ref(true)
const concurrency = ref(10)

const payloadKind = ref<AutomationPayloadKind>('library')
const listSource = ref<'typed' | 'file'>('typed')
const listText = ref('')
const listFile = ref('')
const listSeparator = ref('')
const listPicker = ref<HTMLInputElement | null>(null)
const numFrom = ref(1)
const numTo = ref(100)
const numStep = ref(1)
const randCount = ref(50)
const randLength = ref(16)
const randCharset = ref('alnum')

const stopMatch = ref<'any' | 'all'>('any')
const stopOn = ref<AutomationStopCondition[]>([])

let suppress = false
const dirty = ref(false)
const busy = ref(false)
const saving = ref(false)
const showSetup = ref(false)
const setupError = ref('')

const results = ref<AutomationResultSummary[]>([])
const selectedResult = ref<AutomationResult | null>(null)
const side = ref<string>('request')

// The selected result's request spelled as curl and fetch().
const snippets = useSnippets(
  () =>
    current.value && selectedResult.value ? `${current.value.id}/${selectedResult.value.id}` : '',
  () => automationApi.snippets(current.value!.id, selectedResult.value!.id),
)

// The other spellings of the selected result's request.
const showSnippets = ref(false)

const CHARSETS = ['alnum', 'alpha', 'digits', 'hex', 'printable']

const SEPARATORS = [
  { value: '', label: 'none, the whole line' },
  { value: ',', label: 'comma' },
  { value: ';', label: 'semicolon' },
  { value: '\t', label: 'tab' },
  { value: '|', label: 'pipe' },
]

// Where a value is written: $N for column N, the placeholder for column 1.
// The same rule the server applies, see automation.Tokens.
const COLUMN_TOKEN = /\$([1-9][0-9]*)/g

const STATUS_VARIANT: Record<AutomationStatus, string> = {
  draft: 'neutral',
  running: 'warning',
  done: 'success',
  stopped: 'neutral',
  error: 'danger',
}

// Methods that carry a body. The editor is also kept for any other method
// once a body is there - cloned from the log, say - so nothing is hidden
// that would still be sent.
const BODY_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE'])
const withBody = computed(() => BODY_METHODS.has(method.value) || body.value !== '')

const running = computed(() => current.value?.status === 'running')

const sortedResults = computed(() => [...results.value].sort((a, b) => a.index - b.index))

// How many tokens the template carries, and the highest column they name.
const template = computed(() => {
  const ph = placeholder.value
  let positions = 0
  let columns = 0
  for (const part of [url.value, body.value, ...headers.value.map((h) => h.value)]) {
    if (ph) positions += part.split(ph).length - 1
    for (const m of part.matchAll(COLUMN_TOKEN)) {
      positions++
      columns = Math.max(columns, Number(m[1]))
    }
  }
  return { positions, columns: Math.max(columns, positions > 0 ? 1 : 0) }
})
const positions = computed(() => template.value.positions)

const estimate = computed(() => {
  switch (payloadKind.value) {
    case 'list': {
      // A file is counted by the server. Trailing blank lines are not
      // values, and the server drops them the same way. An empty box is
      // no values.
      if (listSource.value === 'file') return null
      const text = listText.value.replace(/\n+$/, '')
      return text === '' ? 0 : text.split('\n').length
    }
    case 'numbers': {
      const step = numStep.value || 1
      if ((step > 0 && numTo.value < numFrom.value) || (step < 0 && numTo.value > numFrom.value))
        return 0
      return Math.floor(Math.abs(numTo.value - numFrom.value) / Math.abs(step)) + 1
    }
    case 'random':
      return randCount.value
    default:
      return null
  }
})

const sideTabs = computed<TabItem[]>(() => [
  { id: 'request', label: 'Request' },
  {
    id: 'response',
    label: 'Response',
    disabled: !selectedResult.value || !!selectedResult.value.error,
  },
])

const half = computed(() => {
  const r = selectedResult.value
  if (!r || !current.value) return null
  if (side.value === 'response' && !r.error) {
    return {
      key: 'res-' + r.id,
      headers: r.headers ?? [],
      body: r.body ?? '',
      truncated: r.body_truncated ?? false,
      binary: r.body_binary,
      size: r.body_size,
      rawUrl: `/api/automation/jobs/${current.value.id}/results/${r.id}/body/response`,
    }
  }
  return {
    key: 'req-' + r.id,
    headers: r.req_headers ?? [],
    body: r.req_body ?? '',
    truncated: false,
    binary: r.req_body_binary,
    size: r.req_body_size,
    rawUrl: `/api/automation/jobs/${current.value.id}/results/${r.id}/body/request`,
  }
})

function fill(j: AutomationJob | null) {
  name.value = j?.name ?? ''
  method.value = j?.method ?? 'GET'
  url.value = j?.url ?? ''
  proto.value = j?.proto ?? 'HTTP/2.0'
  headers.value = (j?.headers ?? []).map((h) => ({ ...h }))
  body.value = j?.body ?? ''
  placeholder.value = j?.placeholder || 'AUTO'
  urlEncode.value = j?.url_encode ?? true
  concurrency.value = j?.concurrency || 10
  const p = j?.payload
  payloadKind.value = p?.kind ?? 'library'
  listSource.value = p?.file ? 'file' : 'typed'
  listText.value = (p?.list ?? []).join('\n')
  listFile.value = p?.file ?? ''
  listSeparator.value = p?.separator ?? ''
  numFrom.value = p?.from ?? 1
  numTo.value = p?.to ?? 100
  numStep.value = p?.step ?? 1
  randCount.value = p?.count ?? 50
  randLength.value = p?.length ?? 16
  randCharset.value = p?.charset ?? 'alnum'
  stopMatch.value = j?.stop_match === 'all' ? 'all' : 'any'
  stopOn.value = (j?.stop_on ?? []).map((c) => ({ ...c }))
  suppress = true
  dirty.value = false
  void nextTick(() => (suppress = false))
}

watch(
  [
    name,
    method,
    url,
    proto,
    headers,
    body,
    placeholder,
    urlEncode,
    concurrency,
    payloadKind,
    listSource,
    listText,
    listFile,
    listSeparator,
    numFrom,
    numTo,
    numStep,
    randCount,
    randLength,
    randCharset,
    stopMatch,
    stopOn,
  ],
  () => {
    if (!suppress) dirty.value = true
  },
  { deep: true },
)

function payloadOf() {
  switch (payloadKind.value) {
    case 'list':
      return {
        kind: 'list' as const,
        list: listSource.value === 'typed' ? listText.value.split('\n') : [],
        file: listSource.value === 'file' ? listFile.value : '',
        separator: listSeparator.value,
      }
    case 'numbers':
      return { kind: 'numbers' as const, from: numFrom.value, to: numTo.value, step: numStep.value }
    case 'random':
      return {
        kind: 'random' as const,
        count: randCount.value,
        length: randLength.value,
        charset: randCharset.value,
      }
    default:
      return { kind: 'library' as const }
  }
}

// Load from file reads a file the browser was given into the box, so the
// values are kept with the job and the file can go.
async function pickList(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  try {
    listText.value = await file.text()
  } catch {
    notify.error('Could not read the file')
  }
}

async function load() {
  if (!projects.active) return
  loading.value = true
  try {
    const res = await automationApi.list({ search: search.value || undefined })
    jobs.value = res.data.jobs ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load the jobs'))
  } finally {
    loading.value = false
  }
}

async function loadCurrent(id: string) {
  selectedResult.value = null
  results.value = []
  side.value = 'request'
  if (!id || !projects.active) {
    if (!creating.value) current.value = null
    return
  }
  try {
    const res = await automationApi.get(id)
    // A slower answer must not replace what was selected since.
    if (selectedId.value !== id) return
    current.value = res.data.job
    fill(current.value)
    await loadResults(id)
  } catch (e) {
    if (selectedId.value !== id) return
    current.value = null
    notify.error(apiErrorMessage(e, 'Failed to load the job'))
  }
}

async function loadResults(id: string) {
  try {
    const res = await automationApi.results(id)
    if (selectedId.value !== id) return
    results.value = res.data.results ?? []
  } catch {
    if (selectedId.value !== id) return
    results.value = []
  }
}

function startNew() {
  clearSelection()
  creating.value = true
  current.value = null
  fill(null)
  results.value = []
  selectedResult.value = null
  setupError.value = ''
  showSetup.value = true
}

function backToList() {
  creating.value = false
  clearSelection()
  current.value = null
  void load()
}

// Save writes the form. A rewrite drops the stored results, so the list
// is emptied here too. Where a refusal is shown depends on who asked: the
// Setup dialog carries it in its error line, the run head as a toast.
async function save(report: 'dialog' | 'toast' = 'toast'): Promise<AutomationJob | null> {
  if (saving.value) return null
  saving.value = true
  try {
    const res = await automationApi.save({
      id: current.value?.id,
      name: name.value,
      method: method.value,
      url: url.value,
      proto: proto.value,
      headers: headers.value,
      // A binary body is shown, not edited: null keeps the stored bytes.
      body: current.value?.body_binary ? null : body.value,
      placeholder: placeholder.value,
      url_encode: urlEncode.value,
      payload: payloadOf(),
      concurrency: concurrency.value,
      stop_match: stopMatch.value,
      stop_on: stopOn.value,
    })
    current.value = res.data.job
    dirty.value = false
    results.value = []
    selectedResult.value = null
    // The route watcher sees the job is already loaded and leaves it be.
    if (res.data.job.id !== selectedId.value) select(res.data.job.id)
    return res.data.job
  } catch (e) {
    const message = apiErrorMessage(e, 'Could not save the job')
    if (report === 'dialog') setupError.value = message
    else notify.error(message)
    return null
  } finally {
    saving.value = false
  }
}

async function saveSetup() {
  setupError.value = ''
  const saved = await save('dialog')
  if (saved) showSetup.value = false
}

async function start() {
  busy.value = true
  try {
    const saved = dirty.value || !current.value ? await save() : current.value
    if (!saved) return
    results.value = []
    selectedResult.value = null
    const res = await automationApi.start(saved.id)
    current.value = res.data.job
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not start the run'))
  } finally {
    busy.value = false
  }
}

async function stop() {
  if (!current.value) return
  try {
    await automationApi.stop(current.value.id)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not stop the run'))
  }
}

async function removeJob(id: string) {
  const job = jobs.value.find((j) => j.id === id)
  const ok = await confirm({
    title: `Delete ${job?.name || 'this job'}?`,
    message: 'The job and its results are deleted.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await automationApi.remove(id)
    if (id === selectedId.value) backToList()
    else void load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not delete the job'))
  }
}

async function clearAll() {
  const ok = await confirm({
    title: 'Clear every automation job?',
    message: 'Every job of this project and its results are deleted.',
    confirmText: 'Clear',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await automationApi.clear()
    jobs.value = []
    notify.success('Jobs cleared')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to clear the jobs'))
  }
}

// The result last clicked, so a slower answer to an earlier click does
// not replace it.
let wantedResult = ''

async function openResult(r: AutomationResultSummary) {
  if (!current.value) return
  const jobId = current.value.id
  wantedResult = r.id
  try {
    const res = await automationApi.result(jobId, r.id)
    if (current.value?.id !== jobId || wantedResult !== r.id) return
    selectedResult.value = res.data.result
    side.value = res.data.result.error ? 'request' : 'response'
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not load the result'))
  }
}

useLiveEvents({
  // Progress arrives often, so the row is patched in place. The list is
  // fetched only for a job it does not know yet.
  'automation.job': (s: AutomationSummary) => {
    if (jobs.value.some((j) => j.id === s.id)) {
      jobs.value = jobs.value.map((j) => (j.id === s.id ? { ...j, ...s } : j))
    } else {
      void load()
    }
    if (current.value && s.id === current.value.id) {
      current.value = {
        ...current.value,
        status: s.status,
        total: s.total,
        completed: s.completed,
        error: s.error,
      }
    }
  },
  'automation.result': (e: { job_id: string; result: AutomationResultSummary }) => {
    if (!current.value || e.job_id !== current.value.id) return
    if (!results.value.some((x) => x.id === e.result.id)) results.value.push(e.result)
    current.value = { ...current.value, completed: results.value.length }
  },
  'automation.deleted': () => void load(),
  'automation.cleared': () => {
    jobs.value = []
  },
})

watch(
  () => projects.active?.id,
  (_id, prev) => {
    // On the first resolution honour a deep link that mounted before the
    // project store was ready. Reset only on a real project switch.
    if (prev === undefined) {
      void load()
      if (selectedId.value) void loadCurrent(selectedId.value)
      return
    }
    backToList()
  },
)

onMounted(() => {
  void load()
  if (selectedId.value) void loadCurrent(selectedId.value)
})

// A save of a new job lands its id in the route with the job already
// loaded. Only a selection made elsewhere, such as the table, a deep
// link or the back button, has to be fetched.
watch(selectedId, (id) => {
  if (!id) return
  creating.value = false
  if (current.value?.id !== id) void loadCurrent(id)
})

function shortUrl(u: string) {
  return u.replace(/^https?:\/\//, '')
}
</script>

<template>
  <div class="reader-page">
    <PageHeader :title="showDetail ? 'Automation job' : 'Automation'">
      <template v-if="!showDetail">
        <button class="btn btn-secondary" :disabled="gated || jobs.length === 0" @click="clearAll">
          Clear jobs
        </button>
        <button class="btn btn-primary" :disabled="gated" @click="startNew">New job</button>
      </template>
      <button v-else class="btn btn-secondary" @click="backToList">Back to jobs</button>
    </PageHeader>

    <ProjectGate v-if="gated" />

    <!-- The jobs, a plain table. -->
    <template v-else-if="!showDetail">
      <SearchBar
        v-model="search"
        class="mb-4"
        placeholder="Search by name or URL"
        :help="false"
        submit-label="Search"
        @submit="load"
      />

      <div class="card">
        <LoadingBlock v-if="loading" />
        <EmptyState
          v-else-if="jobs.length === 0"
          title="No jobs yet"
          text="Add a job, mark its template with the placeholder and run it."
        />
        <table v-else class="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Target</th>
              <th>Payload</th>
              <th>Status</th>
              <th class="text-right"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="j in jobs" :key="j.id" class="automation-row" @click="select(j.id)">
              <td>
                <span class="cell-title">{{ j.name || shortUrl(j.url) }}</span>
              </td>
              <td>
                <code class="mr-1">{{ j.method }}</code>
                <code class="automation-target">{{ shortUrl(j.url) }}</code>
              </td>
              <td>
                <span class="badge badge-neutral">{{ j.payload_kind }}</span>
              </td>
              <td>
                <span class="badge" :class="`badge-${STATUS_VARIANT[j.status]}`">{{
                  j.status
                }}</span>
                <span v-if="j.total" class="text-muted text-sm ml-2"
                  >{{ j.completed }}/{{ j.total }}</span
                >
              </td>
              <td class="text-right" @click.stop>
                <button class="btn btn-secondary btn-sm text-danger" @click="removeJob(j.id)">
                  Delete
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <!-- One job, a reader like the request log. -->
    <template v-else>
      <div class="exchange-head automation-run-head">
        <select v-model="method" class="form-select method-select">
          <option v-for="m in METHODS" :key="m" :value="m">{{ m }}</option>
        </select>
        <input
          v-model="url"
          class="form-input code-font"
          placeholder="https://example.com/users/AUTO"
          spellcheck="false"
        />
        <select v-model="proto" class="form-select proto-select" title="Protocol">
          <option v-for="p in PROTOS" :key="p" :value="p">{{ p }}</option>
        </select>
        <button v-if="running" class="btn btn-secondary text-danger" @click="stop">Stop</button>
        <button
          v-else
          class="btn btn-primary"
          :disabled="busy || !url || positions === 0"
          @click="start"
        >
          {{ busy ? 'Starting...' : 'Start' }}
        </button>
      </div>
      <div class="exchange-sub automation-run-sub mb-3">
        <button class="btn btn-secondary btn-sm" @click="showSetup = true">Setup</button>
        <button
          class="btn btn-secondary btn-sm"
          :disabled="!dirty || !url || running || saving"
          @click="save()"
        >
          Save
        </button>
        <button
          v-if="current"
          class="btn btn-secondary btn-sm text-danger"
          :disabled="running"
          @click="removeJob(current.id)"
        >
          Delete
        </button>
        <span class="text-muted text-sm"
          >{{ positions }} {{ positions === 1 ? 'position' : 'positions'
          }}<template v-if="template.columns > 1">
            in {{ template.columns }} columns</template
          ></span
        >
        <span v-if="current" class="badge" :class="`badge-${STATUS_VARIANT[current.status]}`">
          {{ current.status }}
        </span>
        <span v-if="current?.total" class="text-muted text-sm">
          {{ current.completed }}/{{ current.total }}
        </span>
        <a
          v-if="current && results.length > 0"
          class="btn btn-secondary btn-sm"
          :href="automationApi.harUrl(current.id)"
          download
          >Export HAR</a
        >
        <span v-if="current?.error" class="text-danger text-sm">{{ current.error }}</span>
        <span v-if="dirty" class="text-muted text-sm">unsaved changes</span>
      </div>

      <div class="card reader-split">
        <div class="list-pane">
          <EmptyState
            v-if="results.length === 0"
            title="No results yet"
            text="Set the payload in Setup, then Start."
          />
          <!-- Not a ListRow: the row leads with an index and a payload, not a method and a URL. -->
          <button
            v-for="r in sortedResults"
            v-else
            :key="r.id"
            type="button"
            class="log-row automation-result-row"
            :class="{ selected: selectedResult?.id === r.id, matched: r.matched }"
            @click="openResult(r)"
          >
            <div class="log-row-top">
              <span class="automation-idx text-muted">#{{ r.index + 1 }}</span>
              <span v-if="r.error" class="badge badge-danger">error</span>
              <StatusBadge v-else :code="r.status_code || 0" :reason="r.status || ''" />
            </div>
            <div class="log-path truncate" :title="r.payload">{{ r.payload || '(empty)' }}</div>
            <div class="log-row-meta">
              <span>{{ r.size ?? 0 }} B</span>
              <span>{{ r.duration_ms ?? 0 }} ms</span>
              <span v-if="r.matched" class="text-warning">stopped here</span>
            </div>
          </button>
        </div>

        <div class="reader-pane">
          <EmptyState
            v-if="!selectedResult"
            title="Select a result"
            text="The request that was sent and its response are shown here."
          />
          <div v-else class="exchange">
            <div class="exchange-sub">
              <span>payload</span>
              <code class="automation-payload" :title="selectedResult.payload">{{
                selectedResult.payload || '(empty)'
              }}</code>
              <template v-if="selectedResult.status_code">
                <StatusBadge
                  :code="selectedResult.status_code"
                  :reason="selectedResult.status || ''"
                />
                <span>{{ selectedResult.duration_ms ?? 0 }} ms</span>
              </template>
              <span v-if="selectedResult.error" class="text-danger">{{
                selectedResult.error
              }}</span>
              <CopyButton
                :value="snippets?.curl ?? ''"
                label="Copy as curl"
                :disabled="!snippets"
              />
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :disabled="!snippets"
                @click="showSnippets = true"
              >
                Copy as...
              </button>
              <CompareButtons
                v-if="current"
                kind="automation"
                :id="selectedResult.id"
                :job-id="current.id"
                :label="`#${selectedResult.index + 1} ${selectedResult.payload}`"
              />
            </div>

            <MessagePane
              v-if="half"
              :key="half.key"
              :headers="half.headers"
              :body="half.body"
              :truncated="half.truncated"
              :binary="half.binary"
              :size="half.size"
              :raw-url="half.rawUrl"
              fill
            >
              <template #lead>
                <TabStrip v-model="side" :tabs="sideTabs" class="side-tabs" />
              </template>
            </MessagePane>
          </div>
        </div>
      </div>
    </template>

    <!-- The template, payload and stop conditions. The request line is
         here as well as in the run head - the same refs, so the two cannot
         disagree - because a new job is put together in this dialog. -->
    <FormDialog
      v-if="showSetup"
      title="Automation setup"
      size="modal-w720"
      :error="setupError"
      :saving="saving"
      @close="showSetup = false"
      @submit="saveSetup"
    >
      <label class="form-label">Request</label>
      <div class="automation-run-head mb-3">
        <select v-model="method" class="form-select method-select">
          <option v-for="m in METHODS" :key="m" :value="m">{{ m }}</option>
        </select>
        <input
          v-model="url"
          class="form-input code-font"
          placeholder="https://example.com/users/AUTO"
          spellcheck="false"
        />
        <select v-model="proto" class="form-select proto-select" title="Protocol">
          <option v-for="p in PROTOS" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>

      <div class="form-row">
        <FormField label="Name" hint="Optional, for the table.">
          <input v-model="name" class="form-input" placeholder="Name this run" />
        </FormField>
        <FormField
          label="Placeholder"
          hint="Put this token in the URL, a header value or the body. A list with a separator also offers its columns as $1, $2 and so on, and the placeholder is $1."
        >
          <input v-model="placeholder" class="form-input code-font" spellcheck="false" />
        </FormField>
      </div>

      <label class="checkbox-label mt-2">
        <input v-model="urlEncode" type="checkbox" />
        Percent-encode the payload where it lands in the URL
      </label>

      <MessageEditor
        v-model:headers="headers"
        v-model:body="body"
        :binary="current?.body_binary ?? false"
        :raw-url="current ? `/api/automation/jobs/${current.id}/body` : ''"
        :with-body="withBody"
      />

      <hr class="automation-sep" />

      <FormField label="Payload source" hint="What to write in the placeholder's place.">
        <select v-model="payloadKind" class="form-select">
          <option value="list">List of values</option>
          <option value="numbers">Number range</option>
          <option value="random">Random strings</option>
          <option value="library">Built-in awkward inputs</option>
        </select>
      </FormField>

      <template v-if="payloadKind === 'list'">
        <div class="form-row">
          <FormField label="Lines come from" hint="One line is one request.">
            <select v-model="listSource" class="form-select">
              <option value="typed">the box below</option>
              <option value="file">a file on this machine</option>
            </select>
          </FormField>
          <FormField
            label="Separator"
            hint="Splits a line into columns $1, $2 and so on, taken as they are."
          >
            <select v-model="listSeparator" class="form-select">
              <option v-for="sep in SEPARATORS" :key="sep.value" :value="sep.value">
                {{ sep.label }}
              </option>
            </select>
          </FormField>
        </div>

        <template v-if="listSource === 'typed'">
          <div class="automation-list-head">
            <label class="form-label mb-0">Values, one per line</label>
            <button type="button" class="btn btn-secondary btn-sm" @click="listPicker?.click()">
              Load from file
            </button>
            <input ref="listPicker" type="file" hidden @change="pickList" />
          </div>
          <CodeEditor
            v-model="listText"
            language="text"
            placeholder="One value per line, columns split by the separator"
          />
        </template>
        <FormField
          v-else
          label="File path"
          hint="Absolute path on the machine ihttp runs on, read when the run starts. ~ is your home."
        >
          <input
            v-model="listFile"
            class="form-input code-font"
            placeholder="~/payloads/pairs.csv"
            spellcheck="false"
          />
        </FormField>
      </template>

      <div v-else-if="payloadKind === 'numbers'" class="form-row">
        <FormField label="From">
          <input v-model.number="numFrom" class="form-input status-input" type="number" />
        </FormField>
        <FormField label="To">
          <input v-model.number="numTo" class="form-input status-input" type="number" />
        </FormField>
        <FormField label="Step">
          <input v-model.number="numStep" class="form-input status-input" type="number" />
        </FormField>
      </div>

      <div v-else-if="payloadKind === 'random'" class="form-row">
        <FormField label="Count">
          <input v-model.number="randCount" class="form-input status-input" type="number" min="1" />
        </FormField>
        <FormField label="Length">
          <input
            v-model.number="randLength"
            class="form-input status-input"
            type="number"
            min="1"
          />
        </FormField>
        <FormField label="Charset">
          <select v-model="randCharset" class="form-select">
            <option v-for="c in CHARSETS" :key="c" :value="c">{{ c }}</option>
          </select>
        </FormField>
      </div>

      <p v-else class="form-hint">
        A built-in set of edge cases - empty, very long, unicode, newlines, quotes, numbers,
        structure characters - for finding where a handler mishandles an input.
      </p>

      <div class="form-row mt-3">
        <FormField label="Concurrency" hint="Requests in flight at once, up to 50.">
          <input
            v-model.number="concurrency"
            class="form-input status-input"
            type="number"
            min="1"
            max="50"
          />
        </FormField>
        <FormField v-if="estimate !== null" label="Requests" hint="How many this run fires.">
          <input class="form-input status-input" :value="estimate" disabled />
        </FormField>
      </div>

      <hr class="automation-sep" />

      <StopConditionEditor v-model="stopOn" v-model:match="stopMatch" />
    </FormDialog>

    <SnippetsDialog
      v-if="showSnippets"
      :snippets="snippets"
      :label="selectedResult ? `result ${selectedResult.id}` : ''"
      @close="showSnippets = false"
    />
  </div>
</template>
