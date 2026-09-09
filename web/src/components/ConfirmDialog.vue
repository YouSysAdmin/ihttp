<script setup lang="ts">
// The one dialog that asks before something happens. Mounted once, in
// App.vue, and driven by useConfirm from anywhere, which is why it reads
// the composable rather than taking props.
import { computed } from 'vue'
import { useConfirm } from '../composables/useConfirm'
import { getIcon } from '../layouts/icons'
import BaseModal from './BaseModal.vue'

const { open, shown, accept, dismiss } = useConfirm()

const variant = computed(() => shown.value?.variant ?? 'info')

// The glyph comes from the icon map, so it matches the nav's weight.
const glyph = computed(() => {
  if (variant.value === 'danger') return getIcon('x-circle')
  if (variant.value === 'warning') return getIcon('alert-triangle')

  return getIcon('info')
})

// Red only for danger. A warning is reversible and keeps the primary
// button, the dialog's text carries the difference.
const confirmClass = computed(() => (variant.value === 'danger' ? 'btn-danger' : 'btn-primary'))
</script>

<template>
  <Teleport to="body">
    <BaseModal v-if="open && shown" size="modal-w440" @close="dismiss">
      <template #header>
        <div class="ask">
          <span class="ask-mark" :class="`ask-mark-${variant}`" aria-hidden="true" v-html="glyph" />
          <h3>{{ shown.title }}</h3>
        </div>
      </template>

      <p class="ask-message">{{ shown.message }}</p>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="dismiss">
          {{ shown.cancelText }}
        </button>
        <button type="button" class="btn" :class="confirmClass" @click="accept">
          {{ shown.confirmText }}
        </button>
      </template>
    </BaseModal>
  </Teleport>
</template>

<style scoped>
.ask {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* A tinted square holding the glyph, sized to sit level with the title
   beside it rather than to be noticed on its own. */
.ask-mark {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 34px;
  height: 34px;
  border-radius: var(--radius-md);
}

/* The badge tokens, so severity means the same thing here as it does in
   a table cell or a notice on the page behind this dialog. */
.ask-mark-danger {
  background: var(--danger-50);
  color: var(--danger-fg);
}

.ask-mark-warning {
  background: var(--warning-50);
  color: var(--warning-fg);
}

.ask-mark-info {
  background: var(--info-50);
  color: var(--info-fg);
}

.ask-message {
  font-size: 14px;
  color: var(--text-secondary);
  line-height: 1.6;
}
</style>
