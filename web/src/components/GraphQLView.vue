<script setup lang="ts">
// A GraphQL body, read as GraphQL. A BodyView branch like the form and
// HTML ones, so it is one more way of showing a body rather than a tab
// somewhere else.
//
// The REQUEST shows the document and its variables apart, which is how
// they were sent and how they are read - a document escaped into one
// JSON string is unreadable, and that is what every client sends.
//
// The RESPONSE shows the errors FIRST and the data under them. A failed
// GraphQL call is usually a 200, so errors that come below a screenful
// of data are errors nobody sees.
import { computed, ref } from 'vue'
import CodeView from './CodeView.vue'
import { humanSize } from '../composables/humanSize'

const props = defineProps<{
  body: string
  contentType: string
  // The request half, as against the result.
  request: boolean
  truncated?: boolean
  size?: number
  rawUrl?: string
  fill?: boolean
}>()

interface Parsed {
  query: string
  variables: unknown
  operationName: string
  batch: number
  data: unknown
  errors: unknown[]
}

// One parse, of the body already in hand. A body that will not parse
// falls back to the source, which is what BodyView would have shown.
const parsed = computed<Parsed | null>(() => {
  // application/graphql is the document itself rather than JSON.
  if (props.contentType.split(';')[0]!.trim().toLowerCase() === 'application/graphql') {
    return {
      query: props.body,
      variables: null,
      operationName: '',
      batch: 1,
      data: null,
      errors: [],
    }
  }

  let value: unknown
  try {
    value = JSON.parse(props.body)
  } catch {
    return null
  }

  const first = Array.isArray(value) ? value[0] : value
  if (!first || typeof first !== 'object') return null

  const o = first as Record<string, unknown>

  return {
    query: typeof o.query === 'string' ? o.query : '',
    variables: o.variables ?? null,
    operationName: typeof o.operationName === 'string' ? o.operationName : '',
    batch: Array.isArray(value) ? value.length : 1,
    data: o.data ?? null,
    errors: Array.isArray(o.errors) ? o.errors : [],
  }
})

const variables = computed(() =>
  parsed.value?.variables == null ? '' : JSON.stringify(parsed.value.variables, null, 2),
)
const data = computed(() =>
  parsed.value?.data == null ? '' : JSON.stringify(parsed.value.data, null, 2),
)
const errors = computed(() => parsed.value?.errors ?? [])

// The source is one click away, because what was actually sent is what
// a protocol problem is read from.
const source = ref(false)
const readable = computed(() => !source.value && !!parsed.value)

const label = computed(() => {
  const bytes = humanSize(props.size || props.body.length)
  if (!props.request) return `GraphQL result, ${bytes}`

  const batch = parsed.value && parsed.value.batch > 1 ? `, ${parsed.value.batch} operations` : ''

  return `GraphQL request, ${bytes}${batch}`
})

// One error's message and where it happened, without pretending to know
// the rest of the shape.
function errorLine(e: unknown): string {
  if (!e || typeof e !== 'object') return String(e)

  const o = e as Record<string, unknown>
  const message = typeof o.message === 'string' ? o.message : JSON.stringify(o)
  const path = Array.isArray(o.path) ? o.path.join('.') : ''

  return path ? `${message}  (at ${path})` : message
}

function errorDetail(e: unknown): string {
  return JSON.stringify(e, null, 2)
}

const openError = ref<number | null>(null)
</script>

<template>
  <div class="body-view" :class="{ 'body-view--fill': fill }">
    <div class="code-view-bar">
      <span class="text-xs text-muted">
        {{ label }}<template v-if="truncated"> (truncated)</template>
      </span>
      <div class="flex gap-2">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="!parsed"
          @click="source = !source"
        >
          {{ source ? 'GraphQL' : 'Source' }}
        </button>
        <a v-if="rawUrl" class="btn btn-secondary btn-sm" :href="rawUrl" download>Download</a>
      </div>
    </div>

    <template v-if="readable && request">
      <h4 class="gql-title">
        Query<template v-if="parsed?.operationName"> - {{ parsed.operationName }}</template>
      </h4>
      <pre class="gql-query code-font">{{ parsed?.query || 'No document.' }}</pre>

      <template v-if="variables">
        <h4 class="gql-title">Variables</h4>
        <CodeView :body="variables" content-type="application/json" />
      </template>

      <p v-if="parsed && parsed.batch > 1" class="viewer-muted">
        {{ parsed.batch }} operations came in this request. The first is shown - the source has them
        all.
      </p>
    </template>

    <template v-else-if="readable">
      <template v-if="errors.length">
        <h4 class="gql-title gql-title--danger">
          {{ errors.length }} error<template v-if="errors.length > 1">s</template>
        </h4>
        <ul class="gql-errors">
          <li v-for="(e, i) in errors" :key="i">
            <button
              type="button"
              class="link-button"
              :aria-expanded="openError === i"
              @click="openError = openError === i ? null : i"
            >
              {{ errorLine(e) }}
            </button>
            <pre v-if="openError === i" class="gql-error-detail code-font">{{
              errorDetail(e)
            }}</pre>
          </li>
        </ul>
      </template>

      <h4 class="gql-title">Data</h4>
      <CodeView v-if="data" :body="data" content-type="application/json" :fill="fill" />
      <p v-else class="viewer-muted">No data. The whole operation failed rather than part of it.</p>
    </template>

    <CodeView
      v-else
      :body="body"
      :content-type="contentType"
      :truncated="truncated"
      :size="size"
      :fill="fill"
    />
  </div>
</template>
