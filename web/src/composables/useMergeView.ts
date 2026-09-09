// Two documents side by side, or one over the other, with their
// differences marked. The sibling of useCodeMirror for a MergeView,
// which is not an EditorView and cannot be wrapped by it.
import { onBeforeUnmount, ref, shallowRef } from 'vue'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { MergeView, unifiedMergeView } from '@codemirror/merge'
import { readerExtensions } from './useCodeMirror'
import type { BodyLanguage } from './http'

export interface MergeOptions {
  language?: BodyLanguage
  // One column, B with A's differences inline, instead of two.
  unified?: boolean
}

export function useMergeView() {
  const host = ref<HTMLElement | null>(null)
  const view = shallowRef<MergeView | EditorView | null>(null)

  function mount(a: string, b: string, options: MergeOptions = {}) {
    destroy()
    if (!host.value) return

    const extensions = readerExtensions(options.language)

    if (options.unified) {
      view.value = new EditorView({
        state: EditorState.create({
          doc: b,
          extensions: [...extensions, unifiedMergeView({ original: a, mergeControls: false })],
        }),
        parent: host.value,
      })
      return
    }

    view.value = new MergeView({
      a: { doc: a, extensions },
      b: { doc: b, extensions },
      parent: host.value,
      highlightChanges: true,
      gutter: true,
      collapseUnchanged: { margin: 3, minSize: 6 },
    })
  }

  function destroy() {
    view.value?.destroy()
    view.value = null
  }

  onBeforeUnmount(destroy)

  return { host, mount, destroy }
}
