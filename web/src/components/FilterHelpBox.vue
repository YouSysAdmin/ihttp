<script setup lang="ts">
// The help, folded under a toggle, for a dialog that is a form first.
// Open by default it pushed Save off the screen. The standalone syntax
// dialog renders FilterHelp directly, since there it is the whole point.
import { ref } from 'vue'
import FilterHelp from './FilterHelp.vue'
import { getIcon } from '../layouts/icons'

withDefaults(defineProps<{ subject?: 'log' | 'request' | 'response' }>(), { subject: 'log' })

const open = ref(false)
</script>

<template>
  <div class="help-fold">
    <button type="button" class="help-fold-toggle" :aria-expanded="open" @click="open = !open">
      <span
        class="help-fold-caret"
        :class="{ open }"
        aria-hidden="true"
        v-html="getIcon('chevron-down')"
      ></span>
      Syntax help
    </button>
    <div v-if="open" class="help-box">
      <slot />
      <FilterHelp :subject="subject" />
    </div>
  </div>
</template>
