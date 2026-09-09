<script setup lang="ts">
// The request log as a two-pane reader: the list on the left, live, and
// the selected exchange on the right in LogEntryReader. The selection
// lives in the URL so a link to one entry opens straight at it. Saved
// entries live in their own view, out of the log and out of Clear log.
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { reqlogsApi } from '../../api/reqlogs'
import { projectsApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type {
  HostCount,
  LogEntry,
  LogSummary,
  View as ViewModel,
  WsMessageSummary,
} from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useCompareStore } from '../../stores/compare'
import { useConfirm } from '../../composables/useConfirm'
import { useLiveEvents } from '../../composables/useLiveEvents'
import { useRouteSelection } from '../../composables/useRouteSelection'
import { humanSize } from '../../composables/humanSize'
import { useShortcuts } from '../../composables/useShortcuts'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import EmptyState from '../../components/EmptyState.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import SearchBar from '../../components/SearchBar.vue'
import ListRow from '../../components/ListRow.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import DropdownMenu from '../../components/DropdownMenu.vue'
import ViewPicker from '../../components/ViewPicker.vue'
import FormDialog from '../../components/FormDialog.vue'
import FormField from '../../components/FormField.vue'
import LogFilterDialog from '../../components/LogFilterDialog.vue'
import HostList from '../../components/HostList.vue'
import TopDialog from '../../components/TopDialog.vue'
import LogEntryReader from '../../components/LogEntryReader.vue'

const PAGE = 100

// How many rows the list will hold while live events push new ones on
// top. Five pages is far more than anybody scrolls back through by eye,
// and Load older reaches the rest.
const MAX_ROWS = PAGE * 5

// Which of the two lists is on screen. Not to be confused with a
// saved View, which is a whole filter under a name.
type LogTab = 'log' | 'saved'

const projects = useProjectStore()
const notify = useNotificationStore()
const compare = useCompareStore()
const { confirm } = useConfirm()
const { selectedId, select, clear: clearSelection } = useRouteSelection('/logs')

// Whether a project is open is the store's knowledge, kept current by
// the event stream, so the page asks it rather than probing the API.
const gated = computed(() => projects.loaded && !projects.active)

const loading = ref(true)
const loadingMore = ref(false)
const entries = ref<LogSummary[]>([])
const more = ref(false)

// The view, the search and the scope switch are remembered per project,
// so switching back finds them still set.
const filterKey = computed(() => `ihttp_log_filter_${projects.active?.id ?? ''}`)
const tab = ref<LogTab>('log')
const search = ref('')
const onlyInScope = ref(false)

const TABS: TabItem[] = [
  { id: 'log', label: 'Log' },
  { id: 'saved', label: 'Saved' },
]
const saved = computed(() => tab.value === 'saved')

const entry = ref<LogEntry | null>(null)
const entryLoading = ref(false)

// The tags this project has used, offered while adding one.
const knownTags = ref<string[]>([])

async function loadTags() {
  if (!projects.active) return
  try {
    const res = await reqlogsApi.tags()
    knownTags.value = res.data.tags.map((t) => t.name)
  } catch {
    knownTags.value = []
  }
}

// The reader reports the entry as the server now has it. The row takes
// what the summary carries. Saving moves the entry to the other view,
// so its row goes while the reader keeps showing it.
function onEntryUpdated(e: LogEntry) {
  entry.value = e
  if (!!e.saved !== saved.value) {
    entries.value = entries.value.filter((x) => x.id !== e.id)
    return
  }
  entries.value = entries.value.map((x) =>
    x.id === e.id
      ? {
          ...x,
          tags: e.tags,
          color: e.color,
          saved: e.saved,
          messages: Math.max(x.messages ?? 0, e.websocket?.messages ?? 0),
        }
      : x,
  )
}

// The HAR link follows the view, the search and the scope switch.
const filtered = computed(() => !!search.value || onlyInScope.value)
const selection = computed(() => ({
  search: search.value || undefined,
  only_in_scope: onlyInScope.value || undefined,
  saved: saved.value || undefined,
}))
const harUrl = computed(() => reqlogsApi.harUrl(selection.value))
const harLabel = computed(() => (filtered.value ? 'Export filtered HAR' : 'Export HAR'))

