<script setup lang="ts">
// Two exchanges side by side. A was pinned on a reader page, B is the one
// the reader was on when Compare was clicked. Both are fetched by id, so
// the page works cold from its URL. Status line and headers in one merge
// view, the body in another, JSON pretty-printed when both sides are JSON.
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { reqlogsApi } from '../../api/reqlogs'
import { senderApi } from '../../api/sender'
import { automationApi } from '../../api/automation'
import { apiErrorMessage } from '../../api/client'
import { useProjectStore } from '../../stores/project'
import { decodeRef } from '../../stores/compare'
import {
  fromAutomation,
  fromLog,
  fromSender,
  headersDoc,
  type Exchange,
  type Half,
} from '../../composables/exchange'
import { bodyLanguage, contentType, prettyJSON } from '../../composables/http'
import { humanSize } from '../../composables/humanSize'
import { useMergeView } from '../../composables/useMergeView'
import PageHeader from '../../components/PageHeader.vue'
import ProjectGate from '../../components/ProjectGate.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import Notice from '../../components/Notice.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import MethodBadge from '../../components/MethodBadge.vue'
import StatusBadge from '../../components/StatusBadge.vue'

// A body larger than this is not diffed: the algorithm is quadratic in
// the worst case and the page would freeze.
const MAX_DIFF = 2 << 20

const route = useRoute()
const router = useRouter()
const projects = useProjectStore()

const gated = computed(() => projects.loaded && !projects.active)

const loading = ref(true)
const error = ref('')
const a = ref<Exchange | null>(null)
const b = ref<Exchange | null>(null)

const side = ref<string>('response')
const layout = ref<string>('split')
const pretty = ref(true)

const sideTabs = computed<TabItem[]>(() => [
  { id: 'request', label: 'Request' },
  { id: 'response', label: 'Response', disabled: !a.value?.response || !b.value?.response },
])
const layoutTabs: TabItem[] = [
  { id: 'split', label: 'Side by side' },
  { id: 'unified', label: 'Unified' },
]

const halves = computed<[Half, Half] | null>(() => {
  if (!a.value || !b.value) return null
  const ha = side.value === 'response' ? a.value.response : a.value.request
  const hb = side.value === 'response' ? b.value.response : b.value.request
  return ha && hb ? [ha, hb] : null
})

const bothJSON = computed(() => {
  const h = halves.value
  if (!h) return false
  return h.every((x) => !x.binary && bodyLanguage(contentType(x.headers), x.body) === 'json')
})

const anyBinary = computed(() => halves.value?.some((x) => x.binary) ?? false)
const tooLarge = computed(() => halves.value?.some((x) => x.body.length > MAX_DIFF) ?? false)

const headersView = useMergeView()
const bodyView = useMergeView()
const headersHost = headersView.host
const bodyHost = bodyView.host

// The digests of binary bodies, the only comparison bytes allow here.
const hashes = ref<[string, string] | null>(null)

async function render() {
  const h = halves.value
  hashes.value = null
  if (!h) {
    headersView.destroy()
    bodyView.destroy()
    return
  }
  await nextTick()
  const unified = layout.value === 'unified'
  headersView.mount(headersDoc(h[0]), headersDoc(h[1]), { unified })

  if (anyBinary.value) {
    bodyView.destroy()
    hashes.value = await Promise.all([digest(h[0]), digest(h[1])])
    return
  }
  if (tooLarge.value) {
    bodyView.destroy()
    return
  }
  const show = (x: Half) => (bothJSON.value && pretty.value ? prettyJSON(x.body) : x.body)
  const language = bodyLanguage(contentType(h[0].headers), h[0].body)
  bodyView.mount(show(h[0]), show(h[1]), { unified, language })
}

async function digest(h: Half): Promise<string> {
  try {
    const bytes = await (await fetch(h.rawUrl)).arrayBuffer()
    const sum = await crypto.subtle.digest('SHA-256', bytes)
    return Array.from(new Uint8Array(sum), (x) => x.toString(16).padStart(2, '0')).join('')
  } catch {
    return ''
  }
}

