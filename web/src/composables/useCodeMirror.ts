// One CodeMirror editor, wrapped so a view can declare several without
// several copies of the same twenty lines. Owns the lifecycle: the host
// element, the view, the text, and tearing it down.
import { onBeforeUnmount, ref, shallowRef } from 'vue'
import { Compartment, EditorState } from '@codemirror/state'
import { EditorView, placeholder as showPlaceholder } from '@codemirror/view'
import { html } from '@codemirror/lang-html'
import { json } from '@codemirror/lang-json'
import { xml } from '@codemirror/lang-xml'
import { javascript } from '@codemirror/lang-javascript'
import { oneDark } from '@codemirror/theme-one-dark'
import { basicSetup } from 'codemirror'
import type { BodyLanguage } from './http'

export interface CodeMirrorOptions {
  language?: BodyLanguage
  placeholder?: string
  readOnly?: boolean

  // Called when the PERSON typed, never when the document was set.
  onEdit?: () => void
}

// Code is always on dark: syntax colours are designed against it, and
// an editor that restyles itself with the page makes one body look
// like two documents.
const surface = EditorView.theme({
  '&': { fontSize: '13px', height: '100%' },
  '.cm-scroller': {
    fontFamily: '"Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, monospace',
  },
  '.cm-content': { minHeight: '80px' },
})

function languageExtension(lang: BodyLanguage | undefined) {
  switch (lang) {
    case 'json':
      return json()
    case 'html':
      return html()
    case 'xml':
      return xml()
    case 'javascript':
      return javascript()
    default:
      return []
  }
}

// The read-only editor's extensions, for a view built outside useCodeMirror.
export function readerExtensions(lang: BodyLanguage | undefined) {
  return [
    basicSetup,
    EditorView.lineWrapping,
    oneDark,
    surface,
    languageExtension(lang),
    EditorState.readOnly.of(true),
  ]
}

export function useCodeMirror(options: CodeMirrorOptions = {}) {
  const host = ref<HTMLElement | null>(null)
  const view = shallowRef<EditorView | null>(null)
  const text = ref('')

  const language = new Compartment()
  const readOnly = new Compartment()

  let replacing = false

  function mount(initial = '') {
    if (!host.value || view.value) return

    text.value = initial
    const extensions = [
      basicSetup,
      EditorView.lineWrapping,
      EditorView.updateListener.of((update) => {
        if (!update.docChanged) return

        text.value = update.state.doc.toString()
        if (!replacing) options.onEdit?.()
      }),
      oneDark,
      surface,
      language.of(languageExtension(options.language)),
      readOnly.of(EditorState.readOnly.of(!!options.readOnly)),
    ]

    if (options.placeholder) extensions.push(showPlaceholder(options.placeholder))

    view.value = new EditorView({
      state: EditorState.create({ doc: initial, extensions }),
      parent: host.value,
    })
  }

  // Replace the whole document without it counting as an edit.
  function set(next: string) {
    const editor = view.value
    if (!editor) {
      text.value = next
      return
    }

    replacing = true
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: next } })
    replacing = false
  }

  function setLanguage(lang: BodyLanguage) {
    view.value?.dispatch({ effects: language.reconfigure(languageExtension(lang)) })
  }

  function destroy() {
    view.value?.destroy()
    view.value = null
  }

  onBeforeUnmount(destroy)

  return { host, text, mount, set, setLanguage, destroy }
}
