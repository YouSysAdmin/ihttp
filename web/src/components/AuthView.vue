<script setup lang="ts">
// The credentials of one message on their own tab: the auth headers
// first, then the cookies, one row each. It is a reading of the headers
// and nothing more - every line here is also under Headers, where a
// long Cookie is one unreadable line and a token buried in a vendor
// header is one line among thirty.
import { computed, onMounted } from 'vue'
import type { Header } from '../api/types'
import { credentialsOf } from '../composables/auth'
import { useSettingsStore } from '../stores/settings'

const props = defineProps<{ headers: Header[] }>()

// The instance's own header names, on top of the built-in list. Loaded
// once per tab and shared, so a page of messages asks the server once.
const settings = useSettingsStore()

onMounted(() => {
  settings.ensure()
})

const creds = computed(() => credentialsOf(props.headers, settings.authHeaders))
</script>

<template>
  <div v-if="creds.count" class="auth-block">
    <template v-if="creds.headers.length">
      <h4 class="auth-title">Auth headers ({{ creds.headers.length }})</h4>
      <div class="table-wrapper">
        <table class="headers-table">
          <tbody>
            <tr v-for="(h, i) in creds.headers" :key="i">
              <td class="header-name">
                <code>{{ h.name }}</code>
              </td>
              <td class="header-value">
                <span v-if="h.scheme" class="auth-scheme">{{ h.scheme }}</span
                >{{ h.credential }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <template v-if="creds.sent.length">
      <h4 class="auth-title">Cookies sent ({{ creds.sent.length }})</h4>
      <div class="table-wrapper">
        <table class="headers-table">
          <tbody>
            <tr v-for="(c, i) in creds.sent" :key="i">
              <td class="header-name">
                <code>{{ c.name }}</code>
              </td>
              <td class="header-value">{{ c.value }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <template v-if="creds.set.length">
      <h4 class="auth-title">Cookies set ({{ creds.set.length }})</h4>
      <div class="table-wrapper">
        <table class="headers-table">
          <tbody>
            <template v-for="(c, i) in creds.set" :key="i">
              <tr>
                <td class="header-name">
                  <code>{{ c.name }}</code>
                </td>
                <td class="header-value">{{ c.value }}</td>
              </tr>
              <tr v-if="c.attributes.length">
                <td></td>
                <td class="cookie-attrs">
                  <span v-for="(a, j) in c.attributes" :key="j" class="cookie-attr">
                    {{ a.name }}<template v-if="a.value">: {{ a.value }}</template>
                  </span>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </template>
  </div>
  <p v-else class="viewer-muted">No cookies or auth headers.</p>
</template>
