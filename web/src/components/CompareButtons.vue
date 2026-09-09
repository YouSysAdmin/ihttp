<script setup lang="ts">
// Pin an exchange as A, or compare the one on show with the pin. The
// sender and the automation reader render this, the log reader has the
// same three states in its Actions menu.
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useCompareStore, type PinnedExchange } from '../stores/compare'

const props = defineProps<PinnedExchange>()

const compare = useCompareStore()
const router = useRouter()

const self = computed<PinnedExchange>(() => ({
  kind: props.kind,
  id: props.id,
  jobId: props.jobId,
  label: props.label,
}))
const isA = computed(() => compare.isPinned(self.value))
</script>

<template>
  <template v-if="!compare.pinned">
    <button type="button" class="btn btn-secondary btn-sm" @click="compare.pin(self)">
      Pin as A
    </button>
  </template>
  <template v-else-if="isA">
    <span class="badge badge-info">A</span>
    <button type="button" class="btn btn-secondary btn-sm" @click="compare.unpin()">Unpin</button>
  </template>
  <template v-else>
    <button
      type="button"
      class="btn btn-secondary btn-sm"
      @click="router.push(compare.hrefFor(self))"
    >
      Compare with A
    </button>
  </template>
</template>
