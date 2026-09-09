<script setup lang="ts">
// The intercept queue: what is waiting on the left, the selected item
// on the right as an editor, with Forward and Drop. A request can also
// ask for its response to be held, whatever the project setting says.
import { computed, onMounted, ref, watch } from 'vue'
import { interceptApi } from '../../api/intercept'
import { projectsApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { Header, InterceptItem } from '../../api/types'
import { METHODS } from '../../api/types'
import { useInterceptStore } from '../../stores/intercept'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useRouteSelection } from '../../composables/useRouteSelection'
import { useShortcuts } from '../../composables/useShortcuts'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import EmptyState from '../../components/EmptyState.vue'
import ListRow from '../../components/ListRow.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import MessageEditor from '../../components/MessageEditor.vue'
import Notice from '../../components/Notice.vue'
import FormDialog from '../../components/FormDialog.vue'
import FormField from '../../components/FormField.vue'
import FilterHelpBox from '../../components/FilterHelpBox.vue'

const queue = useInterceptStore()
const projects = useProjectStore()
const notify = useNotificationStore()
const { selectedId, select, clear: clearSelection } = useRouteSelection('/intercept')

const gated = computed(() => projects.loaded && !projects.active)

const item = computed<InterceptItem | null>(
  () => queue.items.find((x) => x.id === selectedId.value) ?? null,
)

const intercept = computed(() => projects.settings?.intercept ?? null)
const nothingEnabled = computed(
  () =>
    intercept.value !== null &&
    !intercept.value.requests_enabled &&
    !intercept.value.responses_enabled,
)

// The editable copy. Rebuilt when the selection changes, never while
// the person is typing into it.
const method = ref('GET')
const url = ref('')
const headers = ref<Header[]>([])
const body = ref('')
const statusCode = ref(200)
const statusText = ref('')
const holdResponse = ref<'default' | 'yes' | 'no'>('default')
const busy = ref(false)

// A binary body is shown and passed through untouched: the JSON copy has
// been coerced to text and editing it would corrupt the bytes.
const binary = computed(() =>
  item.value?.kind === 'request'
    ? !!item.value.request.body_binary
    : !!item.value?.response?.body_binary,
)
const rawUrl = computed(() =>
  item.value ? `/api/intercept/items/${item.value.id}/body/${item.value.kind}` : '',
)

function loadEditor(it: InterceptItem | null) {
  if (!it) return
  if (it.kind === 'request') {
    method.value = it.request.method
    url.value = it.request.url
    headers.value = it.request.headers.map((h) => ({ ...h }))
    body.value = it.request.body
    holdResponse.value = 'default'
  } else if (it.response) {
    statusCode.value = it.response.status_code
    statusText.value = it.response.status
    headers.value = it.response.headers.map((h) => ({ ...h }))
    body.value = it.response.body
  }
}

watch(item, (next, prev) => {
  if (next?.id !== prev?.id || next?.kind !== prev?.kind) loadEditor(next)
})

onMounted(async () => {
  await queue.fetchAll()
  loadEditor(item.value)
})

// The queue moves under the reader: when the selected item is answered
// elsewhere or by the client giving up, step to the next one.
watch(
  () => queue.items,
  (items) => {
    if (selectedId.value && !items.some((x) => x.id === selectedId.value)) {
      const next = items[0]
      if (next) select(next.id)
      else clearSelection()
    }
  },
)

