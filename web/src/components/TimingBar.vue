<script setup lang="ts">
// Where one exchange spent its time, as a proportional bar.
//
// The honesty this component owes the reader:
//   - no measurement at all is said, not drawn as an empty bar,
//   - a reused connection did not do DNS, connect or TLS, so those are
//     left out entirely rather than shown as three instant segments,
//   - a streamed body was never read, so receive is unknown, not zero.
import { computed } from 'vue'
import type { Timings } from '../api/types'

const props = withDefaults(
  defineProps<{
    timings?: Timings | null
    // The whole exchange as the log measured it. The phases are measured
    // around the upstream round trip and can add up to slightly less.
    durationMs?: number
    // A streamed body is never read, so its receive phase is not zero,
    // it is unknown.
    streamed?: boolean
  }>(),
  { timings: null, durationMs: 0, streamed: false },
)

interface Phase {
  id: string
  label: string
  ms: number
}

const phases = computed<Phase[]>(() => {
  const t = props.timings
  if (!t) return []

  const all: Phase[] = [
    { id: 'blocked', label: 'Blocked', ms: t.blocked_ms },
    { id: 'dns', label: 'DNS', ms: t.dns_ms },
    { id: 'connect', label: 'Connect', ms: t.connect_ms },
    { id: 'tls', label: 'TLS', ms: t.tls_ms },
    { id: 'send', label: 'Send', ms: t.send_ms },
    { id: 'wait', label: 'Wait', ms: t.wait_ms },
  ]

  if (!props.streamed) all.push({ id: 'receive', label: 'Receive', ms: t.receive_ms })

  // A phase either did not happen or was too fast to put a number on -
  // "Blocked 0.00 ms" is noise either way. Filtered on what would
  // actually be printed, so the bar and the legend agree.
  return all.filter((p) => Number(round(p.ms)) > 0)
})

const total = computed(() => phases.value.reduce((sum, p) => sum + p.ms, 0))

const ttfb = computed(() =>
  phases.value.filter((p) => p.id !== 'receive').reduce((sum, p) => sum + p.ms, 0),
)

function round(ms: number) {
  if (ms >= 100) return String(Math.round(ms))
  if (ms >= 10) return ms.toFixed(1)

  return ms.toFixed(2)
}
</script>

<template>
  <div v-if="!timings" class="timing-none text-sm text-muted">
    Not measured. A response a rule answered locally never reaches the network, and entries logged
    by an older build carry no breakdown.
  </div>

  <div v-else class="timing">
    <div class="timing-bar" role="img" :aria-label="`Time to first byte ${round(ttfb)} ms`">
      <span
        v-for="p in phases"
        :key="p.id"
        class="timing-seg"
        :class="`timing-${p.id}`"
        :style="{ flexGrow: p.ms }"
        :title="`${p.label}: ${round(p.ms)} ms`"
      ></span>
    </div>

    <dl class="timing-legend">
      <div v-for="p in phases" :key="p.id" class="timing-item">
        <dt>
          <span class="timing-dot" :class="`timing-${p.id}`" aria-hidden="true"></span>
          {{ p.label }}
        </dt>
        <dd class="code-font">{{ round(p.ms) }} ms</dd>
      </div>
    </dl>

    <p class="timing-facts text-sm text-muted">
      <span class="code-font">TTFB {{ round(ttfb) }} ms</span>
      <span v-if="durationMs">total {{ durationMs }} ms</span>
      <span v-if="timings.reused" title="DNS, connect and TLS did not run for this request">
        reused connection
      </span>
      <span v-if="streamed" title="A streamed body is relayed uncaptured, so it is never read here">
        body streamed, receive not measured
      </span>
      <span v-if="timings.server_addr" class="code-font">{{ timings.server_addr }}</span>
      <span v-if="timings.tls_version" class="code-font">{{ timings.tls_version }}</span>
      <span v-if="timings.alpn" class="code-font">alpn {{ timings.alpn }}</span>
    </p>
  </div>
</template>