function rememberFilter() {
  localStorage.setItem(
    filterKey.value,
    JSON.stringify({
      search: search.value,
      scope: onlyInScope.value,
      tab: tab.value,
      muted: includeMuted.value,
      hosts: showHosts.value,
    }),
  )
}

function recallFilter() {
  try {
    const raw = localStorage.getItem(filterKey.value)
    if (!raw) return
    const v = JSON.parse(raw) as {
      search?: string
      scope?: boolean
      tab?: LogTab
      muted?: boolean
      hosts?: boolean
    }
    search.value = v.search ?? ''
    onlyInScope.value = !!v.scope
    tab.value = v.tab === 'saved' ? 'saved' : 'log'
    includeMuted.value = !!v.muted
    showHosts.value = !!v.hosts
  } catch {
    // A corrupt value is no filter.
  }
}

function params(before?: string) {
  // include_muted belongs to a LIST and not to `selection`: an export
  // and a delete take the filter as written, muted or not.
  return { ...selection.value, before, limit: PAGE, include_muted: includeMuted.value || undefined }
}

// quiet leaves the loading block alone, for a reload the person did not
// ask for: a live list that blanks itself every time traffic arrives is
// unreadable.
async function load(quiet = false) {
  if (!projects.active) return
  if (!quiet) loading.value = true
  inFlight = true
  try {
    const res = await reqlogsApi.list(params())
    entries.value = res.data.entries ?? []
    more.value = res.data.more
  } catch (e) {
    if (!quiet) notify.error(apiErrorMessage(e, 'Failed to load the log'))
  } finally {
    inFlight = false
    if (!quiet) loading.value = false
  }
}

// RELOADS ARE COALESCED. With a filter set only the server can say
// whether a new entry matches, so the list is read again - and a proxy
// under load logs hundreds of requests a second. An event ASKS for a
// reload, and at most one happens per window however many asked.
const RELOAD_WINDOW = 400

let reloadTimer: ReturnType<typeof setTimeout> | null = null
let inFlight = false

function scheduleLoad() {
  if (reloadTimer) return

  reloadTimer = setTimeout(() => {
    reloadTimer = null

    // One at a time: a second read started over the first would race it
    // and could land the older answer last.
    if (inFlight) {
      scheduleLoad()

      return
    }

    void load(true)
  }, RELOAD_WINDOW)
}

async function loadMore() {
  const last = entries.value[entries.value.length - 1]
  if (!last) return
  loadingMore.value = true
  try {
    const res = await reqlogsApi.list(params(last.id))
    entries.value = entries.value.concat(res.data.entries ?? [])
    more.value = res.data.more
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load more'))
  } finally {
    loadingMore.value = false
  }
}

async function loadEntry(id: string) {
  if (!id || !projects.active) {
    entry.value = null
    return
  }
  entryLoading.value = true
  try {
    const res = await reqlogsApi.get(id)
    // A slower answer must not replace what was selected since.
    if (selectedId.value !== id) return
    entry.value = res.data.entry
  } catch (e) {
    if (selectedId.value !== id) return
    entry.value = null
    notify.error(apiErrorMessage(e, 'Failed to load the entry'))
  } finally {
    entryLoading.value = false
  }
}

function applyFilter() {
  rememberFilter()
  void load()
}

// The tab reloads from the click, not from a watcher, so recalling a
// remembered one neither loads twice nor writes back what it just read.
function setTab(v: string) {
  tab.value = v === 'saved' ? 'saved' : 'log'
  applyFilter()
}

// Clear removes what the view shows: the unsaved log, or the saved
// entries, narrowed by the filter when one is set. Saved entries never
// go with the log.
const clearLabel = computed(() => {
  if (filtered.value) return 'Delete filtered'
  return saved.value ? 'Delete all saved' : 'Clear log'
})

async function clear() {
  const what = saved.value ? 'saved entries' : 'logged exchanges'
  const ok = await confirm({
    title: filtered.value ? `Delete the ${what} the filter selects?` : `Delete all ${what}?`,
    message: saved.value
      ? 'What was saved in this project is deleted.'
      : 'Every logged exchange of this project is deleted. Saved entries are not touched.',
    confirmText: filtered.value || saved.value ? 'Delete' : 'Clear',
    variant: 'danger',
  })
  if (!ok) return
  try {
    const res = await reqlogsApi.clear(filtered.value || saved.value ? selection.value : {})
    entries.value = []
    entry.value = null
    clearSelection()
    const n = typeof res.data === 'object' ? res.data.deleted : 0
    notify.success(
      typeof res.data === 'object'
        ? `Deleted ${n} ${n === 1 ? 'entry' : 'entries'}`
        : 'Log cleared',
    )
    void load()
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to clear the log'))
  }
}

