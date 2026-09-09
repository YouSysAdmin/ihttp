<script setup lang="ts">
// Copy a value to the clipboard and say so in the button's own label,
// because the button is the thing that was clicked.
import { onUnmounted, ref } from 'vue'
import { useNotificationStore } from '../stores/notification'

const props = withDefaults(
  defineProps<{
    value: string
    label?: string
    copiedLabel?: string
    // Any of the stylesheet's button classes.
    variant?: string
    // Greyed out while the value is not there yet.
    disabled?: boolean
  }>(),
  {
    label: 'Copy',
    copiedLabel: 'Copied',
    variant: 'btn btn-secondary btn-sm',
    disabled: false,
  },
)

const notify = useNotificationStore()

const copied = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
  } catch {
    // A refused clipboard says so: the next move is to copy by hand.
    notify.error('Could not copy - select the value and copy it by hand')

    return
  }
  copied.value = true
  clearTimeout(timer)
  timer = setTimeout(() => (copied.value = false), 2000)
}

// The timer may outlive the button.
onUnmounted(() => clearTimeout(timer))
</script>

<template>
  <button type="button" :class="variant" :disabled="disabled" @click="copy">
    {{ copied ? copiedLabel : label }}
  </button>
</template>
