<script setup lang="ts">
// The Decoder: paste something, pick a transform, read the result. The
// result can be sent back to the input so steps chain - base64, then
// JSON pretty-print, then read. The transforms sit in one strip over
// the two panes, a group tab and that group's actions, the way a
// message pane carries its own tabs.
import { computed, ref } from 'vue'
import PageHeader from '../../components/PageHeader.vue'
import CopyButton from '../../components/CopyButton.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import * as codec from '../../composables/codec'

type Transform = (s: string) => string | Promise<string>

interface Op {
  label: string
  run: Transform
}

interface Group {
  title: string
  ops: Op[]
}

const GROUPS: Group[] = [
  {
    title: 'Base64',
    ops: [
      { label: 'Encode', run: (s) => codec.base64Encode(s) },
      { label: 'Encode URL-safe', run: (s) => codec.base64Encode(s, true) },
      { label: 'Decode', run: codec.base64Decode },
    ],
  },
  {
    title: 'URL',
    ops: [
      { label: 'Encode', run: codec.urlEncode },
      { label: 'Decode', run: codec.urlDecode },
    ],
  },
  {
    title: 'HTML',
    ops: [
      { label: 'Encode', run: codec.htmlEncode },
      { label: 'Decode', run: codec.htmlDecode },
    ],
  },
  {
    title: 'Hex',
    ops: [
      { label: 'Encode', run: codec.hexEncode },
      { label: 'Decode', run: codec.hexDecode },
    ],
  },
  {
    title: 'JSON',
    ops: [
      { label: 'Pretty', run: codec.jsonPretty },
      { label: 'Minify', run: codec.jsonMinify },
    ],
  },
  {
    title: 'JWT',
    ops: [{ label: 'Decode', run: codec.jwtDecode }],
  },
  {
    title: 'Gzip',
    ops: [
      { label: 'Compress to base64', run: codec.gzipEncode },
      { label: 'Decompress from base64', run: codec.gzipDecode },
    ],
  },
  {
    title: 'Hash',
    ops: [
      { label: 'SHA-1', run: codec.sha1 },
      { label: 'SHA-256', run: codec.sha256 },
      { label: 'SHA-512', run: codec.sha512 },
    ],
  },
  {
    title: 'Time',
    ops: [{ label: 'Unix timestamp to date', run: codec.timestampDecode }],
  },
]

const input = ref('')
const output = ref('')
const error = ref('')
const last = ref('')

// The open group. Its actions are the buttons at the end of the strip.
const groupTitle = ref(GROUPS[0]!.title)
const groupTabs = computed<TabItem[]>(() => GROUPS.map((g) => ({ id: g.title, label: g.title })))
const group = computed(() => GROUPS.find((g) => g.title === groupTitle.value) ?? GROUPS[0]!)

async function apply(op: Op) {
  const g = group.value
  error.value = ''
  try {
    output.value = await op.run(input.value)
    last.value = `${g.title} - ${op.label}`
  } catch (e) {
    output.value = ''
    error.value = `${g.title} ${op.label}: ${(e as Error).message}`
  }
}

function chain() {
  input.value = output.value
  output.value = ''
  last.value = ''
}

function clear() {
  input.value = ''
  output.value = ''
  error.value = ''
  last.value = ''
}
</script>

<template>
  <div class="reader-page">
    <PageHeader title="Decoder">
      <button class="btn btn-secondary" :disabled="!input && !output" @click="clear">Clear</button>
    </PageHeader>

    <div class="card decoder">
      <div class="strip">
        <TabStrip v-model="groupTitle" :tabs="groupTabs" />
        <div class="strip-end">
          <button
            v-for="op in group.ops"
            :key="op.label"
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="!input"
            @click="apply(op)"
          >
            {{ op.label }}
          </button>
        </div>
      </div>

      <div class="decoder-panes">
        <div class="decoder-pane">
          <div class="decoder-pane-head">
            <span>Input</span>
            <span class="text-xs text-muted">{{ input.length.toLocaleString() }} chars</span>
          </div>
          <textarea
            v-model="input"
            class="form-textarea decoder-text"
            placeholder="Paste a token, a query string, a base64 blob..."
            spellcheck="false"
          ></textarea>
        </div>

        <div class="decoder-pane">
          <div class="decoder-pane-head">
            <span>Output</span>
            <div class="flex items-center gap-2">
              <span v-if="last" class="text-xs text-muted">{{ last }}</span>
              <CopyButton v-if="output" :value="output" />
              <button class="btn btn-secondary btn-sm" :disabled="!output" @click="chain">
                Use as input
              </button>
            </div>
          </div>
          <p v-if="error" class="text-danger text-sm mb-2">{{ error }}</p>
          <textarea
            :value="output"
            class="form-textarea decoder-text"
            readonly
            placeholder="The result appears here."
            spellcheck="false"
          ></textarea>
        </div>
      </div>
    </div>
  </div>
</template>
