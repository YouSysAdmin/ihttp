<script setup lang="ts">
// A dialog that is a form: a title, an error the server sent back, the
// fields, and Cancel beside Save.
import BaseModal from './BaseModal.vue'
import Notice from './Notice.vue'

withDefaults(
  defineProps<{
    title: string
    // From the width scale in the stylesheet, modal-w440 to modal-w900.
    size?: string
    // The server's refusal, shown above the fields.
    error?: string
    // Disables the submit button while a request is in flight.
    saving?: boolean
    submitLabel?: string
    cancelLabel?: string
  }>(),
  { size: 'modal-w560', error: '', saving: false, submitLabel: 'Save', cancelLabel: 'Cancel' },
)

const emit = defineEmits<{ (e: 'close'): void; (e: 'submit'): void }>()
</script>

<template>
  <BaseModal :title="title" form :size="size" @close="emit('close')" @submit="emit('submit')">
    <Notice v-if="error" kind="danger" class="mb-3">
      <p>{{ error }}</p>
    </Notice>
    <slot />
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="emit('close')">
        {{ cancelLabel }}
      </button>
      <button type="submit" class="btn btn-primary" :disabled="saving">{{ submitLabel }}</button>
    </template>
  </BaseModal>
</template>
