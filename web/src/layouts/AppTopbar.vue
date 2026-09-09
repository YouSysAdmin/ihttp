<script setup lang="ts">
// The bar above the page: the drawer handle, the connection state, the
// theme toggle.
import { computed } from 'vue'
import { getIcon } from './icons'
import { useThemeStore } from '../stores/theme'
import { useEventsStore } from '../stores/events'

const emit = defineEmits<{ (e: 'open-menu'): void }>()

const theme = useThemeStore()
const events = useEventsStore()

const themeTitle = computed(() => (theme.isDark ? 'Switch to light' : 'Switch to dark'))
</script>

<template>
  <header class="topbar">
    <div class="bar-left">
      <button type="button" class="drawer-handle" aria-label="Open menu" @click="emit('open-menu')">
        <span class="bar-glyph" aria-hidden="true" v-html="getIcon('menu')"></span>
      </button>
    </div>

    <div class="bar-right">
      <span
        v-if="!events.connected"
        class="badge badge-warning offline"
        title="The live connection to ihttp is down. It reconnects on its own."
      >
        <span class="bar-glyph" aria-hidden="true" v-html="getIcon('wifi-off')"></span>
        Offline
      </span>

      <button
        type="button"
        class="bar-btn"
        :title="themeTitle"
        :aria-label="themeTitle"
        @click="theme.toggle()"
      >
        <span
          class="bar-glyph"
          aria-hidden="true"
          v-html="getIcon(theme.isDark ? 'sun' : 'moon')"
        ></span>
      </button>
    </div>
  </header>
</template>

<style scoped>
.topbar {
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 52px;
  padding: 0 var(--gutter);
  border-bottom: 1px solid var(--border-primary);
  background: var(--bg-primary);
  transition:
    background var(--transition-slow),
    border-color var(--transition-slow);
}

.bar-left {
  display: flex;
  align-items: center;
}

.bar-right {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-left: auto;
}

.drawer-handle {
  display: none;
  align-items: center;
  justify-content: center;
  padding: 6px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-tertiary);
  cursor: pointer;
}

.drawer-handle:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

@media (max-width: 1024px) {
  .drawer-handle {
    display: flex;
  }
}

.bar-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border: none;
  border-radius: 8px;
  background: none;
  color: var(--text-secondary);
  cursor: pointer;
  transition:
    color var(--transition),
    background var(--transition);
}

.bar-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.bar-glyph {
  display: flex;
  width: 18px;
  height: 18px;
}

.offline {
  gap: 6px;
}

@media (max-width: 640px) {
  .topbar {
    padding: 0 16px;
  }
}
</style>
