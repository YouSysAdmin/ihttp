<script setup lang="ts">
// The console shell: a navigation rail, a top bar, and the routed page between them.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import AppSidebar from './AppSidebar.vue'
import AppTopbar from './AppTopbar.vue'
import { useProjectStore } from '../stores/project'
import { useInterceptStore } from '../stores/intercept'
import { useShortcuts } from '../composables/useShortcuts'
import ShortcutSheet from '../components/ShortcutSheet.vue'

const COLLAPSED_KEY = 'ihttp_sidebar_collapsed'

const projects = useProjectStore()
const intercept = useInterceptStore()

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === 'true'
  } catch {
    return false
  }
}

const collapsed = ref(readCollapsed())
const drawerOpen = ref(false)

// The switcher's open state lives here because the rail is rendered
// twice, and an open switcher must not survive as two.
const switcherOpen = ref(false)

// A click that lands outside the switcher closes it. The switcher is
// found by its data attribute, not a class, so a styling rename cannot
// leave the menu unable to close.
function onDocumentClick(e: MouseEvent) {
  if (switcherOpen.value && !(e.target as Element | null)?.closest?.('[data-project-picker]')) {
    switcherOpen.value = false
  }
}

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  try {
    localStorage.setItem(COLLAPSED_KEY, String(collapsed.value))
  } catch {
    // Not remembered, still applied.
  }
}

// ? is the shell's own key, so every page has it and no page has to
// remember to bind it. The sheet lists whatever the page bound.
const showKeys = ref(false)

useShortcuts([
  {
    keys: '?',
    label: 'Show these shortcuts',
    group: 'Everywhere',
    run: () => {
      showKeys.value = true
    },
  },
])

onMounted(() => {
  void projects.ensure()
  intercept.wire()
  document.addEventListener('click', onDocumentClick)
})

onBeforeUnmount(() => document.removeEventListener('click', onDocumentClick))
</script>

<template>
  <div class="layout" :class="{ 'sidebar-collapsed': collapsed }">
    <AppSidebar
      v-model:switcher-open="switcherOpen"
      :collapsed="collapsed"
      @toggle="toggleCollapsed"
    />

    <div class="main-wrapper">
      <AppTopbar @open-menu="drawerOpen = true" />
      <main class="main-content">
        <router-view />
      </main>
    </div>

    <ShortcutSheet v-if="showKeys" @close="showKeys = false" />

    <Transition name="overlay-fade">
      <div v-if="drawerOpen" class="sidebar-overlay" @click="drawerOpen = false" />
    </Transition>

    <Transition name="sidebar-slide">
      <AppSidebar
        v-if="drawerOpen"
        v-model:switcher-open="switcherOpen"
        drawer
        @close="drawerOpen = false"
        @navigate="drawerOpen = false"
      />
    </Transition>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  min-height: 100vh;
  background: var(--bg-secondary);
}

.main-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  min-width: 0;
  margin-left: 240px;
  transition: margin-left var(--transition-slow);
}

.sidebar-collapsed .main-wrapper {
  margin-left: 64px;
}

.main-content {
  flex: 1;
  padding: var(--gutter);
}

.sidebar-overlay {
  position: fixed;
  inset: 0;
  z-index: 35;
  background: var(--overlay);
  backdrop-filter: blur(4px);
}

.overlay-fade-enter-active {
  transition: opacity 200ms ease;
}

.overlay-fade-leave-active {
  transition: opacity 150ms ease;
}

.overlay-fade-enter-from,
.overlay-fade-leave-to {
  opacity: 0;
}

.sidebar-slide-enter-active {
  transition: transform 200ms ease;
}

.sidebar-slide-leave-active {
  transition: transform 150ms ease;
}

.sidebar-slide-enter-from,
.sidebar-slide-leave-to {
  transform: translateX(-100%);
}

@media (max-width: 1024px) {
  .main-wrapper {
    margin-left: 0 !important;
  }
}

@media (max-width: 640px) {
  .main-content {
    box-sizing: border-box;
    width: 100%;
    max-width: 100vw;
    min-width: 0;
    overflow-x: hidden;
    padding: 20px 16px;
  }
}
</style>
