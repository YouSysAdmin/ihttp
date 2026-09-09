<script setup lang="ts">
// The instance's ways out: the named proxies ihttp itself goes through,
// and the default it falls back to.
//
// A page rather than a card on Settings: it is a table with four
// actions per row, and Settings had grown to four cards. What a project
// does with this list is chosen on the Projects page, because a
// project's own value never goes on an instance page.
import { onMounted, ref } from 'vue'
import { infoApi } from '../../api/info'
import { upstreamsApi } from '../../api/upstreams'
import { apiErrorMessage } from '../../api/client'
import type { Info } from '../../api/types'
import PageHeader from '../../components/PageHeader.vue'
import Notice from '../../components/Notice.vue'
import UpstreamList from '../../components/UpstreamList.vue'

const info = ref<Info | null>(null)

onMounted(async () => {
  try {
    info.value = (await infoApi.get()).data
  } catch {
    // The page is still useful without it: only the default row needs it.
    info.value = null
  }
})

// The answer stays on the page: a measurement to read, not an event to
// acknowledge.
const testing = ref(false)
const result = ref<{ kind: 'success' | 'danger' | 'info'; text: string } | null>(null)

async function testDefault() {
  testing.value = true
  result.value = null

  try {
    const { result: r, error } = (await upstreamsApi.test()).data

    if (error) {
      result.value = {
        kind: 'danger',
        text: `The default could not reach ${r.target} after ${r.took_ms} ms: ${error}`,
      }
    } else if (r.direct) {
      result.value = {
        kind: 'info',
        text: `${r.target} is on the default's no-proxy list, so this proved nothing about the proxy. It answered ${r.status} in ${r.took_ms} ms.`,
      }
    } else {
      result.value = {
        kind: 'success',
        text: `The default reached ${r.target} in ${r.took_ms} ms, which answered ${r.status}.`,
      }
    }
  } catch (e) {
    result.value = { kind: 'danger', text: apiErrorMessage(e, 'The test failed') }
  } finally {
    testing.value = false
  }
}
</script>

<template>
  <div>
    <PageHeader title="Proxies" />

    <Notice kind="info" class="mb-4">
      <p>
        The proxies ihttp itself goes out through, for a lab or a network where nothing reaches the
        internet directly. A project picks one on the Projects page, so switching project switches
        the way out without a restart. The same way out is used by the proxy, the Sender and the
        Automation, so a replay leaves the machine the way the original did.
      </p>
      <p class="mt-2">
        Names are what every list and picker shows, and a host here never carries a credential. A
        stored password is only ever visible in that proxy's own editor.
      </p>
    </Notice>

    <div class="card">
      <div class="card-header">
        <h2>Default</h2>
      </div>
      <div class="card-body">
        <dl v-if="info?.upstream_proxy" class="facts">
          <dt>Proxy</dt>
          <dd class="upstream-fact">
            <code>{{ info.upstream_proxy }}</code>
            <span class="text-muted">from --upstream-proxy</span>
            <button class="btn btn-secondary btn-sm" :disabled="testing" @click="testDefault">
              {{ testing ? 'Testing...' : 'Test' }}
            </button>
          </dd>
          <template v-if="info.upstream_bypass?.length">
            <dt>No proxy</dt>
            <dd>
              <code>{{ info.upstream_bypass.join(', ') }}</code>
              <span class="text-muted">, and localhost always</span>
            </dd>
          </template>
        </dl>
        <p v-else class="text-sm text-muted">
          None. A project that picks nothing goes out directly, or through the
          <code>HTTP_PROXY</code> environment if it is set. Start ihttp with
          <code>--upstream-proxy</code> to give it one.
        </p>

        <Notice v-if="result" :kind="result.kind" class="mt-3">
          <p>{{ result.text }}</p>
        </Notice>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h2>Named proxies</h2>
      </div>
      <div class="card-body">
        <UpstreamList />
      </div>
    </div>
  </div>
</template>
