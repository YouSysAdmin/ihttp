<script setup lang="ts">
// The instance's ways out: add, edit, delete, test.
//
// The rule this component exists to keep: a NAME is what the operator
// sees everywhere, and the host beside it carries no credential at all -
// not even a masked one, since the summary the server sends has the
// userinfo stripped. The stored URL is fetched only when the editor
// opens, and only for the one being edited. That is the risk we accept,
// in one place, where it was asked for.
import { onMounted, ref } from 'vue'
import { upstreamsApi } from '../api/upstreams'
import { apiErrorMessage } from '../api/client'
import type { UpstreamSummary } from '../api/types'
import { useNotificationStore } from '../stores/notification'
import { useConfirm } from '../composables/useConfirm'
import EmptyState from './EmptyState.vue'
import FormDialog from './FormDialog.vue'
import FormField from './FormField.vue'
import Notice from './Notice.vue'

const notify = useNotificationStore()
const { confirm } = useConfirm()

const servers = ref<UpstreamSummary[]>([])
const loading = ref(true)

const emit = defineEmits<{ (e: 'changed'): void }>()

async function load() {
  try {
    servers.value = (await upstreamsApi.list()).data.servers ?? []
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not read the proxy list'))
  } finally {
    loading.value = false
  }
}

onMounted(load)

const showEditor = ref(false)
const editingID = ref('')
const draft = ref({ name: '', url: '', bypass: '' })
const error = ref('')
const saving = ref(false)

function startAdd() {
  editingID.value = ''
  draft.value = { name: '', url: '', bypass: '' }
  error.value = ''
  showEditor.value = true
}

async function startEdit(id: string) {
  error.value = ''

  try {
    const srv = (await upstreamsApi.get(id)).data.server
    editingID.value = srv.id
    draft.value = { name: srv.name, url: srv.url, bypass: (srv.bypass ?? []).join(', ') }
    showEditor.value = true
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not read that proxy'))
  }
}

async function save() {
  saving.value = true
  error.value = ''

  try {
    await upstreamsApi.save({
      ...(editingID.value ? { id: editingID.value } : {}),
      name: draft.value.name,
      url: draft.value.url,
      bypass: draft.value.bypass
        .split(',')
        .map((b) => b.trim())
        .filter(Boolean),
    })

    showEditor.value = false
    await load()
    emit('changed')
    notify.success('Proxy saved')
  } catch (e) {
    error.value = apiErrorMessage(e, 'The proxy was refused')
  } finally {
    saving.value = false
  }
}

async function remove(row: UpstreamSummary) {
  const ok = await confirm({
    title: `Delete ${row.name}?`,
    message: 'A project still pointing at it goes out the instance default way instead.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await upstreamsApi.remove(row.id)
    await load()
    emit('changed')
    notify.success('Proxy deleted')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not delete it'))
  }
}

// The answer stays on the page: it is a measurement to read, not an
// event to acknowledge.
const testing = ref('')
const result = ref<{ kind: 'success' | 'danger' | 'info'; text: string } | null>(null)

async function test(id: string, label: string) {
  testing.value = id || 'default'
  result.value = null

  try {
    const { result: r, error: failed } = (await upstreamsApi.test(id)).data

    if (failed) {
      result.value = {
        kind: 'danger',
        text: `${label} could not reach ${r.target} after ${r.took_ms} ms: ${failed}`,
      }
    } else if (r.direct) {
      result.value = {
        kind: 'info',
        text: `${r.target} is on ${label}'s no-proxy list, so this proved nothing about the proxy. It answered ${r.status} in ${r.took_ms} ms.`,
      }
    } else {
      result.value = {
        kind: 'success',
        text: `${label} reached ${r.target} in ${r.took_ms} ms, which answered ${r.status}.`,
      }
    }
  } catch (e) {
    result.value = { kind: 'danger', text: apiErrorMessage(e, 'The test failed') }
  } finally {
    testing.value = ''
  }
}

defineExpose({ reload: load })
</script>

<template>
  <div>
    <EmptyState
      v-if="!loading && servers.length === 0"
      title="No proxies yet"
      text="Add one and a project can pick it, so switching project switches the way out."
    />

    <table v-else-if="servers.length" class="table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Proxy</th>
          <th>No proxy</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in servers" :key="row.id">
          <td>
            <span class="cell-title">{{ row.name }}</span>
            <span v-if="row.has_credentials" class="badge badge-neutral ml-2">signed in</span>
          </td>
          <td>
            <code>{{ row.host }}</code>
          </td>
          <td>
            <code v-if="row.bypass?.length">{{ row.bypass.join(', ') }}</code>
            <span v-else class="text-muted">localhost only</span>
          </td>
          <td>
            <div class="table-actions">
              <button
                class="btn btn-secondary btn-sm"
                :disabled="testing === row.id"
                @click="test(row.id, row.name)"
              >
                {{ testing === row.id ? 'Testing...' : 'Test' }}
              </button>
              <button class="btn btn-secondary btn-sm" @click="startEdit(row.id)">Edit</button>
              <button class="btn btn-secondary btn-sm text-danger" @click="remove(row)">
                Delete
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <Notice v-if="result" :kind="result.kind" class="mt-3">
      <p>{{ result.text }}</p>
    </Notice>

    <div class="mt-3">
      <button class="btn btn-primary btn-sm" @click="startAdd">Add proxy</button>
    </div>

    <FormDialog
      v-if="showEditor"
      :title="editingID ? 'Edit proxy' : 'Add proxy'"
      size="modal-w560"
      :error="error"
      :saving="saving"
      @close="showEditor = false"
      @submit="save"
    >
      <FormField label="Name" hint="What the lists and the project picker show.">
        <input v-model="draft.name" class="form-input" maxlength="60" required />
      </FormField>
      <FormField
        label="Proxy URL"
        hint="http://, https:// or socks5://. Credentials go in as http://user:pass@proxy:3128 and are stored as typed - this form is the only place they are shown."
      >
        <input
          v-model="draft.url"
          class="form-input code-font"
          placeholder="http://proxy.corp:3128"
          spellcheck="false"
          required
        />
      </FormField>
      <FormField
        label="No proxy"
        hint="Host globs reached without this proxy, comma separated - the NO_PROXY of this entry. localhost is always in it."
      >
        <input
          v-model="draft.bypass"
          class="form-input code-font"
          placeholder="*.internal, api.local"
          spellcheck="false"
        />
      </FormField>
    </FormDialog>
  </div>
</template>
