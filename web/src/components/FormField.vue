<script setup lang="ts">
// A labelled field: label, the control, and whatever guidance goes under
// it. Errors come from two places: `error` is the server's message, and
// the browser's own constraint check (`type=number`, `min`, `required`)
// is read here, because a number input with letters in it hands the
// model '' and nothing on screen would say so. The native check reports
// on blur, never before the field has been touched, and clears as soon
// as the value is valid. The control stays in the slot rather than
// becoming props.
import { computed, onBeforeUnmount, onMounted, ref, useId } from 'vue'

const props = withDefaults(
  defineProps<{
    // Plain text. Use the label slot when the heading carries markup.
    label?: string
    // Ties the label to the control. Pass the same value as the
    // control's id.
    for?: string
    // Guidance under the control. Plain text, or the hint slot for markup.
    hint?: string
    // The server's message for this field.
    error?: string
    // Marks the plain-text label. A label given through the slot writes
    // its own, since it is markup by then.
    required?: boolean
    // Turn the browser's own check off, for a control whose constraints
    // are not what the reader is being asked about.
    native?: boolean
  }>(),
  { label: '', for: '', hint: '', error: '', required: false, native: true },
)

const root = ref<HTMLElement | null>(null)
const nativeError = ref('')

type Control = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement

function controls(): Control[] {
  if (!root.value) return []

  return Array.from(root.value.querySelectorAll<Control>('input, select, textarea'))
}

// The reasons a person can act on are worded here, the browser's own
// localized `validationMessage` covers the rest.
function messageFor(el: Control): string {
  const v = el.validity
  if (v.valid) return ''

  if (v.badInput) {
    return el.type === 'number' ? 'Enter a number' : 'This value is not valid'
  }

  if (v.rangeUnderflow) return `Must be ${(el as HTMLInputElement).min} or more`
  if (v.rangeOverflow) return `Must be ${(el as HTMLInputElement).max} or less`
  if (v.valueMissing) return 'Required'
  if (v.typeMismatch && el.type === 'email') return 'Enter an email address'
  if (v.typeMismatch && el.type === 'url') return 'Enter a URL'

  return el.validationMessage
}

function check() {
  if (!props.native) return
  for (const el of controls()) {
    const message = messageFor(el)
    if (message) {
      nativeError.value = message

      return
    }
  }
  nativeError.value = ''
}

// Blur reports, typing only ever clears. The second half is what keeps a
// corrected value from staying red until the field is left again.
function onBlur() {
  check()
}

function onInput() {
  if (nativeError.value) check()
}

// The label is tied to its control here, since this component owns
// both and the id exists only to join them. It never overrides: a `for`
// prop, a `for` on the label, or an id on the control all win.
const autoID = useId()

function tieLabelToControl() {
  if (props.for) return

  const label = root.value?.querySelector<HTMLLabelElement>('label.form-label')
  if (!label || label.htmlFor) return

  // A checkbox row writes its own <label>, so this does not run there.
  // With several controls the heading names the group and the first
  // one takes the focus.
  const el = controls()[0]
  if (!el) return

  if (!el.id) el.id = autoID
  label.htmlFor = el.id
}

onMounted(() => {
  root.value?.addEventListener('focusout', onBlur)
  root.value?.addEventListener('input', onInput)
  tieLabelToControl()
})

onBeforeUnmount(() => {
  root.value?.removeEventListener('focusout', onBlur)
  root.value?.removeEventListener('input', onInput)
})

// The server's answer wins: it saw the whole request.
const shown = computed(() => props.error || nativeError.value)
</script>

<template>
  <div ref="root" class="form-group">
    <!-- A trailing asterisk, because that is how the forms that mark a
         required field already do it. No new class for it: an unstyled
         one is a build failure here, and inventing a colour would make
         the same mark look like two different things across pages. -->
    <label v-if="label || $slots.label" class="form-label" :for="$props.for">
      <slot name="label">{{ label }}{{ required ? ' *' : '' }}</slot>
    </label>
    <slot />
    <!-- The error replaces the hint rather than stacking under it: two
         lines of guidance where one of them says the value is wrong
         reads as if both are still true.

         Which is why a hint belongs HERE and not in the slot above. The
         prop was written for this and had no callers at all: all 73 of
         them wrote their own <p class="form-hint"> into the default
         slot, where this component cannot reach it, so every one went on
         sitting under the error it was meant to be replaced by. The slot is for the three that carry markup. -->
    <p v-if="shown" class="form-error">{{ shown }}</p>
    <p v-else-if="hint || $slots.hint" class="form-hint">
      <slot name="hint">{{ hint }}</slot>
    </p>
  </div>
</template>
