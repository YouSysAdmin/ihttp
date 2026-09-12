<script setup lang="ts">
// Host overrides: where a name is dialled. The connection moves and
// nothing else does - the Host header, the SNI and the certificate
// check all come from the URL, so the target sees the request it would
// have seen. That is what makes this a replacement for dnsmasq rather
// than a rewrite rule. A scheme on the address is the one thing a hosts
// file cannot do: it decides whether the hop to the target is TLS.
import { computed, ref } from 'vue'
import { projectsApi } from '../../api/projects'
import { apiErrorMessage } from '../../api/client'
import type { HostOverride } from '../../api/types'
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

const overrides = computed<HostOverride[]>(() => projects.settings?.host_overrides ?? [])

const showEditor = ref(false)
const editing = ref<number | null>(null)
const draft = ref<HostOverride>(blank())
const error = ref('')
const saving = ref(false)

function blank(): HostOverride {
  return { host: '', address: '', enabled: true }
}

function startAdd() {
  editing.value = null
  draft.value = blank()
  error.value = ''
  showEditor.value = true
}

function startEdit(i: number) {
  editing.value = i
  draft.value = { ...overrides.value[i]! }
  error.value = ''
  showEditor.value = true
}

async function put(next: HostOverride[]) {
  const res = await projectsApi.putHostOverrides(next)
  projects.setSettings(res.data.settings)
}

async function save() {
  if (saving.value) return
  saving.value = true
  error.value = ''
  const next = overrides.value.map((o) => ({ ...o }))
  if (editing.value === null) next.push({ ...draft.value })
  else next[editing.value] = { ...draft.value }

  try {
    await put(next)
    showEditor.value = false
  } catch (e) {
    error.value = apiErrorMessage(e, 'The override was refused')
  } finally {
    saving.value = false
  }
}

// Turning one off is how a target is compared against the real host, so
// it is one click and the address is kept.
async function toggle(i: number, e: Event) {
  const want = (e.target as HTMLInputElement).checked
  const next = overrides.value.map((o, at) => (at === i ? { ...o, enabled: want } : { ...o }))
  try {
    await put(next)
  } catch (err) {
    ;(e.target as HTMLInputElement).checked = !want
    notify.error(apiErrorMessage(err, 'Could not change the override'))
  }
}

async function remove(i: number) {
  const row = overrides.value[i]!
  const ok = await confirm({
    title: `Dial ${row.host} where DNS says again?`,
    message:
      'New connections go wherever the name resolves. Connections already open are not touched.',
    confirmText: 'Remove',
    variant: 'danger',
  })
  if (!ok) return

  try {
    await put(overrides.value.filter((_, at) => at !== i))
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not remove the override'))
  }
}
</script>

<template>
  <div>
    <PageHeader title="Host overrides">
      <button class="btn btn-primary" :disabled="!projects.active" @click="startAdd">
        Add override
      </button>
    </PageHeader>

    <ProjectGate v-if="projects.loaded && !projects.active" />

    <template v-else-if="projects.active">
      <Notice kind="info" class="mb-4">
        <p>
          A hosts file for this project, so a lab address does not have to go into dnsmasq on every
          machine a client runs on. Only the connection moves: the target still sees the name the
          client asked for in the <code>Host</code> header and in the SNI, and is still asked for
          that name's certificate. A host with an override is reached directly even when the project
          goes out through a proxy.
        </p>
        <p>
          Put <code>http://</code> in front of the address to reach a plain server - a dev server on
          <code>http://127.0.0.1:3000</code> - while the client goes on asking for
          <code>https</code>. Without it the client's own scheme is kept, and pointing an
          <code>https</code> name at a plain port fails the handshake. The log records what the
          client asked for either way.
        </p>
      </Notice>

      <div class="card">
        <div class="card-header">
          <h2>Overrides</h2>
          <span class="header-sub m-0"
            >The first match wins, so put a host above the pattern that would also cover it.</span
          >
        </div>
        <EmptyState
          v-if="overrides.length === 0"
          title="No overrides"
          text="Every name is dialled wherever it resolves, which is what you want until a target moves."
        />
        <div v-else class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th></th>
                <th>Host</th>
                <th>Dialled at</th>
                <th class="text-right"></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(o, i) in overrides" :key="o.host + i" :class="{ 'rule-off': !o.enabled }">
                <td>
                  <input
                    type="checkbox"
                    :checked="o.enabled"
                    :title="o.enabled ? 'On' : 'Off'"
                    @change="toggle(i, $event)"
                  />
                </td>
                <td>
                  <code>{{ o.host }}</code>
                </td>
                <td>
                  <code>{{ o.address }}</code>
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
    </template>

    <FormDialog
      v-if="showEditor"
      :title="editing === null ? 'Add override' : 'Edit override'"
      :error="error"
      :saving="saving"
      @close="showEditor = false"
      @submit="save"
    >
      <FormField
        label="Host"
        hint="A host or a glob, e.g. api.example.com or *.test.local. No port, no scheme, no path."
      >
        <input v-model="draft.host" class="form-input code-font" spellcheck="false" required />
      </FormField>
      <FormField
        label="Dialled at"
        hint="An IP, e.g. 10.0.0.5. Add a port to move that too, and http:// or https:// to decide whether the hop to it is TLS: http://127.0.0.1:3000 reaches a plain dev server."
      >
        <input
          v-model="draft.address"
          class="form-input code-font"
          placeholder="10.0.0.5"
          spellcheck="false"
          required
        />
      </FormField>
      <FormField hint="Turn one off to compare against the real host without retyping the address.">
        <label class="checkbox-label">
          <input v-model="draft.enabled" type="checkbox" />
          <span>Applied</span>
        </label>
      </FormField>
    </FormDialog>
  </div>
</template>