// Deleting the selected entry moves the selection to its neighbour, the
// one below or else the one above, so a run of Delete presses walks the
// list instead of leaving it each time.
async function removeEntry(id: string) {
  const ok = await confirm({
    title: 'Delete this entry?',
    message: 'The exchange and any WebSocket messages it carried are deleted.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  const at = entries.value.findIndex((e) => e.id === id)
  const next = entries.value[at + 1] ?? entries.value[at - 1]
  try {
    await reqlogsApi.remove(id)
    entries.value = entries.value.filter((e) => e.id !== id)
    if (entry.value?.id === id) {
      entry.value = null
      if (next) select(next.id)
      else clearSelection()
    }
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not delete the entry'))
  }
}

// Moving through the list with the keyboard. Selecting a row IS opening
// it - the reader shows whatever is selected - so there is no separate
// Enter to open. With nothing selected, either direction takes the
// newest.
function step(by: number) {
  if (entries.value.length === 0) return

  const at = entries.value.findIndex((e) => e.id === selectedId.value)
  if (at < 0) {
    select(entries.value[0]!.id)

    return
  }

  const next = entries.value[at + by]
  if (next) select(next.id)
}

// The filter box, focused from the keyboard. Found in the DOM because
// the box belongs to SearchBar and one shortcut is not a reason to
// thread a ref through it.
function focusFilter() {
  const input = document.querySelector<HTMLInputElement>('.log-search .search-bar input')
  input?.focus()
  input?.select()
}

useShortcuts([
  {
    keys: ['j', 'ArrowDown'],
    label: 'Next entry, newest first',
    run: () => step(1),
  },
  {
    keys: ['k', 'ArrowUp'],
    label: 'Previous entry',
    run: () => step(-1),
  },
  {
    keys: 'f',
    label: 'Focus the filter',
    run: focusFilter,
  },
  {
    keys: ['Delete', 'Backspace'],
    label: 'Delete the selected entry',
    when: () => !!entry.value,
    run: () => {
      if (entry.value) void removeEntry(entry.value.id)
    },
  },
  {
    keys: 'h',
    label: 'Show or hide the host counts',
    run: toggleHosts,
  },
])

// The color a filter names, if it names one, for the color menu. The
// menu replaces that term and leaves the rest of the search alone.
const COLOR_TERM = /\s*\bAND\s+req\.color\s*=\s*\S+|\breq\.color\s*=\s*\S+\s*(AND\s+)?/i

const activeColor = computed(() => {
  const m = /\breq\.color\s*=\s*"?([a-z]+)"?/i.exec(search.value)
  return m ? m[1]!.toLowerCase() : ''
})

function setColor(color: string) {
  const rest = search.value.replace(COLOR_TERM, ' ').trim()
  if (!color) search.value = rest
  else search.value = rest ? `${rest} AND req.color = ${color}` : `req.color = ${color}`
}

// The host counts, and whether the panel showing them is open. Counting
// is a full walk of the log, so it happens when the panel is opened,
// when a host is muted and when Refresh is pressed - never per event.
const showHosts = ref(false)
const hosts = ref<HostCount[]>([])
const hostsLoading = ref(false)
const includeMuted = ref(false)

const HOST_TERM = /\breq\.host\s*=\s*"?([^\s"]+)"?/i

const activeHost = computed(() => {
  const m = HOST_TERM.exec(search.value)

  return m ? m[1]!.toLowerCase() : ''
})

async function loadHosts() {
  if (!projects.active) return
  hostsLoading.value = true
  try {
    hosts.value = (await reqlogsApi.hosts()).data.hosts ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to count the hosts'))
  } finally {
    hostsLoading.value = false
  }
}

function toggleHosts() {
  showHosts.value = !showHosts.value
  rememberFilter()

  if (showHosts.value && hosts.value.length === 0) void loadHosts()
}

// Clicking a host replaces the host term in the filter, the way the
// color menu replaces the color term. Clicking the one already there
// takes it off again, so the same click undoes itself.
function pickHost(host: string) {
  const rest = search.value
    .replace(HOST_TERM, ' ')
    .replace(/\s+(AND|OR)\s*$/i, ' ')
    .trim()

  if (activeHost.value === host) {
    search.value = rest
  } else {
    search.value = rest ? `${rest} AND req.host = ${host}` : `req.host = ${host}`
  }

  applyFilter()
}

