<script setup lang="ts">
// Rules: what the proxy does to matching traffic on your behalf - answer
// with a mock instead of the backend, serve a local file, point one host
// at another, add or drop a header, change a status, edit a body, add a
// delay, allow CORS. Rules run in order, and one exchange may match
// several - a mock does not end the chain, the other rules still apply.
import { computed, onMounted, ref, watch } from 'vue'
import { projectsApi } from '../../api/projects'
import { rulesApi } from '../../api/rules'
import { apiErrorMessage } from '../../api/client'
import type {
  Header,
  Replacement,
  Rule,
  RuleAction,
  RuleActionType,
  RuleVar,
} from '../../api/types'
import { METHODS } from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import EmptyState from '../../components/EmptyState.vue'
import FormDialog from '../../components/FormDialog.vue'
import FormField from '../../components/FormField.vue'
import HeaderEditor from '../../components/HeaderEditor.vue'
import ReplacementEditor from '../../components/ReplacementEditor.vue'
import CodeEditor from '../../components/CodeEditor.vue'
import FilterHelpBox from '../../components/FilterHelpBox.vue'
import Notice from '../../components/Notice.vue'
import { bodyLanguage } from '../../composables/http'
import { useLiveEvents } from '../../composables/useLiveEvents'
import { formatClock } from '../../composables/formatDate'

const projects = useProjectStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const gated = computed(() => projects.loaded && !projects.active)
const rules = computed<Rule[]>(() => projects.settings?.rules ?? [])

// What the capture rules have read. Runtime state of the proxy rather
// than a setting, so it is asked for rather than read off the project,
// and it arrives live as captures happen.
const captured = ref<RuleVar[]>([])

async function loadCaptured() {
  if (!projects.active) {
    captured.value = []

    return
  }

  try {
    captured.value = (await rulesApi.variables()).data.variables ?? []
  } catch {
    // Nothing captured is the same as nothing to show.
    captured.value = []
  }
}

async function forgetCaptured() {
  try {
    await rulesApi.clearVariables()
    captured.value = []
    notify.success('Captured values forgotten')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not clear the values'))
  }
}

useLiveEvents({
  'rules.captured': (v: RuleVar) => {
    captured.value = [v, ...captured.value.filter((x) => x.name !== v.name)]
  },
  // Another project's values are not this one's, and the server has
  // already forgotten them.
  'project.opened': () => void loadCaptured(),
  'project.closed': () => {
    captured.value = []
  },
})

onMounted(loadCaptured)
watch(() => projects.active?.id, loadCaptured)

// What each action is called and what it does, for the type picker and
// the table. One list, so the two cannot disagree.
const ACTIONS: { type: RuleActionType; label: string; hint: string }[] = [
  {
    type: 'mock',
    label: 'Mock response',
    hint: 'Answer with this status, headers and body. The backend is not called.',
  },
  {
    type: 'map_local',
    label: 'Serve local file',
    hint: 'Answer with a file from disk. The content type follows the extension.',
  },
  {
    type: 'rewrite_url',
    label: 'Rewrite URL',
    hint: 'Replace the part of the URL that "URL matches" found before the request leaves - point a production host at localhost.',
  },
  {
    type: 'set_request_header',
    label: 'Set request headers',
    hint: 'Add or overwrite these headers on the way out. Seen by the backend and in the request log, not in the browser.',
  },
  {
    type: 'remove_request_header',
    label: 'Remove request headers',
    hint: 'Drop these headers on the way out.',
  },
  {
    type: 'set_response_header',
    label: 'Set response headers',
    hint: 'Add or overwrite these headers on the way back.',
  },
  {
    type: 'remove_response_header',
    label: 'Remove response headers',
    hint: 'Drop these headers on the way back.',
  },
  {
    type: 'set_status',
    label: 'Set response status',
    hint: 'Make a working endpoint answer 500, or a failing one answer 200.',
  },
  {
    type: 'replace_body',
    label: 'Replace in response body',
    hint: 'Regular expressions over the body, applied in order, with $1-style groups in the replacement.',
  },
  {
    type: 'replace_request_body',
    label: 'Replace in request body',
    hint: 'The same, on the body the client sent, before it leaves.',
  },
  {
    type: 'delay',
    label: 'Delay request',
    hint: 'Wait before the request leaves, to see how the app copes with a slow backend.',
  },
  {
    type: 'delay_response',
    label: 'Delay response',
    hint: 'Wait before the answer reaches the client. The backend is called at once, so this is a slow link rather than a slow backend.',
  },
  {
    type: 'block',
    label: 'Block',
    hint: 'Answer with this status instead of calling the backend - 502 when it is not set. For making an endpoint fail on purpose: analytics, a third party, a dependency you want to test without.',
  },
  {
    type: 'throttle',
    label: 'Throttle response',
    hint: 'Pace the answer at this many bytes a second. Works on a streamed body too, unlike a delay, so an event stream can be made to trickle.',
  },
  {
    type: 'allow_cors',
    label: 'Allow CORS',
    hint: 'Answer preflights and add permissive CORS headers, for a frontend on one origin and an API on another.',
  },
  {
    type: 'capture',
    label: 'Capture a value',
    hint: 'Read a value out of the exchange and remember it under a name, for later rules to put into a request as ${name}. This is what carries a token from a sign-in into the requests that need it - every other action writes the text you typed, this one writes what the traffic said.',
  },
]

