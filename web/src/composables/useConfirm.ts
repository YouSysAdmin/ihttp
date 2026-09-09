// The console's one confirmation prompt. ConfirmDialog is mounted once
// near the root and every caller drives it through this module's state,
// so the state lives at module level rather than per caller.
import { computed, shallowRef } from 'vue'

// How loud the prompt is, which decides its icon and button color.
export type ConfirmVariant = 'danger' | 'warning' | 'info'

// What a caller asks. Only the question itself is required.
export interface ConfirmRequest {
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  variant?: ConfirmVariant
}

// An open prompt: what to show, and how to answer it. One object, so
// "the dialog is open" and "somebody awaits an answer" cannot disagree.
interface OpenPrompt {
  shown: Required<ConfirmRequest>
  answer: (confirmed: boolean) => void
}

const prompt = shallowRef<OpenPrompt | null>(null)

const fallback = {
  confirmText: 'Confirm',
  cancelText: 'Cancel',
  variant: 'info' as ConfirmVariant,
}

// Views destructure `{ confirm }` and await it. ConfirmDialog takes
// `open`, `shown`, `accept` and `dismiss`.
export function useConfirm() {
  // Ask, and resolve true only when confirmed. Dismissing resolves
  // false rather than rejecting: refusing is an ordinary answer.
  function confirm(request: ConfirmRequest): Promise<boolean> {
    // A second prompt while one is open answers the first as declined,
    // so a stray double click cannot leave an await hanging.
    prompt.value?.answer(false)

    return new Promise<boolean>((resolve) => {
      prompt.value = {
        shown: { ...fallback, ...request },
        answer: (confirmed) => {
          prompt.value = null
          resolve(confirmed)
        },
      }
    })
  }

  return {
    open: computed(() => prompt.value !== null),
    shown: computed(() => prompt.value?.shown ?? null),
    accept: () => prompt.value?.answer(true),
    dismiss: () => prompt.value?.answer(false),
    confirm,
  }
}