// Muting is a project setting, so it travels with the whole request-log
// document the way the dialog's half does.
async function setMuted(host: string, muted: boolean) {
  const current = logSettings.value
  const list = current?.muted_hosts ?? []
  const next = muted ? [...list, host] : list.filter((h) => h !== host)

  try {
    const res = await projectsApi.putRequestLog({
      paused: current?.paused ?? false,
      bypass_out_of_scope: current?.bypass_out_of_scope ?? false,
      ignore_filter: current?.ignore_filter ?? '',
      max_entries: current?.max_entries ?? 0,
      muted_hosts: next,
    })
    projects.setSettings(res.data.settings)
    notify.success(muted ? `${host} muted` : `${host} unmuted`)
    await Promise.all([loadHosts(), load()])
  } catch (e) {
    notify.error(apiErrorMessage(e, 'The change was refused'))
  }
}

// Everything that decides what is in front of you, other than the
// filter query itself, lives in one dialog - see LogFilterDialog for why
// its two halves are kept apart. The dot is on when any of them is set,
// including the project half, so a log going quiet is never a mystery.
const showFilters = ref(false)

const logSettings = computed(() => projects.settings?.request_log ?? null)
const filtersSet = computed(
  () =>
    onlyInScope.value ||
    !!activeColor.value ||
    !!logSettings.value?.bypass_out_of_scope ||
    !!logSettings.value?.ignore_filter ||
    !!logSettings.value?.max_entries ||
    (!!logSettings.value?.muted_hosts?.length && !includeMuted.value),
)

function applyDialog(v: { onlyInScope: boolean; color: string; includeMuted: boolean }) {
  onlyInScope.value = v.onlyInScope
  includeMuted.value = v.includeMuted
  setColor(v.color)
  applyFilter()
}

// Saved views. The picker shows which one the page is on, which is
// decided by what the filter triple actually is, not by remembering a
// click: typing over a view's filter leaves the view behind, and the
// label goes back to All traffic, which is the truth.
const views = computed<ViewModel[]>(() => projects.settings?.views ?? [])

const activeViewId = computed(() => {
  const match = views.value.find(
    (v) =>
      v.query === search.value &&
      !!v.only_in_scope === onlyInScope.value &&
      !!v.saved === saved.value,
  )

  return match?.id ?? ''
})

function pickView(v: ViewModel | null) {
  search.value = v?.query ?? ''
  onlyInScope.value = !!v?.only_in_scope
  tab.value = v?.saved ? 'saved' : 'log'
  applyFilter()
}

// Saving and managing share one dialog: a name for a new view, a list
// for the existing ones. Both write the whole list, since that is what
// the endpoint takes.
const showSaveView = ref(false)
const showManageViews = ref(false)
const viewName = ref('')
const viewError = ref('')
const savingViews = ref(false)

function openSaveView() {
  viewName.value = ''
  viewError.value = ''
  showSaveView.value = true
}

function openManageViews() {
  viewError.value = ''
  showManageViews.value = true
}

async function putViews(next: ViewModel[]) {
  savingViews.value = true
  viewError.value = ''
  try {
    const res = await projectsApi.putViews(next)
    projects.setSettings(res.data.settings)

    return true
  } catch (e) {
    viewError.value = apiErrorMessage(e, 'The views were refused')

    return false
  } finally {
    savingViews.value = false
  }
}

async function saveView() {
  const name = viewName.value.trim()
  if (!name) {
    viewError.value = 'A view needs a name'
    return
  }

  const next: ViewModel[] = [
    ...views.value,
    {
      id: '',
      name,
      query: search.value,
      only_in_scope: onlyInScope.value,
      saved: saved.value,
    },
  ]

  if (await putViews(next)) {
    showSaveView.value = false
    notify.success(`Saved the view ${name}`)
  }
}

async function removeView(id: string) {
  const next = views.value.filter((v) => v.id !== id)
  if (await putViews(next)) notify.success('View deleted')
}

async function renameView(id: string, name: string) {
  const trimmed = name.trim()
  if (!trimmed) return

  const next = views.value.map((v) => (v.id === id ? { ...v, name: trimmed } : v))
  await putViews(next)
}

