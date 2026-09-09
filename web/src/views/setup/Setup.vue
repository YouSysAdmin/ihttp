<script setup lang="ts">
// How to get a given client through the proxy, with this instance's real
// address and real certificate path filled in - and a way to check it
// rather than assume it.
//
// Two problems per target, kept apart because they fail differently:
// routing decides whether the request reaches ihttp at all, and trust
// decides whether the client accepts the certificate. An empty log is
// the first. A TLS error is the second.
import { computed, onMounted, ref, watch } from 'vue'
import { infoApi } from '../../api/info'
import { reqlogsApi } from '../../api/reqlogs'
import { apiErrorMessage } from '../../api/client'
import type { Info, LogSummary } from '../../api/types'
import { useProjectStore } from '../../stores/project'
import { useLiveEvents } from '../../composables/useLiveEvents'
import PageHeader from '../../components/PageHeader.vue'
import TabStrip, { type TabItem } from '../../components/TabStrip.vue'
import Notice from '../../components/Notice.vue'
import CopyButton from '../../components/CopyButton.vue'
import LoadingBlock from '../../components/LoadingBlock.vue'
import { TARGETS, type Facts } from './targets'

const projects = useProjectStore()

const info = ref<Info | null>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    info.value = (await infoApi.get()).data
  } catch (e) {
    error.value = apiErrorMessage(e, 'Could not read this instance')
  } finally {
    loading.value = false
  }
})

const error = ref('')

