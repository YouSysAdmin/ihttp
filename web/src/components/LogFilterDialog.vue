<script setup lang="ts">
// One dialog for every reason an exchange is not in front of you. Two
// sections, because the two halves have different reach and mixing them
// up silently is how a persistent project setting gets changed by
// someone who meant to narrow their own view:
//
//   - what this view shows - the scope switch and the color mark, this
//     browser only, remembered per project beside the filter query,
//   - what the proxy logs - the ignore filter and the out-of-scope
//     switch, a project setting that holds for everyone who opens it.
//
// Save applies both and only writes the project half when it changed,
// so opening the dialog to tick a color never reports a settings save.
import { computed, ref } from 'vue'
import { projectsApi } from '../api/projects'
import { apiErrorMessage } from '../api/client'
import type { RequestLogSettings } from '../api/types'
import { useProjectStore } from '../stores/project'
import { useNotificationStore } from '../stores/notification'
import ColorSwatches from './ColorSwatches.vue'
import FilterHelpBox from './FilterHelpBox.vue'
import FormDialog from './FormDialog.vue'
import FormField from './FormField.vue'

const props = defineProps<{
  // What the view currently shows.
  onlyInScope: boolean
  color: string
  // Whether the view is showing entries from muted hosts.
  includeMuted: boolean
  // The project's stored log settings, null until they have loaded.
  settings: RequestLogSettings | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'apply', v: { onlyInScope: boolean; color: string; includeMuted: boolean }): void
}>()

const projects = useProjectStore()
const notify = useNotificationStore()

// A copy, so a refused filter leaves the page as it was.
const draft = ref({
  onlyInScope: props.onlyInScope,
  color: props.color,
  includeMuted: props.includeMuted,
  bypass_out_of_scope: props.settings?.bypass_out_of_scope ?? false,
  ignore_filter: props.settings?.ignore_filter ?? '',
  max_entries: props.settings?.max_entries ?? 0,
})

const error = ref('')
const saving = ref(false)

const settingsChanged = computed(
  () =>
    draft.value.bypass_out_of_scope !== (props.settings?.bypass_out_of_scope ?? false) ||
    draft.value.ignore_filter !== (props.settings?.ignore_filter ?? '') ||
    Number(draft.value.max_entries) !== (props.settings?.max_entries ?? 0),
)

async function submit() {
  if (settingsChanged.value) {
    saving.value = true
    error.value = ''
    try {
      const res = await projectsApi.putRequestLog({
        // Pause is a mode, toggled on the page, not edited here. It
        // still has to travel: the PUT replaces the whole document.
        paused: props.settings?.paused ?? false,
        bypass_out_of_scope: draft.value.bypass_out_of_scope,
        ignore_filter: draft.value.ignore_filter,
        max_entries: Number(draft.value.max_entries) || 0,

        // Which hosts are muted is edited from the Hosts panel, one
        // click per host. It still has to travel: the PUT replaces the
        // whole document.
        muted_hosts: props.settings?.muted_hosts ?? [],
      })
      projects.setSettings(res.data.settings)
      notify.success('Log settings saved')
    } catch (e) {
      error.value = apiErrorMessage(e, 'The settings were refused')
      return
    } finally {
      saving.value = false
    }
  }

  emit('apply', {
    onlyInScope: draft.value.onlyInScope,
    color: draft.value.color,
    includeMuted: draft.value.includeMuted,
  })
  emit('close')
}
</script>

<template>
  <FormDialog
    title="Log filters"
    size="modal-w720"
    :error="error"
    :saving="saving"
    submit-label="Apply"
    @close="emit('close')"
    @submit="submit"
  >
    <p class="mb-3 text-sm text-muted">
      The filter query in the bar narrows the list too. This is everything else that decides what
      you see.
    </p>

    <h3 class="dialog-section">What this view shows</h3>
    <p class="mb-3 text-sm text-muted">
      Yours alone, remembered for this project. Nothing is deleted and nothing stops being logged.
    </p>
    <div class="form-group">
      <label class="checkbox-label">
        <input v-model="draft.onlyInScope" type="checkbox" />
        Only entries in scope
      </label>
    </div>
    <div v-if="settings?.muted_hosts?.length" class="form-group">
      <!-- The hint sits beside the label, not inside it, so the label
           stays one line. -->
      <label class="checkbox-label">
        <input v-model="draft.includeMuted" type="checkbox" />
        Show muted hosts
      </label>
      <p class="form-hint">
        {{ settings.muted_hosts.length }} muted: <code>{{ settings.muted_hosts.join(', ') }}</code
        >. A muted host is hidden from this list and nothing else - it is still captured, still
        exported, and found by a filter when this is ticked. Mute and unmute from the Hosts panel.
      </p>
    </div>
    <FormField label="Color mark" hint="Shows only the entries carrying this mark.">
      <ColorSwatches v-model="draft.color" />
    </FormField>

    <h3 class="dialog-section">What the proxy logs</h3>
    <p class="mb-3 text-sm text-muted">
      A project setting: it holds for everyone who opens this project, and what it drops is never
      written, so it cannot be got back by clearing a filter.
    </p>
    <div class="form-group">
      <label class="checkbox-label">
        <input v-model="draft.bypass_out_of_scope" type="checkbox" :disabled="!settings" />
        Do not log requests outside the scope
      </label>
    </div>
    <FormField
      label="Ignore filter"
      hint="What matches is never written to the log. Decided on the request, before the response exists. A line break reads as a space."
    >
      <textarea
        v-model="draft.ignore_filter"
        class="form-textarea form-textarea--filter"
        placeholder="req.ext in (png, css, woff2) OR req.host contains telemetry"
        spellcheck="false"
        :disabled="!settings"
      ></textarea>
    </FormField>
    <FilterHelpBox subject="request" />

    <FormField
      label="Keep at most"
      hint="Entries. Past it the oldest go as new ones arrive, and saved entries are never dropped. Empty or 0 keeps everything, which is what an unbounded log did before."
    >
      <input
        v-model="draft.max_entries"
        class="form-input"
        type="number"
        min="0"
        max="10000000"
        step="1"
        placeholder="0 - keep everything"
        :disabled="!settings"
      />
    </FormField>
  </FormDialog>
</template>