// The ranking report, opened from the menu: an action, not a mode.
const showTop = ref<'duration' | 'size' | ''>('')

function openTop(by: 'duration' | 'size') {
  showTop.value = by
}

function openFromTop(id: string) {
  select(id)
  showTop.value = ''
}

// Importing a HAR: the menu item clicks a hidden file input, since a
// file picker needs a real input and a styled label in a menu would not
// read as a menu item.
const harInput = ref<HTMLInputElement | null>(null)
const importing = ref(false)

function pickHar() {
  harInput.value?.click()
}

async function importHar(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]

  // Cleared right away, so picking the same file twice still fires.
  input.value = ''

  if (!file) return

  importing.value = true
  try {
    const res = await reqlogsApi.importHar(file)
    const n = res.data.entries
    notify.success(`Imported ${n} ${n === 1 ? 'entry' : 'entries'}, tagged imported`)
    void load()
    void loadTags()
  } catch (err) {
    notify.error(apiErrorMessage(err, 'The file was refused'))
  } finally {
    importing.value = false
  }
}

// Pause is a mode, so it stays on the page rather than going in the
// dialog: the proxy keeps forwarding and the log stops growing. The
// checkbox is put back by hand when the server refuses, so it never
// shows a state the project is not in.
const pausing = ref(false)

async function setPaused(e: Event) {
  const box = e.target as HTMLInputElement
  const want = box.checked
  const cur = logSettings.value
  if (!cur) return

  pausing.value = true
  try {
    const res = await projectsApi.putRequestLog({ ...cur, paused: want })
    projects.setSettings(res.data.settings)
    notify.success(want ? 'Recording paused' : 'Recording resumed')
  } catch (err) {
    box.checked = !want
    notify.error(apiErrorMessage(err, 'Could not change recording'))
  } finally {
    pausing.value = false
  }
}

// Live updates. A new request goes on top of the plain log. With a
// filter, or in the saved view, the server decides what matches, so the
// list is reloaded or left alone. A response fills in the row it belongs
// to, a mark or a save reshapes it.
useLiveEvents({
  'reqlog.request': (s: LogSummary) => {
    if (saved.value) return
    if (filtered.value) {
      scheduleLoad()

      return
    }
    if (entries.value.some((e) => e.id === s.id)) return

    const next = [s, ...entries.value]

    // Capped, or a session left watching a busy proxy grows this list
    // and the DOM with it without limit. The tail is dropped and `more`
    // says so: Load older brings it back from the id still on screen.
    if (next.length > MAX_ROWS) {
      entries.value = next.slice(0, MAX_ROWS)
      more.value = true

      return
    }

    entries.value = next
  },
  'reqlog.response': (s: LogSummary) => {
    entries.value = entries.value.map((e) => (e.id === s.id ? { ...e, ...s } : e))
    if (s.id === selectedId.value) void loadEntry(s.id)
  },
  'reqlog.updated': (s: LogSummary) => {
    if (!!s.saved !== saved.value) {
      entries.value = entries.value.filter((e) => e.id !== s.id)
    } else if (!entries.value.some((e) => e.id === s.id)) {
      scheduleLoad()
    } else {
      entries.value = entries.value.map((e) =>
        e.id === s.id ? { ...e, tags: s.tags ?? [], color: s.color ?? '', saved: s.saved } : e,
      )
    }
    if (s.id === selectedId.value) void loadEntry(s.id)
  },
  'reqlog.cleared': () => {
    entry.value = null
    void load()
  },
  // An import elsewhere - another tab, or the API - adds rows this list
  // has never seen, so it is read again rather than patched.
  'reqlog.imported': () => {
    void load()
    void loadTags()
  },
  // A message on the open connection counts at once, on the row and on
  // the tab, without waiting for the server to write the count.
  'reqlog.message': (m: WsMessageSummary) => {
    entries.value = entries.value.map((e) =>
      e.id === m.entry_id
        ? { ...e, websocket: true, messages: Math.max(e.messages ?? 0, m.seq) }
        : e,
    )
    const cur = entry.value
    if (cur && cur.id === m.entry_id) {
      const ws = cur.websocket ?? { messages: 0 }
      entry.value = { ...cur, websocket: { ...ws, messages: Math.max(ws.messages, m.seq) } }
    }
  },
  'reqlog.deleted': (d: { ids: string[]; trimmed?: number }) => {
    // Retention names no ids: it dropped the oldest, which this page may
    // never have loaded, so the list is read again.
    if (d.trimmed) {
      scheduleLoad()

      return
    }

    const gone = new Set(d.ids)
    entries.value = entries.value.filter((e) => !gone.has(e.id))
    if (entry.value && gone.has(entry.value.id)) {
      entry.value = null
      clearSelection()
    }
  },
})

