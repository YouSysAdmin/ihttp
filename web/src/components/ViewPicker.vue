<script setup lang="ts">
// The saved ways of looking at the log, as a dropdown beside the filter
// box. All traffic is the empty query and is always first, so there is
// always a way back to the unfiltered list.
//
// A view is project state, not browser state: it survives a restart and
// travels with a settings-only export. Editing happens through the
// dialog the parent owns, and this component only picks and reports.
import { computed } from 'vue'
import type { View } from '../api/types'
import DropdownMenu from './DropdownMenu.vue'

const props = defineProps<{
  views: View[]
  // The id of the view whose filter the page currently shows, or '' for
  // All traffic and for a filter typed by hand.
  activeId: string
}>()

const emit = defineEmits<{
  (e: 'pick', view: View | null): void
  (e: 'manage'): void
  (e: 'save'): void
}>()

const active = computed(() => props.views.find((v) => v.id === props.activeId) ?? null)
const label = computed(() => active.value?.name ?? 'All traffic')
</script>

<template>
  <DropdownMenu variant="btn btn-secondary" :label="label" align="left">
    <button
      type="button"
      class="menu-item"
      role="menuitemradio"
      :aria-checked="!activeId"
      @click="emit('pick', null)"
    >
      All traffic
    </button>

    <template v-if="views.length">
      <hr class="menu-sep" />
      <button
        v-for="v in views"
        :key="v.id"
        type="button"
        class="menu-item"
        role="menuitemradio"
        :aria-checked="v.id === activeId"
        :title="v.query || 'no filter'"
        @click="emit('pick', v)"
      >
        {{ v.name }}
        <span v-if="v.saved" class="menu-hint">saved</span>
        <span v-else-if="v.only_in_scope" class="menu-hint">in scope</span>
      </button>
    </template>

    <hr class="menu-sep" />
    <button type="button" class="menu-item" @click="emit('save')">Save this filter as...</button>
    <button type="button" class="menu-item" :disabled="!views.length" @click="emit('manage')">
      Manage views
    </button>
  </DropdownMenu>
</template>