async function forward() {
  if (!item.value) return
  busy.value = true
  try {
    if (item.value.kind === 'request') {
      await interceptApi.forwardRequest(item.value.id, {
        method: method.value,
        url: url.value,
        headers: headers.value,
        body: binary.value ? null : body.value,
        intercept_response: holdResponse.value === 'default' ? null : holdResponse.value === 'yes',
      })
    } else {
      await interceptApi.forwardResponse(item.value.id, {
        status_code: Number(statusCode.value),
        status: statusText.value,
        headers: headers.value,
        body: binary.value ? null : body.value,
      })
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to forward'))
  } finally {
    busy.value = false
  }
}

async function drop() {
  if (!item.value) return
  busy.value = true
  try {
    if (item.value.kind === 'request') await interceptApi.dropRequest(item.value.id)
    else await interceptApi.dropResponse(item.value.id)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to drop'))
  } finally {
    busy.value = false
  }
}

// Forward and drop from the keyboard, with a modifier: these send or
// destroy somebody's request, and a bare letter over an editor that has
// just lost focus is how the wrong one goes. The modifier is also what
// lets them fire from inside the editor, which is where the cursor is
// on this page.
//
// Moving through the queue is j and k, the same as the log.
function step(by: number) {
  const items = queue.items
  if (items.length === 0) return

  const at = items.findIndex((x) => x.id === selectedId.value)
  if (at < 0) {
    select(items[0]!.id)

    return
  }

  const next = items[at + by]
  if (next) select(next.id)
}

useShortcuts([
  {
    keys: 'mod+Enter',
    label: 'Forward the selected item',
    when: () => !!item.value && !busy.value,
    run: () => void forward(),
  },
  {
    keys: 'mod+Backspace',
    label: 'Drop the selected item',
    when: () => !!item.value && !busy.value,
    run: () => void drop(),
  },
  { keys: ['j', 'ArrowDown'], label: 'Next item in the queue', run: () => step(1) },
  { keys: ['k', 'ArrowUp'], label: 'Previous item', run: () => step(-1) },
])

// The filters, edited here where they act. The dialog carries a copy of
// the stored settings so a refused filter leaves the page as it was.
const showFilters = ref(false)

// Which half the help describes follows the field that has focus, so a
// person typing a response filter reads about res.* fields.
const helpFor = ref<string>('request')
const helpTabs: TabItem[] = [
  { id: 'request', label: 'Request fields' },
  { id: 'response', label: 'Response fields' },
]
const helpSubject = computed(() => (helpFor.value === 'response' ? 'response' : 'request'))

const filterForm = ref({ request_filter: '', response_filter: '' })
const filterError = ref('')
const savingFilters = ref(false)

function openFilters() {
  filterForm.value = {
    request_filter: intercept.value?.request_filter ?? '',
    response_filter: intercept.value?.response_filter ?? '',
  }
  filterError.value = ''
  showFilters.value = true
}

async function saveFilters() {
  if (!intercept.value) return
  savingFilters.value = true
  filterError.value = ''
  try {
    const res = await projectsApi.putIntercept({ ...intercept.value, ...filterForm.value })
    projects.setSettings(res.data.settings)
    showFilters.value = false
    notify.success('Filters saved')
  } catch (e) {
    filterError.value = apiErrorMessage(e, 'The filter was refused')
  } finally {
    savingFilters.value = false
  }
}

const filtersSet = computed(
  () => !!(intercept.value?.request_filter || intercept.value?.response_filter),
)

async function toggle(which: 'requests_enabled' | 'responses_enabled', ev: Event) {
  if (!intercept.value) return
  const box = ev.target as HTMLInputElement
  try {
    const res = await projectsApi.putIntercept({
      ...intercept.value,
      [which]: !intercept.value[which],
    })
    projects.setSettings(res.data.settings)
  } catch (e) {
    // The store did not change, so the box is put back by hand.
    box.checked = intercept.value?.[which] ?? false
    notify.error(apiErrorMessage(e, 'Failed to update the setting'))
  }
}
</script>

<template>
  <div class="reader-page">
    <PageHeader title="Intercept">
      <template v-if="intercept">
        <label class="checkbox-label">
          <input
            type="checkbox"
            :checked="intercept.requests_enabled"
            @change="toggle('requests_enabled', $event)"
          />
          Hold requests
        </label>
        <label class="checkbox-label">
          <input
            type="checkbox"
            :checked="intercept.responses_enabled"
            @change="toggle('responses_enabled', $event)"
          />
          Hold responses
        </label>
      </template>
      <button class="btn btn-secondary" :disabled="!intercept" @click="openFilters">
        Filters<span v-if="filtersSet" class="filter-dot" aria-hidden="true"></span>
      </button>
    </PageHeader>

    <FormDialog
      v-if="showFilters"
      title="Intercept filters"
      size="modal-w720"
      :error="filterError"
      :saving="savingFilters"
      @close="showFilters = false"
      @submit="saveFilters"
    >
      <p class="mb-3 text-sm text-muted">
        Only what matches a filter is held. An empty filter holds everything that is switched on. A
        long filter can be broken over several lines, a line break reads as a space.
      </p>
      <FormField
        label="Request filter"
        hint="Decides which requests wait for you before they are sent."
      >
        <textarea
          v-model="filterForm.request_filter"
          class="form-textarea form-textarea--filter"
          placeholder='req.method != GET AND req.url =~ "example\.com"'
          spellcheck="false"
          @focus="helpFor = 'request'"
        ></textarea>
      </FormField>
      <FormField
        label="Response filter"
        hint="Decides which responses wait for you before they reach the client."
      >
        <textarea
          v-model="filterForm.response_filter"
          class="form-textarea form-textarea--filter"
          placeholder='res.body contains "full migration"'
          spellcheck="false"
          @focus="helpFor = 'response'"
        ></textarea>
      </FormField>

      <FilterHelpBox :subject="helpSubject">
        <TabStrip v-model="helpFor" :tabs="helpTabs" />
      </FilterHelpBox>
    </FormDialog>

    <ProjectGate v-if="gated" />

    <template v-else>
      <Notice v-if="nothingEnabled && queue.items.length === 0" kind="info" class="mb-4">
        <p>
          Nothing is held right now. Switch on requests or responses above, and set a filter to hold
          only what matters.
        </p>
      </Notice>

      <div class="card reader-split">
        <div class="list-pane">
          <EmptyState
            v-if="queue.items.length === 0"
            title="Nothing waiting"
            text="A held request or response appears here the moment the proxy sees it."
          />
          <ListRow
            v-for="it in queue.items"
            v-else
            :key="it.kind + it.id"
            :method="it.request.method"
            :url="it.request.url"
            :time="it.received_at"
            :status-code="it.kind === 'response' ? it.response?.status_code : undefined"
            :badge="it.kind === 'request' ? 'request' : ''"
            badge-kind="warning"
            :selected="it.id === selectedId"
            @select="select(it.id)"
          >
            <template #meta>
              <span>{{
                it.kind === 'request' ? 'waiting to be sent' : 'waiting to be returned'
              }}</span>
            </template>
          </ListRow>
        </div>

        <div class="reader-pane">
          <EmptyState
            v-if="!item"
            title="Select an item"
            text="Edit it here, then forward it on or drop it."
          />
          <div v-else class="exchange">
            <div class="exchange-head exchange-head--bar">
              <span class="badge" :class="item.kind === 'request' ? 'badge-warning' : 'badge-info'">
                {{ item.kind === 'request' ? 'Request held' : 'Response held' }}
              </span>
              <span class="exchange-url code-font truncate" :title="item.request.url">
                {{ item.request.method }} {{ item.request.url }}
              </span>
              <div class="ml-auto flex gap-2">
                <button class="btn btn-secondary btn-sm" :disabled="busy" @click="drop">
                  Drop
                </button>
                <button class="btn btn-primary btn-sm" :disabled="busy" @click="forward">
                  Forward
                </button>
              </div>
            </div>

            <div class="editor-scroll">
              <template v-if="item.kind === 'request'">
                <div class="editor-line">
                  <select v-model="method" class="form-select method-select">
                    <option v-for="m in METHODS" :key="m" :value="m">{{ m }}</option>
                  </select>
                  <input v-model="url" class="form-input code-font" spellcheck="false" />
                </div>
                <label class="form-label mt-4">Then</label>
                <select v-model="holdResponse" class="form-select then-select">
                  <option value="default">Response: follow the project setting</option>
                  <option value="yes">Response: hold it too</option>
                  <option value="no">Response: let it through</option>
                </select>
              </template>
              <template v-else>
                <div class="editor-line">
                  <input
                    v-model.number="statusCode"
                    class="form-input status-input"
                    type="number"
                    min="100"
                    max="599"
                  />
                  <input
                    v-model="statusText"
                    class="form-input"
                    placeholder="Reason phrase (optional)"
                  />
                </div>
              </template>

              <MessageEditor
                v-model:headers="headers"
                v-model:body="body"
                :binary="binary"
                :raw-url="rawUrl"
              />
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