function actionLabel(type: RuleActionType) {
  return ACTIONS.find((a) => a.type === type)?.label ?? type
}

// The header list of an action, with the single header of an older rule
// folded in, so the table and the editor read one shape.
function headersOf(a: RuleAction): Header[] {
  const list = (a.headers ?? []).map((h) => ({ ...h }))
  if (a.header) list.unshift({ name: a.header, value: a.value ?? '' })
  return list
}

// The replacements of a body action, likewise.
function replacementsOf(a: RuleAction): Replacement[] {
  const list = (a.replacements ?? []).map((r) => ({ ...r }))
  if (a.pattern) list.unshift({ pattern: a.pattern, replace: a.replace ?? '' })
  return list
}

const HEADER_ACTIONS: RuleActionType[] = [
  'set_request_header',
  'remove_request_header',
  'set_response_header',
  'remove_response_header',
]
const REMOVE_ACTIONS: RuleActionType[] = ['remove_request_header', 'remove_response_header']
const BODY_ACTIONS: RuleActionType[] = ['replace_body', 'replace_request_body']

// The editor. A copy of the rule, written back as a whole list on save.
const showEditor = ref(false)
const editing = ref<number | null>(null)
const draft = ref<Rule>(blank())
const draftHeaders = ref<Header[]>([])
const draftReplacements = ref<Replacement[]>([])
const error = ref('')
const saving = ref(false)

function blank(): Rule {
  return {
    id: '',
    enabled: true,
    name: '',
    url: '',
    method: '',
    filter: '',
    action: { type: 'mock', status: 200, body: '' },
  }
}

function startAdd() {
  editing.value = null
  draft.value = blank()
  draftHeaders.value = []
  draftReplacements.value = []
  error.value = ''
  showEditor.value = true
}

function startEdit(i: number) {
  editing.value = i
  const r = rules.value[i]!
  draft.value = { ...r, action: { ...r.action } }
  draftHeaders.value = headersOf(r.action)
  draftReplacements.value = replacementsOf(r.action)
  error.value = ''
  showEditor.value = true
}

const hint = computed(() => ACTIONS.find((a) => a.type === draft.value.action.type)?.hint ?? '')

const bodyLang = computed(() =>
  bodyLanguage(
    draftHeaders.value.find((h) => h.name.toLowerCase() === 'content-type')?.value,
    draft.value.action.body ?? '',
  ),
)

async function put(next: Rule[]) {
  const res = await projectsApi.putRules(next)
  projects.setSettings(res.data.settings)
}

