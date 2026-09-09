<script lang="ts">
// ESCAPE BELONGS TO THE TOP DIALOG, and only to it.
//
// Every open modal listens on `document`, so without a rule one press
// would close a dialog and the one under it. A stack in mount order
// makes the newest win. It cannot be a z-index or a DOM query: dialogs
// from different components sit in different places in the tree and
// share one overlay class.
//
// It has to be this block: a `const` in <script setup> is compiled into
// setup(), so every dialog would get a stack holding only itself.
const openModals: symbol[] = []
</script>

<script setup lang="ts">
// Every dialog in the console is this component, so closing means one
// thing: Escape, the footer, or a click that starts and ends on the
// overlay. Mount it behind v-if, so the keydown listener lives exactly
// as long as the open dialog.
import { onMounted, onUnmounted, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    // Heading text. Use the header slot instead when it is not a plain
    // string.
    title?: string
    // Extra class on the box, from the width scale in the stylesheet:
    // modal-w440 through modal-w900.
    size?: string
    // Wrap body and footer in a form, so a footer submit button and the
    // Enter key both reach @submit.
    form?: boolean
    // Neither the overlay nor Escape closes it, only what the footer
    // offers. For a dialog that shows a value exactly once.
    persistent?: boolean
  }>(),
  { title: '', size: '', form: false, persistent: false },
)

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit'): void
}>()

// A click that STARTS on the overlay and ENDS on the overlay is a click
// outside the box. Anything else is a drag, and @click.self cannot tell
// the two apart because it only ever sees the release.
const startedOnOverlay = ref(false)

function watchClickStart(event: MouseEvent) {
  startedOnOverlay.value = event.target === event.currentTarget
}

function confirmClickEnd(event: MouseEvent) {
  if (props.persistent) return

  if (startedOnOverlay.value && event.target === event.currentTarget) {
    emit('close')
  }
  startedOnOverlay.value = false
}

const id = Symbol('modal')

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || props.persistent) return
  // Not mine to answer. A persistent dialog on top absorbs the press
  // rather than passing it down, which is the point of it being on top.
  if (openModals[openModals.length - 1] !== id) return

  emit('close')
}

onMounted(() => {
  openModals.push(id)
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  const at = openModals.indexOf(id)
  if (at !== -1) openModals.splice(at, 1)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="modal-overlay" @mousedown="watchClickStart" @mouseup="confirmClickEnd">
    <div class="modal" :class="size" role="dialog" aria-modal="true" @mousedown.stop @mouseup.stop>
      <div class="modal-header">
        <slot name="header"
          ><h3>{{ title }}</h3></slot
        >
      </div>

      <!-- The two branches carry the same two rows on purpose. Wrapping
           them in one element that is a form or a div would put an extra
           div around the body of every non-form dialog. -->
      <form v-if="form" @submit.prevent="emit('submit')">
        <div class="modal-body"><slot /></div>
        <div v-if="$slots.footer" class="modal-footer"><slot name="footer" /></div>
      </form>
      <template v-else>
        <div class="modal-body"><slot /></div>
        <div v-if="$slots.footer" class="modal-footer"><slot name="footer" /></div>
      </template>
    </div>
  </div>
</template>
