<script setup lang="ts">
// The segmented tab control. The model is the id of the active tab.
export interface TabItem {
  id: string
  label: string
  disabled?: boolean
  // A small count beside the label.
  count?: number
}

defineProps<{ tabs: TabItem[] }>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <div class="tabs">
    <button
      v-for="t in tabs"
      :key="t.id"
      type="button"
      :class="['tab', { active: model === t.id }]"
      :disabled="t.disabled"
      @click="model = t.id"
    >
      <slot name="label" :tab="t">
        {{ t.label }}
        <span v-if="t.count !== undefined" class="tab-count">{{ t.count }}</span>
      </slot>
    </button>
  </div>
</template>
