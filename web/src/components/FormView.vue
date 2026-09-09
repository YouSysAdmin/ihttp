<script setup lang="ts">
// A form body as its fields. A BodyView branch rather than a tab of its
// own: it is one more way of showing a body, and the toggle back to the
// source sits where Render/Source does for HTML.
//
// A multipart part that carried a file is reported by its name, type and
// length. The bytes are not shown here - the raw endpoint has them,
// and the body reaching the console was coerced to text on the way.
import { computed, ref } from 'vue'
import CodeView from './CodeView.vue'
import { humanSize } from '../composables/humanSize'
import {
  multipartBoundary,
  multipartFields,
  urlencodedFields,
  type FormKind,
} from '../composables/cookies'

const props = defineProps<{
  body: string
  contentType: string
  kind: FormKind
  truncated?: boolean
  size?: number
  rawUrl?: string
  fill?: boolean
}>()

const fields = computed(() => {
  if (props.kind === 'multipart') {
    return multipartFields(props.body, multipartBoundary(props.contentType))
  }

  return urlencodedFields(props.body)
})

// Source rather than fields when there is nothing to show as fields: a
// truncated multipart body, or a boundary the content type never named.
const source = ref(false)
const showFields = computed(() => !source.value && fields.value.length > 0)

const label = computed(() => {
  const kind = props.kind === 'multipart' ? 'multipart form' : 'urlencoded form'

  // The source view has CodeView's own byte count under this bar, so
  // this one does not repeat it.
  if (!showFields.value) return kind

  return `${kind}, ${humanSize(props.size || props.body.length)}`
})
</script>

<template>
  <div class="body-view" :class="{ 'body-view--fill': fill }">
    <div class="code-view-bar">
      <span class="text-xs text-muted">
        {{ label }}<template v-if="showFields">, {{ fields.length }} fields</template>
        <template v-if="truncated"> (truncated)</template>
      </span>
      <div class="flex gap-2">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="fields.length === 0"
          @click="source = !source"
        >
          {{ source ? 'Fields' : 'Source' }}
        </button>
        <a v-if="rawUrl" class="btn btn-secondary btn-sm" :href="rawUrl" download>Download</a>
      </div>
    </div>

    <div v-if="showFields" class="table-wrapper">
      <table class="headers-table">
        <tbody>
          <tr v-for="(f, i) in fields" :key="i">
            <td class="header-name">
              <code>{{ f.name || '(no name)' }}</code>
            </td>
            <td class="header-value">
              <template v-if="f.filename">
                <span class="form-file">{{ f.filename }}</span>
                <span class="text-xs text-muted">
                  {{ f.contentType || 'no content type' }}, {{ humanSize(f.size) }}
                </span>
              </template>
              <template v-else>{{ f.value }}</template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <p v-else-if="fields.length === 0 && !source" class="viewer-muted">
      Nothing that reads as a field. The source is below.
    </p>

    <CodeView
      v-if="!showFields"
      :body="body"
      :content-type="contentType"
      :truncated="truncated"
      :size="size"
      :fill="fill"
    />
  </div>
</template>
