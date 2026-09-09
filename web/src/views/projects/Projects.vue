<script setup lang="ts">
// Projects: the list, which one is open, and the two acts that change
// that. Creating and opening are two visible steps on purpose.
import { onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import { apiErrorMessage } from '../../api/client'
import { projectsApi } from '../../api/projects'
import { upstreamsApi } from '../../api/upstreams'
import type { UpstreamSummary } from '../../api/types'
import { UPSTREAM_DIRECT } from '../../api/types'
import { formatDate } from '../../composables/formatDate'
import PageHeader from '../../components/PageHeader.vue'
import EmptyState from '../../components/EmptyState.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import FormDialog from '../../components/FormDialog.vue'
import FormField from '../../components/FormField.vue'
import DropdownMenu from '../../components/DropdownMenu.vue'
import Notice from '../../components/Notice.vue'

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const loading = ref(true)
const showCreate = ref(route.query.create === '1')
const name = ref('')
const nameError = ref('')
const saving = ref(false)
const busyId = ref('')
const importing = ref(false)
const picker = ref<HTMLInputElement | null>(null)

// Import reads the file the browser was given and posts it as it is.
// The new project lands in the list through the event stream too.
async function importProject(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  importing.value = true
  try {
    const res = await projectsApi.importFile(file)
    projects.onCreated(res.data.project)
    notify.success(`Imported ${res.data.project.name}`)
  } catch (err) {
    notify.error(apiErrorMessage(err, 'Could not import the project'))
  } finally {
    importing.value = false
  }
}

onMounted(async () => {
  try {
    await Promise.all([projects.fetchAll(), loadWays()])
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load projects'))
  } finally {
    loading.value = false
  }
})

watch(showCreate, (open) => {
  if (!open) {
    name.value = ''
    nameError.value = ''
    if (route.query.create) router.replace('/projects')
  }
})

async function create() {
  if (saving.value) return
  saving.value = true
  nameError.value = ''
  try {
    const p = await projects.create(name.value)
    showCreate.value = false
    notify.success(`Created ${p.name}`)
    await open(p.id)
  } catch (e) {
    nameError.value = apiErrorMessage(e, 'Could not create the project')
  } finally {
    saving.value = false
  }
}

async function open(id: string) {
  busyId.value = id
  try {
    const p = await projects.open(id)
    notify.success(`Opened ${p.name}`)
    router.push('/logs')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not open the project'))
  } finally {
    busyId.value = ''
  }
}

async function close() {
  try {
    await projects.close()
    notify.info('Project closed - the proxy forwards without logging')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not close the project'))
  }
}

async function remove(id: string, projectName: string) {
  const ok = await confirm({
    title: `Delete ${projectName}?`,
    message:
      'Its request log, sender history, automation jobs and settings go with it. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await projects.remove(id)
    notify.success(`Deleted ${projectName}`)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not delete the project'))
  }
}

// The instance's ways out, so a row can show a name rather than an id.
// Read once: the list is edited on Settings, and a project page that
// polled it would be guessing at when.
const ways = ref<UpstreamSummary[]>([])
const savingUpstream = ref(false)

async function loadWays() {
  try {
    ways.value = (await upstreamsApi.list()).data.servers ?? []
  } catch {
    // Not fatal: without the list a row falls back to showing the id,
    // and the picker still offers the default and direct.
    ways.value = []
  }
}

function wayName(choice?: string) {
  if (!choice) return 'Instance default'
  if (choice === UPSTREAM_DIRECT) return 'Direct'

  return ways.value.find((w) => w.id === choice)?.name ?? 'a proxy this instance does not have'
}

async function setUpstream(e: Event) {
  const select = e.target as HTMLSelectElement
  const want = select.value
  const before = projects.active?.settings.upstream ?? ''

  savingUpstream.value = true
  try {
    const res = await projectsApi.putUpstream(want)
    projects.setSettings(res.data.settings)
    notify.success(`This project now goes out through ${wayName(want).toLowerCase()}`)
  } catch (err) {
    select.value = before
    notify.error(apiErrorMessage(err, 'Could not change the way out'))
  } finally {
    savingUpstream.value = false
  }
}
</script>

<template>
  <div>
    <PageHeader title="Projects">
      <button v-if="projects.active" class="btn btn-secondary" @click="close">
        Close {{ projects.active.name }}
      </button>
      <button class="btn btn-secondary" :disabled="importing" @click="picker?.click()">
        {{ importing ? 'Importing...' : 'Import' }}
      </button>
      <input
        ref="picker"
        type="file"
        accept=".json,application/json"
        hidden
        @change="importProject"
      />
      <button class="btn btn-primary" @click="showCreate = true">New project</button>
    </PageHeader>

    <Notice v-if="!loading && !projects.active" kind="info" class="mb-4">
      <p>
        No project is open. The proxy still forwards traffic, but nothing is logged or intercepted
        until one is.
      </p>
    </Notice>

    <LoadingBlock v-if="loading" />

    <div v-else class="card">
      <EmptyState
        v-if="projects.projects.length === 0"
        title="No projects yet"
        text="A project holds a request log, a sender history, a scope and intercept settings."
      />
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Goes out through</th>
              <th>Created</th>
              <th>Updated</th>
              <th class="text-right"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in projects.projects" :key="p.id">
              <td>
                <div class="flex items-center gap-2">
                  <span class="cell-title">{{ p.name }}</span>
                  <span v-if="p.is_active" class="badge badge-success">open</span>
                </div>
              </td>
              <td>
                <!-- Settings are written for the OPEN project only, so
                     the closed ones show their choice and say how to
                     change it rather than offering a control that
                     cannot work. -->
                <select
                  v-if="p.is_active"
                  class="form-select upstream-select"
                  :value="p.settings.upstream ?? ''"
                  :disabled="savingUpstream"
                  @change="setUpstream($event)"
                >
                  <option value="">Instance default</option>
                  <option :value="UPSTREAM_DIRECT">Direct</option>
                  <option v-for="u in ways" :key="u.id" :value="u.id">{{ u.name }}</option>
                </select>
                <span v-else :title="'Open the project to change this'">
                  {{ wayName(p.settings.upstream) }}
                </span>
              </td>
              <td>{{ formatDate(p.created_at) }}</td>
              <td>{{ formatDate(p.updated_at) }}</td>
              <td>
                <div class="table-actions">
                  <button
                    v-if="!p.is_active"
                    class="btn btn-secondary btn-sm"
                    :disabled="busyId === p.id"
                    @click="open(p.id)"
                  >
                    Open
                  </button>
                  <button v-else class="btn btn-secondary btn-sm" @click="close">Close</button>
                  <DropdownMenu label="Export">
                    <a class="menu-item" :href="projectsApi.exportUrl(p.id)" download>
                      Everything
                      <span class="menu-hint">settings and traffic</span>
                    </a>
                    <a class="menu-item" :href="projectsApi.exportUrl(p.id, true)" download>
                      Settings only
                      <span class="menu-hint">no captured traffic</span>
                    </a>
                  </DropdownMenu>
                  <button
                    class="btn btn-secondary btn-sm text-danger"
                    :disabled="p.is_active"
                    :title="p.is_active ? 'Close the project before deleting it' : 'Delete'"
                    @click="remove(p.id, p.name)"
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <FormDialog
      v-if="showCreate"
      title="New project"
      size="modal-w440"
      submit-label="Create and open"
      :saving="saving"
      @close="showCreate = false"
      @submit="create"
    >
      <FormField label="Name" :error="nameError" hint="Up to 64 characters.">
        <input
          v-model="name"
          class="form-input"
          required
          maxlength="64"
          autofocus
          placeholder="acme-bug-bounty"
        />
      </FormField>
    </FormDialog>
  </div>
</template>
