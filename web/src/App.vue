<script setup lang="ts">
// The root: the routed page, plus the two surfaces that outlive it.
//
// The confirm dialog and the toast stack are mounted here rather than
// per view because both answer requests raised from anywhere - a view
// that owned them would take them down on navigation.
//
// Instantiating the theme store is a side effect and the point of the
// call: constructing it stamps `data-theme` on the document before the
// first paint. The events store opens the one live connection to the
// server that every page reads from.
import ConfirmDialog from './components/ConfirmDialog.vue'
import ToastStack from './components/ToastStack.vue'
import { useThemeStore } from './stores/theme'
import { useEventsStore } from './stores/events'

useThemeStore()
useEventsStore().connect()
</script>

<template>
  <router-view />
  <ConfirmDialog />
  <ToastStack />
</template>
