<script setup lang="ts">
// Scope: the rules that say which requests are the work at hand. A
// request is in scope when ANY rule matches, and a rule matches when
// EVERY expression it carries does.
import { computed, ref } from 'vue'
import { projectsApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { ScopeRule } from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useNotificationStore } from '../../stores/notification'
import { useConfirm } from '../../composables/useConfirm'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import EmptyState from '../../components/EmptyState.vue'
import FormDialog from '../../components/FormDialog.vue'
import FormField from '../../components/FormField.vue'
import Notice from '../../components/Notice.vue'

const projects = useProjectStore()
const notify = useNotificationStore()
const { confirm } = useConfirm()

const rules = computed<ScopeRule[]>(() => projects.settings?.scope ?? [])

// The other half of "what may this project see": hosts relayed without
// being decrypted at all. It lives here because it is the same
// question as the scope - what this project is allowed to look at -
// only answered by refusing rather than by narrowing.
const noDecrypt = computed<string[]>(() => projects.settings?.no_decrypt ?? [])

const showAdd = ref(false)
const editing = ref<number | null>(null)
const draft = ref<ScopeRule>(blank())
const error = ref('')
const saving = ref(false)

function blank(): ScopeRule {
  return { url: '', header_key: '', header_value: '', body: '' }
}

function startAdd() {
  editing.value = null
  draft.value = blank()
  error.value = ''
  showAdd.value = true
}

function startEdit(i: number) {
  editing.value = i
  draft.value = { ...rules.value[i]! }
  error.value = ''
  showAdd.value = true
}

async function put(next: ScopeRule[]) {
  const res = await projectsApi.putScope(next)
  projects.setSettings(res.data.settings)
}

async function saveDraft() {
  if (saving.value) return
  saving.value = true
  error.value = ''
  const next = rules.value.map((r) => ({ ...r }))
  if (editing.value === null) next.push({ ...draft.value })
  else next[editing.value] = { ...draft.value }

  try {
    await put(next)
    showAdd.value = false
  } catch (e) {
    error.value = apiErrorMessage(e, 'The rule was refused')
  } finally {
    saving.value = false
  }
}

const showHost = ref(false)
const hostDraft = ref('')
const hostError = ref('')
const savingHost = ref(false)

function startAddHost() {
  hostDraft.value = ''
  hostError.value = ''
  showHost.value = true
}

async function putHosts(next: string[]) {
  const res = await projectsApi.putNoDecrypt(next)
  projects.setSettings(res.data.settings)
}

async function saveHost() {
  if (savingHost.value) return
  savingHost.value = true
  hostError.value = ''

  try {
    await putHosts([...noDecrypt.value, hostDraft.value])
    showHost.value = false
  } catch (e) {
    hostError.value = apiErrorMessage(e, 'The host was refused')
  } finally {
    savingHost.value = false
  }
}

async function removeHost(i: number) {
  const ok = await confirm({
    title: `Decrypt ${noDecrypt.value[i]} again?`,
    message: 'New connections to it are read as usual. Connections already open are not touched.',
    confirmText: 'Remove',
  })
  if (!ok) return

  try {
    await putHosts(noDecrypt.value.filter((_, at) => at !== i))
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not remove the host'))
  }
}

