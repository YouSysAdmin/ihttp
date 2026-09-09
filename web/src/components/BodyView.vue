<script setup lang="ts">
// A message body, shown for what it is.
//
// Three forms, decided by the content type and the server's verdict on
// the bytes: an image renders as an image, anything else binary is a hex
// dump, and text goes to the code view with highlighting. The raw
// endpoint carries the exact bytes for the first two and for Download -
// the body in the JSON has already been coerced to text, which is fine
// for reading a response and useless for a JPEG.
import { computed, ref } from 'vue'
import CodeView from './CodeView.vue'
import HexDump from './HexDump.vue'
import GrpcView from './GrpcView.vue'
import FormView from './FormView.vue'
import GraphQLView from './GraphQLView.vue'
import { formKind } from '../composables/cookies'
import { humanSize } from '../composables/humanSize'
import { isProtobufType } from '../composables/http'

const props = withDefaults(
  defineProps<{
    body: string
    contentType?: string
    binary?: boolean
    truncated?: boolean
    // Byte count from the server. The string's length counts characters.
    size?: number
    // The raw endpoint. Required for images, hex and Download.
    rawUrl?: string
    // The body went to the client as it arrived and was not captured.
    streamed?: boolean
    // The gRPC decode endpoint, when this body is one.
    decodeUrl?: string
    // The exchange is GraphQL, as the server derived it from the
    // REQUEST body. Guessing from a response body is not on: a JSON
    // object with a data member is half the REST APIs there are.
    graphql?: boolean
    // Which half is on show, since a GraphQL request and its result
    // read nothing alike.
    request?: boolean
    fill?: boolean
  }>(),
  {
    contentType: '',
    binary: false,
    truncated: false,
    size: 0,
    rawUrl: '',
    streamed: false,
    decodeUrl: '',
    graphql: false,
    request: false,
    fill: false,
  },
)

const media = computed(() => props.contentType.split(';')[0]!.trim().toLowerCase())

const isImage = computed(() => media.value.startsWith('image/') && !!props.rawUrl)
const isHTML = computed(() => media.value.includes('html') && !props.binary && !!props.body)

// Rendered or source. Source by default: the page is the thing being
// debugged and its markup is usually what the reader came to see.
const rendered = ref(false)

// Read these bytes as a protobuf message. Off by default: a binary
// body with no content type - a WebSocket frame, say - could be
// anything, so this is an option rather than a verdict.
const asProtobuf = ref(false)
const isPDF = computed(() => media.value === 'application/pdf' && !!props.rawUrl)

// gRPC framing, or a bare protobuf message over plain HTTP: the same
// view reads both, since it is the same wire format underneath.
const isGrpc = computed(
  () =>
    !!props.decodeUrl &&
    (media.value.startsWith('application/grpc') || isProtobufType(media.value)),
)
const isHex = computed(
  () => props.binary && !isImage.value && !isPDF.value && !isGrpc.value && !!props.rawUrl,
)

// A form body is text with a shape: the fields are what the reader
// came for, and the source is one toggle away.
const form = computed(() => (props.binary ? '' : formKind(props.contentType)))

const label = computed(() => {
  const size = humanSize(props.size || props.body.length)
  return media.value ? `${media.value}, ${size}` : size
})
</script>

<template>
  <div :class="['body-view', { 'body-view--fill': fill }]">
    <template v-if="!body">
      <p v-if="streamed" class="viewer-muted">
        The body was streamed to the client and not captured.
      </p>
      <p v-else class="viewer-muted">No body.</p>
    </template>

    <template v-else-if="isGrpc">
      <div class="code-view-bar">
        <span class="text-xs text-muted">
          {{ label }}<template v-if="truncated"> (truncated)</template>
        </span>
        <a v-if="rawUrl" class="btn btn-secondary btn-sm" :href="rawUrl" download>Download</a>
      </div>
      <GrpcView :url="decodeUrl" />
    </template>

    <template v-else-if="isImage || isPDF || isHex">
      <div class="code-view-bar">
        <span class="text-xs text-muted">
          {{ label }}<template v-if="truncated"> (truncated)</template>
        </span>
        <div class="flex gap-2">
          <!-- Offered, not assumed: bytes with no content type could be
               anything, and protobuf is a guess until it decodes. -->
          <button
            v-if="isHex && decodeUrl"
            type="button"
            class="btn btn-secondary btn-sm"
            @click="asProtobuf = !asProtobuf"
          >
            {{ asProtobuf ? 'Hex' : 'Protobuf' }}
          </button>
          <a class="btn btn-secondary btn-sm" :href="rawUrl" download>Download</a>
        </div>
      </div>
      <div v-if="isImage" class="image-stage">
        <img :src="rawUrl" alt="Response image" />
      </div>
      <iframe v-else-if="isPDF" class="pdf-frame" :src="rawUrl" title="PDF body"></iframe>
      <GrpcView v-else-if="asProtobuf && decodeUrl" :url="decodeUrl" />
      <HexDump v-else :url="rawUrl" />
    </template>

    <GraphQLView
      v-else-if="graphql && !binary"
      :body="body"
      :content-type="contentType"
      :request="request"
      :truncated="truncated"
      :size="size"
      :raw-url="rawUrl"
      :fill="fill"
    />

    <FormView
      v-else-if="form"
      :body="body"
      :content-type="contentType"
      :kind="form"
      :truncated="truncated"
      :size="size"
      :raw-url="rawUrl"
      :fill="fill"
    />

    <template v-else-if="isHTML">
      <div class="code-view-bar">
        <span class="text-xs text-muted">
          {{ label }}<template v-if="truncated"> (truncated)</template>
        </span>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="rendered = !rendered">
            {{ rendered ? 'Source' : 'Render' }}
          </button>
          <a v-if="rawUrl" class="btn btn-secondary btn-sm" :href="rawUrl" download>Download</a>
        </div>
      </div>
      <!-- An empty sandbox: no scripts, no forms, an opaque origin. The
           page can draw itself and nothing else. -->
      <iframe
        v-if="rendered"
        class="html-frame"
        :srcdoc="body"
        sandbox=""
        title="Rendered response"
      ></iframe>
      <CodeView
        v-else
        :body="body"
        :content-type="contentType"
        :truncated="truncated"
        :size="size"
        :fill="fill"
      />
    </template>

    <CodeView
      v-else
      :body="body"
      :content-type="contentType"
      :truncated="truncated"
      :size="size"
      :fill="fill"
      :raw-url="rawUrl"
    />
  </div>
</template>
