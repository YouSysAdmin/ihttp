<script setup lang="ts">
// The navigation rail: the brand, the project switcher, and the grouped
// menu. One component for both the fixed rail and the slide-in drawer.
//
// The switcher is not navigation, it changes what every link here
// MEANS, so it is its own component and this file only says where it
// sits and how wide it is.
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { NAV_GROUPS } from './navigation'
import { getIcon } from './icons'
import BrandMark from '../components/BrandMark.vue'
import ProjectSwitcher from './ProjectSwitcher.vue'
import { useInterceptStore } from '../stores/intercept'
import { useProjectStore } from '../stores/project'

const props = defineProps<{
  // Icons only, no labels. Ignored in a drawer, which is never narrow.
  collapsed?: boolean
  // Rendered as the slide-in drawer rather than the fixed rail.
  drawer?: boolean
}>()

const emit = defineEmits<{
  (e: 'toggle'): void
  (e: 'close'): void
  (e: 'navigate'): void
}>()

const route = useRoute()
const intercept = useInterceptStore()
const projects = useProjectStore()

// The open project's log setting, so a paused log is visible from every
// page and not only from the one that paused it.
const logPaused = computed(() => !!projects.settings?.request_log.paused)

const narrow = computed(() => props.collapsed && !props.drawer)

// Owned by the layout: the rail is rendered twice, as the rail and as
// the drawer, and an open switcher must not survive as two.
const switcherOpen = defineModel<boolean>('switcherOpen', { default: false })

function isCurrent(path: string) {
  return route.path === path || route.path.startsWith(path + '/')
}
</script>

<template>
  <aside class="rail" :class="{ narrow: narrow, drawer }">
    <div class="rail-head">
      <router-link class="brand" to="/logs" aria-label="Request log">
        <BrandMark class="mark" :size="30" />
        <span class="wordmark">iHTTP</span>
      </router-link>

      <button
        v-if="drawer"
        type="button"
        class="head-btn"
        title="Close menu"
        aria-label="Close menu"
        @click="emit('close')"
      >
        <span class="glyph" aria-hidden="true" v-html="getIcon('x')"></span>
      </button>
      <button
        v-else
        type="button"
        class="head-btn"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        @click="emit('toggle')"
      >
        <span
          class="glyph"
          aria-hidden="true"
          v-html="getIcon(collapsed ? 'panel-open' : 'panel-shut')"
        ></span>
      </button>
    </div>

    <ProjectSwitcher v-model:open="switcherOpen" :narrow="narrow" @navigate="emit('navigate')" />

    <nav class="menu">
      <div v-for="group in NAV_GROUPS" :key="group.id" class="menu-group">
        <div v-if="!narrow" class="group-head">{{ group.title }}</div>

        <router-link
          v-for="entry in group.entries"
          :key="entry.path"
          class="link"
          :class="{ here: isCurrent(entry.path) }"
          :title="narrow ? entry.label : ''"
          :to="entry.path"
          @click="emit('navigate')"
        >
          <span class="glyph" v-html="getIcon(entry.icon)"></span>
          <span v-if="!narrow" class="text">{{ entry.label }}</span>
          <span
            v-if="entry.badge === 'intercept' && intercept.count > 0"
            class="count"
            :class="{ dot: narrow }"
          >
            <template v-if="!narrow">{{ intercept.count }}</template>
          </span>
          <!-- A paused log looks exactly like a quiet one, so say so
               here as well as on the page. -->
          <span
            v-else-if="entry.badge === 'log-paused' && logPaused"
            class="count"
            :class="{ dot: narrow }"
            title="Recording is paused"
          >
            <template v-if="!narrow">paused</template>
          </span>
        </router-link>
      </div>
    </nav>
  </aside>
</template>

<style scoped>
.rail {
  position: fixed;
  inset: 0 auto 0 0;
  z-index: 40;
  display: flex;
  flex-direction: column;
  width: 240px;
  background: var(--bg-sidebar);
  border-right: 1px solid var(--sidebar-border);
  transition: width var(--transition-slow);
}

.rail.narrow {
  width: 64px;
}

.rail.drawer {
  z-index: 45;
}

@media (max-width: 1024px) {
  .rail:not(.drawer) {
    display: none;
  }
}

.rail-head {
  position: relative;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 10px;
  padding: 16px 14px 12px;
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  color: var(--sidebar-text-active);
  text-decoration: none;
}

.mark {
  flex-shrink: 0;
}

.wordmark {
  overflow: hidden;
  font-size: 15px;
  font-weight: 600;
  white-space: nowrap;
  transition: opacity var(--transition-slow);
}

.rail.narrow .wordmark {
  width: 0;
  opacity: 0;
}

.head-btn {
  position: absolute;
  top: 18px;
  right: 10px;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  padding: 4px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--sidebar-text);
  cursor: pointer;
  transition:
    color var(--transition),
    background var(--transition);
}

.head-btn:hover {
  background: var(--sidebar-hover);
  color: var(--sidebar-text-active);
}

.rail.narrow .head-btn {
  position: fixed;
  top: 12px;
  right: auto;
  left: calc(64px + 12px);
  z-index: 999;
  border: 1px solid var(--border-primary);
  background: var(--bg-primary);
}

.menu {
  flex: 1;
  overflow-y: auto;
  padding: 0 10px 20px;
}

.menu-group + .menu-group {
  margin-top: 14px;
}

.group-head {
  padding: 4px 10px 6px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.link {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 34px;
  padding: 0 10px;
  border-radius: var(--radius);
  color: var(--sidebar-text);
  font-size: 14px;
  text-decoration: none;
  transition:
    color var(--transition),
    background var(--transition);
}

.rail.narrow .link {
  position: relative;
  justify-content: center;
  padding: 0;
}

.link:hover {
  background: var(--sidebar-hover);
  color: var(--sidebar-text-active);
}

.link.here {
  background: var(--sidebar-active-bg);
  color: var(--sidebar-active-text);
  font-weight: 500;
}

.glyph {
  display: flex;
  flex-shrink: 0;
  width: 18px;
  height: 18px;
}

.text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.count {
  min-width: 20px;
  padding: 0 6px;
  border-radius: 9999px;
  background: var(--warning-fg);
  color: var(--text-on-status);
  font-size: 11px;
  font-weight: 600;
  line-height: 18px;
  text-align: center;
}

.count.dot {
  position: absolute;
  right: 14px;
  min-width: 8px;
  height: 8px;
  padding: 0;
}
</style>