async function saveDraft() {
  saving.value = true
  error.value = ''

  const rule: Rule = { ...draft.value, action: { ...draft.value.action } }
  // The lists replace the single fields of an older rule outright.
  delete rule.action.header
  delete rule.action.value
  if (rule.action.type === 'mock' || isHeaderAction.value) {
    rule.action.headers = draftHeaders.value.filter((h) => h.name)
  }
  if (isBodyAction.value) {
    rule.action.replacements = draftReplacements.value.filter((r) => r.pattern)
    delete rule.action.pattern
    delete rule.action.replace
  }

  const next = rules.value.map((r) => ({ ...r }))
  if (editing.value === null) next.push(rule)
  else next[editing.value] = rule

  try {
    await put(next)
    showEditor.value = false
  } catch (e) {
    error.value = apiErrorMessage(e, 'The rule was refused')
  } finally {
    saving.value = false
  }
}

async function toggle(i: number, ev: Event) {
  const box = ev.target as HTMLInputElement
  const next = rules.value.map((r, at) => (at === i ? { ...r, enabled: !r.enabled } : r))
  try {
    await put(next)
  } catch (e) {
    // The store did not change, so the box is put back by hand.
    box.checked = rules.value[i]?.enabled ?? false
    notify.error(apiErrorMessage(e, 'Could not update the rule'))
  }
}