const facts = computed<Facts | null>(() => {
  const i = info.value
  if (!i) return null

  return {
    proxyURL: i.proxy_url,
    consoleAddr: i.console_url.replace(/^https?:\/\//, ''),
    caPath: i.ca_path,
    probeURL: i.probe_url,
  }
})

const tabs: TabItem[] = TARGETS.map((t) => ({ id: t.id, label: t.name }))
const which = ref(TARGETS[0]!.id)
const target = computed(() => TARGETS.find((t) => t.id === which.value)!)

const routing = computed(() => (facts.value ? target.value.routing(facts.value) : []))
const trust = computed(() =>
  facts.value && target.value.trust ? target.value.trust(facts.value) : '',
)

// The probe. A token in the path, a URL the proxy answers itself, and
// the log watched for it - so what is proved is the whole path the
// person cares about: their client reached the proxy and the exchange
// turned up in the log.
//
// A new token per attempt, so a command run twice cannot pass on the
// first run's evidence.
const token = ref(newToken())
const probeState = ref<'idle' | 'waiting' | 'seen'>('idle')

function newToken() {
  return Math.random().toString(36).slice(2, 10)
}

const probeURL = computed(() => `${facts.value?.probeURL ?? ''}/v/${token.value}`)
const probeSnippet = computed(() =>
  facts.value ? target.value.probe(facts.value, probeURL.value) : null,
)

function watchFor() {
  token.value = newToken()
  probeState.value = 'waiting'
}

function stopWatching() {
  probeState.value = 'idle'
}

// The log's own event stream, so nothing is polled. A probe that arrives
// while we are not watching is ignored rather than counted.
useLiveEvents({
  'reqlog.request': (s: LogSummary) => {
    if (probeState.value === 'waiting' && s.url.includes(token.value)) probeState.value = 'seen'
  },
})

const gated = computed(() => projects.loaded && !projects.active)

// A watch with no project open can never finish, since nothing is being
// logged. Stopped rather than left spinning next to the notice that
// explains why.
watch(gated, (closed) => {
  if (closed && probeState.value === 'waiting') probeState.value = 'idle'
})
</script>

<template>
  <div>
    <PageHeader title="Setup" />

    <Notice v-if="error" kind="danger" class="mb-4">
      <p>{{ error }}</p>
    </Notice>

    <LoadingBlock v-if="loading" />

    <template v-else-if="facts">
      <Notice kind="info" class="mb-4">
        <p>
          Getting a client through the proxy is two separate problems, and they fail differently.
          <strong>Routing</strong> decides whether the request reaches ihttp at all - if it does
          not, the log stays empty. <strong>Trust</strong> decides whether the client accepts the
          certificate ihttp mints for the host - if it does not, you get a TLS error. Every snippet
          below is one or the other, filled in with this instance's own address and certificate.
        </p>
      </Notice>

      <div class="card mb-4">
        <div class="card-body">
          <dl class="facts">
            <dt>Proxy</dt>
            <dd class="flex items-center gap-2">
              <code>{{ facts.proxyURL }}</code>
              <CopyButton :value="facts.proxyURL" label="Copy" />
            </dd>
            <dt>CA certificate</dt>
            <dd class="flex items-center gap-2">
              <code>{{ facts.caPath }}</code>
              <CopyButton :value="facts.caPath" label="Copy" />
            </dd>
          </dl>
        </div>
      </div>

      <TabStrip v-model="which" :tabs="tabs" class="mb-4" />

      <div class="card mb-4">
        <div class="card-header">
          <h2>{{ target.name }}</h2>
        </div>
        <div class="card-body">
          <p class="text-sm text-muted mb-4">{{ target.summary }}</p>

          <h3 class="setup-step">Routing</h3>
          <div v-for="s in routing" :key="s.label" class="setup-snippet">
            <div class="setup-snippet-head">
              <span class="text-sm text-muted">{{ s.label }}</span>
              <CopyButton :value="s.code" label="Copy" />
            </div>
            <pre class="code-font">{{ s.code }}</pre>
          </div>

          <template v-if="trust">
            <h3 class="setup-step">Trust</h3>
            <div class="setup-snippet">
              <div class="setup-snippet-head">
                <span class="text-sm text-muted">For HTTPS</span>
                <CopyButton :value="trust" label="Copy" />
              </div>
              <pre class="code-font">{{ trust }}</pre>
            </div>
          </template>
          <template v-else>
            <h3 class="setup-step">Trust</h3>
            <p class="text-sm text-muted">
              Nothing beyond <code>ihttp cert install</code>, which puts the certificate in this
              machine's system store.
            </p>
          </template>

          <Notice v-if="target.caveat" kind="warning" class="mt-4">
            <p>{{ target.caveat }}</p>
          </Notice>
        </div>
      </div>

      <div class="card">
        <div class="card-header">
          <h2>Check it</h2>
        </div>
        <div class="card-body">
          <p class="text-sm text-muted mb-3">
            Fetch a URL the proxy answers itself and watch for it in the log.
            <code>{{ facts.probeURL }}</code> does not resolve anywhere, so this needs no network
            and reaches no real server.
          </p>

          <Notice v-if="gated" kind="warning" class="mb-3">
            <p>
              No project is open, so nothing is being logged and this cannot report anything. Open
              one on the Projects page first.
            </p>
          </Notice>

          <div v-if="probeSnippet" class="setup-snippet">
            <div class="setup-snippet-head">
              <span class="text-sm text-muted">{{ probeSnippet.label }}</span>
              <CopyButton :value="probeSnippet.code" label="Copy" />
            </div>
            <pre class="code-font">{{ probeSnippet.code }}</pre>
          </div>

          <div class="setup-probe-bar">
            <button
              v-if="probeState !== 'waiting'"
              class="btn btn-primary btn-sm"
              :disabled="gated"
              @click="watchFor"
            >
              {{ probeState === 'seen' ? 'Check again' : 'Start watching' }}
            </button>
            <template v-else>
              <button class="btn btn-secondary btn-sm" @click="stopWatching">Stop</button>
              <span class="text-sm text-muted">
                Watching the log for
                <code>{{ token }}</code>
                - run the command above.
              </span>
            </template>
          </div>

          <Notice v-if="probeState === 'seen'" kind="success" class="mt-3">
            <p>The probe arrived. That client reaches the proxy and the exchange is in the log.</p>
            <p class="mt-2 text-sm">
              What this does <strong>not</strong> prove: which process sent it, that the same client
              will manage HTTPS to a real host, or that your upstream is reachable. It proves the
              routing, which is the half that is usually wrong.
            </p>
          </Notice>
        </div>
      </div>
    </template>
  </div>
</template>