// A different project is a different log.
watch(
  () => projects.active?.id,
  () => {
    entries.value = []
    entry.value = null
    recallFilter()
    void load()
    void loadEntry(selectedId.value)
    void loadTags()
    if (showHosts.value) void loadHosts()
  },
)

onBeforeUnmount(() => {
  if (reloadTimer) clearTimeout(reloadTimer)
})

onMounted(() => {
  recallFilter()
  void load()
  void loadTags()
  void loadEntry(selectedId.value)
  if (showHosts.value) void loadHosts()
})

watch(selectedId, (id) => void loadEntry(id))
</script>

<template>
  <div class="reader-page">
    <!-- Two things on the controls side: what is pinned, and one menu.
         Everything that narrows the list is on the filter row below, so
         the header does not grow a control per feature. -->
    <PageHeader title="Request log">
      <!-- The label is the part that gets cut short, so the Unpin button
           is always in view however long the URL. -->
      <span
        v-if="compare.pinned"
        class="badge badge-info compare-pin"
        :title="compare.pinned.label"
      >
        <span class="compare-pin-label">A: {{ compare.pinned.label }}</span>
        <button
          type="button"
          class="compare-pin-clear"
          aria-label="Unpin"
          title="Unpin"
          @click="compare.unpin()"
        >
          x
        </button>
      </span>
      <label v-if="logSettings" class="checkbox-label">
        <input
          type="checkbox"
          :checked="logSettings.paused"
          :disabled="pausing"
          @change="setPaused"
        />
        Pause recording
      </label>
      <DropdownMenu variant="btn btn-secondary" label="Log">
        <a
          class="menu-item"
          :class="{ disabled: gated || entries.length === 0 }"
          :href="harUrl"
          download
          >{{ harLabel }}</a
        >
        <button type="button" class="menu-item" :disabled="gated || importing" @click="pickHar">
          Import HAR
          <span class="menu-hint">adds to this log</span>
        </button>
        <hr class="menu-sep" />
        <button type="button" class="menu-item" :disabled="gated" @click="openTop('duration')">
          Slowest exchanges
          <span class="menu-hint">of the whole filter</span>
        </button>
        <button type="button" class="menu-item" :disabled="gated" @click="openTop('size')">
          Largest responses
        </button>
        <hr class="menu-sep" />
        <button
          type="button"
          class="menu-item text-danger"
          :disabled="gated || entries.length === 0"
          @click="clear"
        >
          {{ clearLabel }}
        </button>
      </DropdownMenu>
      <input
        ref="harInput"
        type="file"
        accept=".har,.json,application/json"
        hidden
        @change="importHar"
      />
    </PageHeader>

    <FormDialog
      v-if="showSaveView"
      title="Save this filter as a view"
      size="modal-w440"
      :error="viewError"
      :saving="savingViews"
      @close="showSaveView = false"
      @submit="saveView"
    >
      <FormField label="Name" hint="How it reads in the picker.">
        <input v-model="viewName" class="form-input" maxlength="60" required />
      </FormField>
      <p class="text-sm text-muted">
        Keeps the filter, the scope switch and which of the two lists is open. Saved on the project,
        so it survives a restart and travels with a settings-only export.
      </p>
      <p v-if="search" class="text-sm text-muted code-font">{{ search }}</p>
      <p v-else class="text-sm text-muted">No filter: this view shows everything.</p>
    </FormDialog>

    <FormDialog
      v-if="showManageViews"
      title="Views"
      size="modal-w560"
      :error="viewError"
      :saving="savingViews"
      submit-label="Done"
      cancel-label="Close"
      @close="showManageViews = false"
      @submit="showManageViews = false"
    >
      <div v-for="v in views" :key="v.id" class="view-row">
        <input
          class="form-input"
          :value="v.name"
          maxlength="60"
          @change="renameView(v.id, ($event.target as HTMLInputElement).value)"
        />
        <span class="view-query code-font" :title="v.query">{{ v.query || 'no filter' }}</span>
        <button
          type="button"
          class="btn btn-secondary btn-sm text-danger"
          :disabled="savingViews"
          @click="removeView(v.id)"
        >
          Delete
        </button>
      </div>
    </FormDialog>

    <TopDialog
      v-if="showTop"
      :selection="selection"
      :filtered="filtered"
      :by="showTop"
      @open="openFromTop"
      @close="showTop = ''"
    />

    <LogFilterDialog
      v-if="showFilters"
      :only-in-scope="onlyInScope"
      :color="activeColor"
      :include-muted="includeMuted"
      :settings="logSettings"
      @close="showFilters = false"
      @apply="applyDialog"
    />

    <ProjectGate v-if="gated" />

    <template v-else>
      <div class="log-search mb-4">
        <TabStrip :model-value="tab" :tabs="TABS" @update:model-value="setTab" />
        <ViewPicker
          :views="views"
          :active-id="activeViewId"
          @pick="pickView"
          @save="openSaveView"
          @manage="openManageViews"
        />
        <SearchBar v-model="search" @submit="applyFilter" />
        <!-- Not "Filters": the bar's own submit button beside it says
             "Filter", and two buttons a word apart is a coin toss. -->
        <button class="btn btn-secondary" :disabled="gated" @click="showFilters = true">
          Filter options<span v-if="filtersSet" class="filter-dot" aria-hidden="true"></span>
        </button>
        <button
          class="btn btn-secondary"
          :disabled="gated"
          :aria-expanded="showHosts"
          @click="toggleHosts"
        >
          Hosts
        </button>
      </div>

      <div class="card reader-split">
        <div class="list-pane">
          <HostList
            v-if="showHosts"
            :hosts="hosts"
            :loading="hostsLoading"
            :active-host="activeHost"
            :include-muted="includeMuted"
            @pick="pickHost"
            @mute="setMuted"
            @refresh="loadHosts"
          />
          <LoadingBlock v-if="loading" />
          <EmptyState
            v-else-if="entries.length === 0"
            :title="saved ? 'Nothing saved yet' : 'Nothing logged yet'"
            :text="
              filtered
                ? 'No entry matches the filter.'
                : saved
                  ? 'Save an entry from its Actions menu and it stays here through Clear log.'
                  : 'Point a browser at the proxy and requests appear here as they pass.'
            "
          />
          <template v-else>
            <ListRow
              v-for="e in entries"
              :key="e.id"
              :method="e.method"
              :url="e.url"
              :time="e.created_at"
              :status-code="e.status_code"
              :status-reason="e.status"
              :selected="e.id === selectedId"
              :color="e.color"
              :tags="e.tags"
              @select="select(e.id)"
            >
              <template #meta>
                <span v-if="e.duration_ms != null && e.status_code">{{ e.duration_ms }} ms</span>
                <span v-if="e.graphql" class="gql-chip">
                  {{ e.graphql.type || 'graphql' }}
                  <template v-if="e.graphql.name"> {{ e.graphql.name }}</template>
                </span>
                <span v-if="e.graphql?.errors" class="text-danger">
                  {{ e.graphql.errors }} err
                </span>
                <span v-if="e.tunnel"
                  >tunnel {{ humanSize(e.request_size) }} up /
                  {{ humanSize(e.response_size) }} down</span
                >
                <span v-else-if="e.response_size">{{ humanSize(e.response_size) }}</span>
                <span v-if="e.websocket">ws {{ e.messages ?? 0 }}</span>
                <span v-else-if="e.streamed">stream</span>
              </template>
            </ListRow>
            <div v-if="more" class="list-more">
              <button class="btn btn-secondary btn-sm" :disabled="loadingMore" @click="loadMore">
                {{ loadingMore ? 'Loading...' : 'Load older' }}
              </button>
            </div>
          </template>
        </div>

        <div class="reader-pane">
          <LoadingBlock v-if="entryLoading && !entry" />
          <EmptyState
            v-else-if="!entry"
            title="Select an entry"
            text="The request and its response are shown here."
          />
          <LogEntryReader
            v-else
            :entry="entry"
            :known-tags="knownTags"
            @update:entry="onEntryUpdated"
            @delete="removeEntry"
            @tags-changed="loadTags"
          />
        </div>
      </div>
    </template>
  </div>
</template>