async function loadOne(spec: string): Promise<Exchange> {
  const r = decodeRef(spec)
  if (!r) throw new Error(`"${spec}" does not name an exchange`)
  switch (r.kind) {
    case 'log':
      return fromLog((await reqlogsApi.get(r.id)).data.entry)
    case 'sender':
      return fromSender((await senderApi.get(r.id)).data.request)
    default:
      return fromAutomation(r.jobId!, (await automationApi.result(r.jobId!, r.id)).data.result)
  }
}

async function load() {
  if (!projects.active) return
  // The pair asked for. A slower answer to an earlier pair, two quick
  // swaps say, must not replace the one the route names now.
  const qa = String(route.query.a ?? '')
  const qb = String(route.query.b ?? '')
  const stale = () => String(route.query.a ?? '') !== qa || String(route.query.b ?? '') !== qb
  loading.value = true
  error.value = ''
  try {
    const [x, y] = await Promise.all([loadOne(qa), loadOne(qb)])
    if (stale()) return
    a.value = x
    b.value = y
    if (!x.response || !y.response) side.value = 'request'
  } catch (e) {
    if (stale()) return
    a.value = null
    b.value = null
    error.value =
      e instanceof Error && !('response' in e)
        ? e.message
        : apiErrorMessage(e, 'Could not load the exchanges')
  }
  loading.value = false
  void render()
}

function swap() {
  void router.replace({ query: { a: route.query.b, b: route.query.a } })
}

watch([side, layout, pretty], () => void render())
watch(
  () => [route.query.a, route.query.b],
  () => void load(),
)
watch(
  () => projects.active?.id,
  () => void load(),
)
onMounted(load)
</script>

<template>
  <div class="reader-page">
    <PageHeader title="Compare">
      <TabStrip v-model="layout" :tabs="layoutTabs" />
      <button class="btn btn-secondary" :disabled="!a || !b" @click="swap">Swap A and B</button>
    </PageHeader>

    <ProjectGate v-if="gated" />
    <div v-else class="card compare-card">
      <LoadingBlock v-if="loading" />
      <Notice v-else-if="error" kind="danger"
        ><p>{{ error }}</p></Notice
      >
      <template v-else-if="a && b">
        <div class="compare-cols">
          <div v-for="(x, i) in [a, b]" :key="i" class="compare-col">
            <span class="badge badge-info">{{ i === 0 ? 'A' : 'B' }}</span>
            <MethodBadge v-if="x.method" :method="x.method" />
            <span class="exchange-url code-font" :title="x.url">{{ x.url || x.label }}</span>
            <StatusBadge :code="x.statusCode" :reason="x.status" />
          </div>
        </div>

        <div class="strip">
          <TabStrip v-model="side" :tabs="sideTabs" class="side-tabs" />
          <div class="strip-end">
            <button
              v-if="bothJSON"
              type="button"
              class="btn btn-secondary btn-sm"
              @click="pretty = !pretty"
            >
              {{ pretty ? 'Raw' : 'Pretty' }}
            </button>
          </div>
        </div>

        <div class="compare-body">
          <div class="compare-section">
            <div class="compare-section-title">Status and headers</div>
            <div ref="headersHost" class="merge-host"></div>
          </div>

          <div class="compare-section">
            <div class="compare-section-title">Body</div>
            <div v-if="anyBinary && halves" class="compare-binary">
              <table class="table">
                <thead>
                  <tr>
                    <th></th>
                    <th>Size</th>
                    <th>SHA-256</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(h, i) in halves" :key="i">
                    <td>{{ i === 0 ? 'A' : 'B' }}</td>
                    <td>{{ humanSize(h.size) }}</td>
                    <td class="code-font">{{ hashes?.[i] || '...' }}</td>
                  </tr>
                </tbody>
              </table>
              <p class="form-hint">
                <template v-if="hashes && hashes[0] && hashes[0] === hashes[1]"
                  >The bytes are identical.</template
                >
                <template v-else-if="hashes">The bytes differ.</template>
                <template v-else>Comparing the bytes...</template>
              </p>
            </div>
            <Notice v-else-if="tooLarge" kind="warning">
              <p>
                A body is over {{ humanSize(MAX_DIFF) }} and is not diffed. Download both instead.
              </p>
            </Notice>
            <div v-else ref="bodyHost" class="merge-host"></div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>
