<script setup lang="ts">
// Settings: what this instance is - the proxy address, the certificate.
// Project settings live where they act: intercept filters on the
// Intercept page, scope rules on the Scope page.
import { onMounted, ref } from 'vue'
import { infoApi } from '../../api/info'
import { apiErrorMessage } from '../../api/client'
import type { Info } from '../../api/types'
import { useNotificationStore } from '../../stores/notification'
import { formatDate } from '../../composables/formatDate'
import PageHeader from '../../components/PageHeader.vue'
import CopyButton from '../../components/CopyButton.vue'
import Notice from '../../components/Notice.vue'

const notify = useNotificationStore()

const info = ref<Info | null>(null)

onMounted(async () => {
  try {
    info.value = (await infoApi.get()).data
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Failed to load instance info'))
  }
})
</script>

<template>
  <div>
    <PageHeader title="Settings" />

    <div class="card">
      <div class="card-header">
        <h2>This instance</h2>
      </div>
      <div class="card-body">
        <dl v-if="info" class="facts">
          <dt>Proxy</dt>
          <dd class="flex items-center gap-2">
            <code>{{ info.proxy_url }}</code>
            <CopyButton :value="info.proxy_url" label="Copy" />
          </dd>
          <dt>Console</dt>
          <dd>
            <code>{{ info.console_url }}</code>
          </dd>
          <dt>Data directory</dt>
          <dd>
            <code>{{ info.data_dir }}</code>
          </dd>
          <dt>Version</dt>
          <dd>{{ info.version }}</dd>
        </dl>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h2>CA certificate</h2>
      </div>
      <div class="card-body">
        <Notice kind="info" class="mb-4">
          <p>
            HTTPS sites load without warnings once the certificate is trusted. Download it and
            install it in the browser or system trust store, or run
            <code>ihttp cert install</code> in a terminal.
          </p>
        </Notice>
        <div class="flex gap-2 flex-wrap">
          <a class="btn btn-primary" :href="infoApi.caURL" download="ihttp-ca.pem"
            >Download ca.pem</a
          >
          <a
            v-if="info"
            class="btn btn-secondary"
            :href="info.proxy_url"
            target="_blank"
            rel="noopener"
          >
            Open the proxy landing page
          </a>
        </div>
        <p class="form-hint mt-3">
          A browser configured to use the proxy can also visit <code>http://ihttp.proxy/</code> to
          fetch it.
        </p>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h2>Client certificates</h2>
        <span class="header-sub m-0"
          >What ihttp presents to an upstream that asks the client for a certificate.</span
        >
      </div>
      <div class="card-body">
        <Notice kind="info" class="mb-4">
          <p>
            Configured with
            <code>--client-cert '&lt;host&gt;=&lt;cert file&gt;[,&lt;key file&gt;]'</code>,
            repeatable, and read-only here. The files stay on disk and are read at start: ihttp is
            not a key store. A host no pattern names is offered nothing, which is how it should be -
            a certificate is only presented where it belongs.
          </p>
        </Notice>
        <div v-if="info?.client_certs?.length" class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>Hosts</th>
                <th>Subject</th>
                <th>Valid until</th>
                <th>File</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in info.client_certs" :key="c.host + c.path">
                <td>
                  <code>{{ c.host }}</code>
                </td>
                <td>{{ c.subject }}</td>
                <td :class="{ 'text-danger': c.expired }">
                  {{ formatDate(c.not_after) }}<template v-if="c.expired"> - expired</template>
                </td>
                <td>
                  <code>{{ c.path }}</code>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="text-sm text-muted">
          None configured, so no client certificate is ever presented. An upstream that requires one
          refuses the handshake, which reaches the client as a 502.
        </p>
      </div>
    </div>

    <div class="card">
      <div class="card-header">
        <h2>Browsers</h2>
      </div>
      <div class="card-body">
        <p class="text-sm text-muted mb-3">
          A throwaway profile pointed at the proxy, with the browser's own background traffic
          switched off so the log holds what you did.
        </p>
        <!-- The content of a pre is what is between the tags, indentation
             and all, so it starts hard against the tag. -->
        <pre class="code-block">
ihttp browser chrome
ihttp browser firefox</pre>
      </div>
    </div>
  </div>
</template>
