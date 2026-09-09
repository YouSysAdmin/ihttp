<script setup lang="ts">
// The gRPC schemas of the open project: what is uploaded, and a way to
// add .proto files or descriptor sets. Lives beside the gRPC view, where
// the schemas act.
import { onMounted, ref } from 'vue'
import { schemasApi } from '../api/schemas'
import { apiErrorMessage } from '../api/client'
import type { Schema } from '../api/types'
import { formatClock } from '../composables/formatDate'
import { humanSize } from '../composables/humanSize'
import { useConfirm } from '../composables/useConfirm'
import BaseModal from './BaseModal.vue'
import Notice from './Notice.vue'

const emit = defineEmits<{ (e: 'close'): void; (e: 'changed'): void }>()
const { confirm } = useConfirm()

const schemas = ref<Schema[]>([])
const loading = ref(true)
const busy = ref(false)
const error = ref('')
const picker = ref<HTMLInputElement | null>(null)

async function load() {
  loading.value = true
  try {
    const res = await schemasApi.list()
    schemas.value = res.data.schemas
  } catch (e) {
    error.value = apiErrorMessage(e, 'Could not load the schemas')
  } finally {
    loading.value = false
  }
}

// Files are sent one by one. A .proto that imports another one in the
// same batch fails the first time, so what failed is tried once more
// after the rest landed.
async function upload(e: Event) {
  const input = e.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  if (files.length === 0) return
  busy.value = true
  error.value = ''
  let pending = files
  let stored = 0
  const failed: string[] = []
  for (const round of [0, 1]) {
    const retry: File[] = []
    for (const f of pending) {
      try {
        await schemasApi.upload(f)
        stored++
      } catch (err) {
        if (round === 0) retry.push(f)
        else failed.push(`${f.name}: ${apiErrorMessage(err, 'refused')}`)
      }
    }
    pending = retry
    if (pending.length === 0) break
  }
  if (failed.length) error.value = failed.join('\n')
  busy.value = false
  if (stored > 0) emit('changed')
  await load()
}

async function remove(s: Schema) {
  const ok = await confirm({
    title: `Delete ${s.name}?`,
    message: 'Messages this schema decoded fall back to the schemaless view.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!ok) return
  try {
    await schemasApi.remove(s.id)
    error.value = ''
    emit('changed')
    await load()
  } catch (e) {
    error.value = apiErrorMessage(e, 'Could not delete the schema')
  }
}

onMounted(load)
</script>

<template>
  <BaseModal title="gRPC schemas" size="modal-w720" @close="emit('close')">
    <p class="text-sm text-muted mb-3">
      Upload .proto files, or a descriptor set from
      <code>protoc --descriptor_set_out --include_imports</code>. A .proto that imports another
      needs that file uploaded too, in the same batch or before.
    </p>
    <Notice v-if="error" kind="danger" class="mb-3">
      <p class="pre-line">{{ error }}</p>
    </Notice>

    <div class="flex items-center gap-2 mb-3">
      <button
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="busy"
        @click="picker?.click()"
      >
        {{ busy ? 'Uploading...' : 'Add files' }}
      </button>
      <input
        ref="picker"
        type="file"
        multiple
        accept=".proto,.protoset,.pb,.desc,.bin"
        hidden
        @change="upload"
      />
    </div>

    <p v-if="loading" class="viewer-muted">Loading...</p>
    <p v-else-if="schemas.length === 0" class="viewer-muted">No schemas yet.</p>
    <table v-else class="table">
      <thead>
        <tr>
          <th>File</th>
          <th>Services</th>
          <th>Uploaded</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in schemas" :key="s.id">
          <td>
            <span class="code-font">{{ s.name }}</span>
            <span class="text-xs text-muted ml-2">{{ humanSize(s.size) }}</span>
          </td>
          <td class="code-font text-sm">{{ s.services.join(', ') || '-' }}</td>
          <td class="text-sm">{{ formatClock(s.uploaded_at) }}</td>
          <td class="text-right">
            <button type="button" class="btn btn-secondary btn-sm text-danger" @click="remove(s)">
              Delete
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">Close</button>
    </template>
  </BaseModal>
</template>