async function remove(i: number) {
  const ok = await confirm({
    title: 'Remove this scope rule?',
    message: 'What only this rule matched is out of scope from now on.',
    confirmText: 'Remove',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await put(rules.value.filter((_, at) => at !== i))
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not remove the rule'))
  }
}
</script>

<template>
  <div>
    <PageHeader title="Scope">
      <button class="btn btn-secondary" :disabled="!projects.active" @click="startAddHost">
        Add host
      </button>
      <button class="btn btn-primary" :disabled="!projects.active" @click="startAdd">
        Add rule
      </button>
    </PageHeader>

    <ProjectGate v-if="projects.loaded && !projects.active" />

    <template v-else-if="projects.active">
      <Notice kind="info" class="mb-4">
        <p>
          Every field is a regular expression and an empty one is not tested. Within a rule every
          filled field must match, and a request is in scope when any rule matches. Header names
          match regardless of case, values and URLs keep theirs.
        </p>
      </Notice>

      <div class="card">
        <div class="card-header">
          <h2>Rules</h2>
          <span class="header-sub m-0"
            >Logging outside the scope is decided in the request log's settings.</span
          >
        </div>
        <EmptyState
          v-if="rules.length === 0"
          title="No rules"
          text="With no rules, nothing is in scope and the scope switches show nothing."
        />
        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>URL</th>
                <th>Header name</th>
                <th>Header value</th>
                <th>Body</th>
                <th class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(r, i) in rules" :key="i">
                <td>
                  <code v-if="r.url">{{ r.url }}</code
                  ><span v-else class="text-muted">-</span>
                </td>
                <td>
                  <code v-if="r.header_key">{{ r.header_key }}</code
                  ><span v-else class="text-muted">-</span>
                </td>
                <td>
                  <code v-if="r.header_value">{{ r.header_value }}</code
                  ><span v-else class="text-muted">-</span>
                </td>
                <td>
                  <code v-if="r.body">{{ r.body }}</code
                  ><span v-else class="text-muted">-</span>
                </td>
                <td>
                  <div class="table-actions">
                    <button class="btn btn-secondary btn-sm" @click="startEdit(i)">Edit</button>
                    <button class="btn btn-secondary btn-sm text-danger" @click="remove(i)">
                      Remove
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
      <div class="card mt-4">
        <div class="card-header">
          <h2>Do not decrypt</h2>
          <span class="header-sub m-0"
            >Host patterns relayed as they are, for a client that pins its server's certificate or
            traffic that has no business being in a log.</span
          >
        </div>
        <div class="card-body">
          <Notice kind="warning">
            <p>
              Nothing inside these connections is seen, so they cannot be intercepted, changed by a
              rule or searched. The log still gets a CONNECT row with the host, how long it stood
              and how many bytes went each way. A pattern is a host, and <code>*</code> spans a
              whole name: <code>*.example.com</code> covers <code>a.b.example.com</code> too. The
              port is not part of it.
            </p>
          </Notice>
        </div>
        <EmptyState
          v-if="noDecrypt.length === 0"
          title="Every host is decrypted"
          text="Which is what you want until a client refuses to connect at all."
        />
        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Host</th>
                <th class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(host, i) in noDecrypt" :key="host + i">
                <td>
                  <code>{{ host }}</code>
                </td>
                <td>
                  <div class="table-actions">
                    <button class="btn btn-secondary btn-sm text-danger" @click="removeHost(i)">
                      Remove
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>

    <FormDialog
      v-if="showHost"
      title="Add a host to leave alone"
      :error="hostError"
      :saving="savingHost"
      @close="showHost = false"
      @submit="saveHost"
    >
      <FormField
        label="Host"
        hint="A host or a glob, e.g. bank.example.com or *.apple.com. No port, no scheme, no path."
      >
        <input v-model="hostDraft" class="form-input code-font" spellcheck="false" />
      </FormField>
    </FormDialog>

    <FormDialog
      v-if="showAdd"
      :title="editing === null ? 'Add rule' : 'Edit rule'"
      :error="error"
      :saving="saving"
      @close="showAdd = false"
      @submit="saveDraft"
    >
      <FormField
        label="URL matches"
        hint="Tested against the whole URL, e.g. ^https://api\.example\.com/"
      >
        <input v-model="draft.url" class="form-input code-font" spellcheck="false" />
      </FormField>
      <div class="form-row">
        <FormField label="Header name matches" hint="Case does not matter, e.g. ^cookie$">
          <input v-model="draft.header_key" class="form-input code-font" spellcheck="false" />
        </FormField>
        <FormField
          label="Header value matches"
          hint="Case matters. Both set means both on one header."
        >
          <input v-model="draft.header_value" class="form-input code-font" spellcheck="false" />
        </FormField>
      </div>
      <FormField label="Body matches">
        <input v-model="draft.body" class="form-input code-font" spellcheck="false" />
      </FormField>
    </FormDialog>
  </div>
</template>
