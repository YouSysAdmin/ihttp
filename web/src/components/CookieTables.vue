<script setup lang="ts">
// The cookies of one message, under its headers rather than beside
// them: a cookie IS a header, and a tab of its own would suggest it is
// something else. What it adds is the split - one row per cookie
// instead of one line of semicolons - and the attributes of a
// Set-Cookie, which is where the interesting part usually is.
import { computed } from 'vue'
import type { Header } from '../api/types'
import { cookiesOf } from '../composables/cookies'

const props = defineProps<{ headers: Header[] }>()

const cookies = computed(() => cookiesOf(props.headers))
const any = computed(() => cookies.value.sent.length > 0 || cookies.value.set.length > 0)
</script>

<template>
  <div v-if="any" class="cookie-block">
    <template v-if="cookies.sent.length">
      <h4 class="cookie-title">Cookies sent ({{ cookies.sent.length }})</h4>
      <div class="table-wrapper">
        <table class="headers-table">
          <tbody>
            <tr v-for="(c, i) in cookies.sent" :key="i">
              <td class="header-name">
                <code>{{ c.name }}</code>
              </td>
              <td class="header-value">{{ c.value }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <template v-if="cookies.set.length">
      <h4 class="cookie-title">Cookies set ({{ cookies.set.length }})</h4>
      <div class="table-wrapper">
        <table class="headers-table">
          <tbody>
            <template v-for="(c, i) in cookies.set" :key="i">
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
</template>
