<script setup lang="ts">
// Which project is open, and how to change it, in the rail.
//
// Its own component because it is the one thing in the rail that is
// not navigation: it does not link anywhere, it changes what every
// other link MEANS.
//
// `narrow` is a prop rather than something read off the rail: a scoped
// rule reaches a child component's root element and nothing deeper, so
// `.rail.narrow .picker-open` written in the parent would stop matching
// the moment this markup moved in here.
import { useRouter } from 'vue-router'
import { useProjectStore } from '../stores/project'
import { useNotificationStore } from '../stores/notification'
import { apiErrorMessage } from '../api/client'
import { getIcon } from './icons'

const props = defineProps<{
  // Icons only - the tile stands in for the whole control.
  narrow?: boolean
}>()

const emit = defineEmits<{
  // Somewhere was navigated to, so a drawer should dismiss itself.
  (e: 'navigate'): void
}>()

const router = useRouter()
const projects = useProjectStore()
const notify = useNotificationStore()

const open = defineModel<boolean>('open', { default: false })

function initial(name: string | undefined) {
  return name?.charAt(0).toUpperCase() || '-'
}

// Named rather than written inline: prettier reflows a multi-statement
// handler attribute across lines and drops the separator, and the
// template compiler then refuses the file.
function leaveFor(path: string) {
  open.value = false
  emit('navigate')
  router.push(path)
}

// Opening is server state, so the page stays where it is and reloads
// itself when the store announces the change. The menu closes at once,
// since it is the press being acknowledged.
async function choose(id: string) {
  open.value = false
  if (projects.active?.id === id) return
  try {
    await projects.open(id)
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not open the project'))
  }
}

async function close() {
  open.value = false
  try {
    await projects.close()
    notify.info('Project closed - the proxy forwards without logging')
  } catch (e) {
    notify.error(apiErrorMessage(e, 'Could not close the project'))
  }
}
</script>

<template>
  <!-- The attribute, not the class, is what the layout's click-outside
       asks about. A class is styling and gets renamed on a styling
       change, which would silently leave the menu unable to close. -->
  <div class="picker" :class="{ narrow: props.narrow }" data-project-picker>
    <button
      type="button"
      class="picker-open"
      :title="projects.active?.name ?? 'No project open'"
      :aria-expanded="open"
      aria-haspopup="menu"
      @click="open = !open"
    >
      <div class="picker-who">
        <div class="initial" :class="{ off: !projects.active }">
          {{ initial(projects.active?.name) }}
        </div>
        <span v-if="!props.narrow" class="picker-name" :class="{ muted: !projects.active }">
          {{ projects.active?.name ?? 'No project open' }}
        </span>
      </div>
      <span
        v-if="!props.narrow"
        class="picker-caret"
        aria-hidden="true"
        v-html="getIcon('chevron-down')"
      ></span>
    </button>

    <div v-if="open" class="picker-menu" role="menu">
      <button
        v-for="proj in projects.projects"
        :key="proj.id"
        type="button"
        class="picker-row"
        :class="{ on: projects.active?.id === proj.id }"
        role="menuitem"
        @click="choose(proj.id)"
      >
        <div class="initial initial-sm">{{ initial(proj.name) }}</div>
        <span class="picker-row-name">{{ proj.name }}</span>
      </button>
      <div v-if="projects.projects.length === 0" class="picker-empty">No projects yet</div>

      <div class="picker-rule"></div>

      <button
        type="button"
        class="picker-row"
        role="menuitem"
        @click="leaveFor('/projects?create=1')"
      >
        <span class="picker-glyph" v-html="getIcon('plus')"></span>
        <span>Create project</span>
      </button>
      <button
        v-if="projects.active"
        type="button"
        class="picker-row"
        role="menuitem"
        @click="close"
      >
        <span class="picker-glyph" v-html="getIcon('x')"></span>
        <span>Close project</span>
      </button>
      <button type="button" class="picker-row" role="menuitem" @click="leaveFor('/projects')">
        <span class="picker-glyph" v-html="getIcon('briefcase')"></span>
        <span>Manage projects</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.picker {
  position: relative;
  padding: 0 10px 10px;
}

.picker-open {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border: 1px solid var(--sidebar-border);
  border-radius: var(--radius);
  background: none;
  color: var(--sidebar-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.picker-open:hover {
  background: var(--sidebar-hover);
  color: var(--sidebar-text-active);
}

/* Narrowed there is only the tile left, so the frame around it would be
   a box drawn around a box. */
.picker.narrow .picker-open {
  justify-content: center;
  padding: 8px;
  border: none;
}

.picker-who {
  display: flex;
  overflow: hidden;
  align-items: center;
  gap: 8px;
}

.picker-name {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.picker-name.muted {
  color: var(--text-muted);
  font-weight: 400;
}

.picker-caret,
.picker-glyph {
  display: flex;
  flex-shrink: 0;
  width: 15px;
  height: 15px;
}

/* The project's first letter, standing in for a logo. Sized through a
   variable so the smaller one in the menu is one override. */
.initial {
  --tile: 24px;
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: var(--tile);
  height: var(--tile);
  border-radius: 6px;
  background: var(--primary-600);
  color: var(--text-on-primary);
  font-size: 12px;
  font-weight: 600;
}

.initial.off {
  background: var(--bg-tertiary);
  color: var(--text-muted);
}

.initial-sm {
  --tile: 20px;
  border-radius: 5px;
  font-size: 10px;
}

.picker-menu {
  position: absolute;
  top: calc(100% + 2px);
  right: 10px;
  left: 10px;
  z-index: 200;
  min-width: 200px;
  padding: 4px;
  border: 1px solid var(--border-primary);
  border-radius: var(--radius);
  background: var(--bg-popover);
  box-shadow: var(--shadow-lg);
}

/* Every line in the menu is one of these, whether it picks a project or
   does something else. */
.picker-row {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border: none;
  border-radius: var(--radius-sm);
  background: none;
  color: var(--text-secondary);
  font: inherit;
  text-align: left;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition:
    background var(--transition),
    color var(--transition);
}

.picker-row:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.picker-row.on {
  background: var(--bg-active);
  color: var(--accent-fg);
}

.picker-row-name {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.picker-empty {
  padding: 8px 10px;
  color: var(--text-muted);
  font-size: 13px;
}

.picker-rule {
  height: 1px;
  margin: 4px 6px;
  background: var(--border-primary);
}
</style>