async function remove(i: number) {
  const ok = await confirm({
    title: `Delete ${rules.value[i]!.name || 'this rule'}?`,
    message: 'The proxy stops applying it at once.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await put(rules.value.filter((_, at) => at !== i))
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not delete the rule'))
  }
}

async function move(i: number, dir: -1 | 1) {
  const j = i + dir
  if (j < 0 || j >= rules.value.length) return
  const next = rules.value.map((r) => ({ ...r }))
  ;[next[i], next[j]] = [next[j]!, next[i]!]
  try {
    await put(next)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not reorder the rules'))
  }
}

const isHeaderAction = computed(() => HEADER_ACTIONS.includes(draft.value.action.type))
const isRemoveAction = computed(() => REMOVE_ACTIONS.includes(draft.value.action.type))
const isBodyAction = computed(() => BODY_ACTIONS.includes(draft.value.action.type))
</script>

<template>
  <div>
    <PageHeader title="Rules">
      <button class="btn btn-primary" :disabled="gated" @click="startAdd">Add rule</button>
    </PageHeader>

    <ProjectGate v-if="gated" />

    <template v-else>
      <Notice kind="info" class="mb-4">
        <p>
          A rule applies to a request when its method, its URL pattern and its filter all agree -
          each one left empty is no condition. Rules run in order and every matching rule applies: a
          mock, a local file or a block answers instead of the backend, and the header, delay and
          rewrite rules around it still run, so their order does not matter. Switch a rule off to
          keep it without applying it.
        </p>
      </Notice>

      <div class="card">
        <EmptyState
          v-if="rules.length === 0"
          title="No rules"
          text="Mock a response, serve a local file, rewrite a host, add a header, add a delay, allow CORS."
        />
        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th></th>
                <th>Name</th>
                <th>Matches</th>
                <th>Does</th>
                <th class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(r, i) in rules" :key="r.id || i" :class="{ 'rule-off': !r.enabled }">
                <td>
                  <input
                    type="checkbox"
                    :checked="r.enabled"
                    :title="r.enabled ? 'On' : 'Off'"
                    @change="toggle(i, $event)"
                  />
                </td>
                <td>
                  <span class="cell-title">{{ r.name || actionLabel(r.action.type) }}</span>
                </td>
                <td>
                  <!-- A rule with only a filter matches what the filter
                       says, so "every request" would be a lie. -->
                  <code v-if="r.method" class="mr-1">{{ r.method }}</code>
                  <code v-if="r.url">{{ r.url }}</code>
                  <code v-if="r.filter" class="rule-filter" :title="r.filter">{{ r.filter }}</code>
                  <span v-if="!r.method && !r.url && !r.filter" class="text-muted">
                    every request
                  </span>
                </td>
                <td>
                  <span class="badge badge-neutral mr-2">{{ actionLabel(r.action.type) }}</span>
                </td>
                <td>
                  <div class="table-actions">
                    <button
                      class="btn btn-secondary btn-sm"
                      :disabled="i === 0"
                      title="Move up"
                      @click="move(i, -1)"
                    >
                      &uarr;
                    </button>
                    <button
                      class="btn btn-secondary btn-sm"
                      :disabled="i === rules.length - 1"
                      title="Move down"
                      @click="move(i, 1)"
                    >
                      &darr;
                    </button>
                    <button class="btn btn-secondary btn-sm" @click="startEdit(i)">Edit</button>
                    <button class="btn btn-secondary btn-sm text-danger" @click="remove(i)">
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="captured.length" class="card mt-4">
        <div class="card-header">
          <h2>Captured values</h2>
          <button class="btn btn-secondary btn-sm" @click="forgetCaptured">Forget them</button>
        </div>
        <div class="card-body">
          <p class="text-sm text-muted mb-3">
            What the capture rules have read, for <code>${name}</code> to put back. These live while
            the project is open and are never stored, so nothing here travels with an export.
          </p>
          <div class="table-wrapper">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Value</th>
                  <th>From</th>
                  <th>Read at</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="v in captured" :key="v.name">
                  <td>
                    <code>${{ '{' }}{{ v.name }}{{ '}' }}</code>
                  </td>
                  <td class="captured-value code-font" :title="v.value">{{ v.value }}</td>
                  <td class="text-xs text-muted">
                    <code>{{ v.from }}</code>
                    <template v-if="v.rule"> - {{ v.rule }}</template>
                  </td>
                  <td class="text-xs text-muted">{{ formatClock(v.at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </template>

    <FormDialog
      v-if="showEditor"
      :title="editing === null ? 'Add rule' : 'Edit rule'"
      size="modal-w720"
      :error="error"
      :saving="saving"
      @close="showEditor = false"
      @submit="saveDraft"
    >
      <div class="form-row">
        <FormField label="Name" hint="Optional, for the table.">
          <input v-model="draft.name" class="form-input" placeholder="Mock the users API" />
        </FormField>
        <FormField label="Method" hint="Empty is any method.">
          <select v-model="draft.method" class="form-select">
            <option value="">Any</option>
            <option v-for="m in METHODS" :key="m" :value="m">{{ m }}</option>
          </select>
        </FormField>
      </div>
      <FormField
        label="URL matches"
        hint="A regular expression over the whole URL. Empty matches every request."
      >
        <input
          v-model="draft.url"
          class="form-input code-font"
          placeholder="^https://api\.example\.com/users"
          spellcheck="false"
        />
      </FormField>

      <FormField
        label="Filter"
        hint="Narrows further, in the same language as the log: what a URL regex cannot say. Decided on the request alone, so res.* fields are refused. Empty is no extra condition."
      >
        <textarea
          v-model="draft.filter"
          class="form-textarea form-textarea--filter"
          placeholder="req.header.authorization exists AND req.size > 1kb"
          spellcheck="false"
        ></textarea>
      </FormField>
      <FilterHelpBox subject="request" />

      <FormField label="Action" :hint="hint">
        <select v-model="draft.action.type" class="form-select">
          <option v-for="a in ACTIONS" :key="a.type" :value="a.type">{{ a.label }}</option>
        </select>
      </FormField>

      <template v-if="draft.action.type === 'mock'">
        <FormField label="Status">
          <input
            v-model.number="draft.action.status"
            class="form-input status-input"
            type="number"
            min="100"
            max="599"
          />
        </FormField>
        <label class="form-label">Headers</label>
        <HeaderEditor v-model="draftHeaders" />
        <label class="form-label mt-4">Body</label>
        <CodeEditor v-model="draft.action.body" :language="bodyLang" placeholder='{"ok": true}' />
        <p class="form-hint">Without a Content-Type header, JSON is detected from the body.</p>
      </template>

      <FormField
        v-if="draft.action.type === 'map_local'"
        label="File path"
        hint="Absolute path on the machine running ihttp."
      >
        <input
          v-model="draft.action.path"
          class="form-input code-font"
          placeholder="/Users/me/fixtures/users.json"
          spellcheck="false"
        />
      </FormField>

      <template v-if="draft.action.type === 'rewrite_url'">
        <FormField
          label="Replace with"
          hint='What the matched part of the URL becomes. With "URL matches" ^https://api\.example\.com and this set to http://localhost:3000, a request to https://api.example.com/users goes to http://localhost:3000/users. $1, $2 insert captured groups.'
        >
          <input
            v-model="draft.action.replace"
            class="form-input code-font"
            placeholder="http://localhost:3000"
            spellcheck="false"
          />
        </FormField>
        <FormField
          label="Pattern to replace"
          hint='Optional. Only when the part to replace differs from the part "URL matches" selects: for example match ^https://api\.example\.com/v2/ but replace just /v2/ with /v3/.'
        >
          <input
            v-model="draft.action.pattern"
            class="form-input code-font"
            placeholder="Empty: the URL match above"
            spellcheck="false"
          />
        </FormField>
      </template>

      <template v-if="isHeaderAction">
        <label class="form-label">{{ isRemoveAction ? 'Headers to remove' : 'Headers' }}</label>
        <HeaderEditor v-model="draftHeaders" :names-only="isRemoveAction" />
        <p class="form-hint">One per row. A blank row at the end adds another.</p>
      </template>

      <template v-if="isBodyAction">
        <label class="form-label">Replacements</label>
        <ReplacementEditor v-model="draftReplacements" />
        <p class="form-hint">
          Applied top to bottom, each over the result of the one before. Go regular expression
          syntax, $1 and $2 insert captured groups.
        </p>
      </template>

      <FormField v-if="draft.action.type === 'block'" label="Status" hint="Empty or 0 answers 502.">
        <input
          v-model.number="draft.action.status"
          class="form-input status-input"
          type="number"
          min="0"
          max="599"
          placeholder="502"
        />
      </FormField>

      <FormField
        v-if="draft.action.type === 'throttle'"
        label="Bytes a second"
        hint="A 3G-ish link is about 50000, a slow one 8000."
      >
        <input
          v-model.number="draft.action.rate_bps"
          class="form-input status-input"
          type="number"
          min="1"
          step="1"
        />
      </FormField>

      <template v-if="draft.action.type === 'capture'">
        <FormField
          label="Read this field"
          hint="A field of the filter language: res.body, res.header.set-cookie, req.query.id. The whole exchange is available, so a req.* field works too."
        >
          <input
            v-model="draft.action.from"
            class="form-input code-font"
            spellcheck="false"
            placeholder="res.header.set-cookie"
          />
        </FormField>
        <FormField
          label="Narrow it (optional)"
          hint="A regular expression over the value. Its first group when it has one, the whole match when it does not. Empty takes the field as it stands."
        >
          <input
            v-model="draft.action.pattern"
            class="form-input code-font"
            spellcheck="false"
            placeholder="session=([^;]+)"
          />
        </FormField>
        <FormField
          label="Remember it as"
          hint="Letters, digits and underscores. Later rules write ${this} in a header, a URL, a mock body or a body replacement."
        >
          <input
            v-model="draft.action.name"
            class="form-input code-font"
            spellcheck="false"
            placeholder="token"
          />
        </FormField>
      </template>

      <FormField v-if="draft.action.type === 'set_status'" label="Status">
        <input
          v-model.number="draft.action.status"
          class="form-input status-input"
          type="number"
          min="100"
          max="599"
        />
      </FormField>

      <FormField
        v-if="draft.action.type === 'delay' || draft.action.type === 'delay_response'"
        label="Delay, milliseconds"
      >
        <input
          v-model.number="draft.action.delay_ms"
          class="form-input status-input"
          type="number"
          min="1"
        />
      </FormField>
    </FormDialog>
  </div>
</template>
