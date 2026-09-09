import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { projectsApi } from '../api/projects'
import type { Project, Settings } from '../api/types'

// The project list and the one that is open.
//
// Which project is open is SERVER state: the proxy answers against it,
// and two tabs see the same one. This store mirrors it and is kept in
// step by the event stream (project.opened, project.closed,
// project.settings), so a change made in one tab lands in the other
// without a reload.
export const useProjectStore = defineStore('project', () => {
  const projects = ref<Project[]>([])
  const active = ref<Project | null>(null)
  const loaded = ref(false)

  const hasActive = computed(() => active.value !== null)
  const settings = computed<Settings | null>(() => active.value?.settings ?? null)

  async function fetchAll() {
    const [list, current] = await Promise.all([projectsApi.list(), projectsApi.active()])
    projects.value = list.data.projects ?? []
    active.value = current.data.project
    loaded.value = true
  }

  async function ensure() {
    if (!loaded.value) await fetchAll()
  }

  async function create(name: string) {
    const res = await projectsApi.create(name)
    projects.value = [res.data.project, ...projects.value]
    return res.data.project
  }

  async function open(id: string) {
    const res = await projectsApi.open(id)
    setActive(res.data.project)
    return res.data.project
  }

  async function close() {
    await projectsApi.close()
    setActive(null)
  }

  async function remove(id: string) {
    await projectsApi.remove(id)
    projects.value = projects.value.filter((p) => p.id !== id)
  }

  // Apply what the server says is open. Also what the event stream calls.
  //
  // The list holds its own copy of every project, so the row is replaced
  // with the fresh one rather than only having its is_active flag
  // flipped: the Projects table reads the row.
  function setActive(p: Project | null) {
    active.value = p
    projects.value = projects.value.map((x) => {
      const isActive = p !== null && x.id === p.id

      return isActive ? { ...p, is_active: true } : { ...x, is_active: false }
    })
  }

  // Settings the server accepted, from a PUT or from another tab.
  //
  // Written to both copies of the open project: the Projects table binds
  // its controls to the ROW, not to the active object.
  function setSettings(s: Settings) {
    const id = active.value?.id
    if (!id) return

    active.value = { ...active.value!, settings: s }
    projects.value = projects.value.map((p) => (p.id === id ? { ...p, settings: s } : p))
  }

  function onCreated(p: Project) {
    if (!projects.value.some((x) => x.id === p.id)) projects.value = [p, ...projects.value]
  }

  function onDeleted(id: string) {
    projects.value = projects.value.filter((p) => p.id !== id)
  }

  return {
    projects,
    active,
    loaded,
    hasActive,
    settings,
    fetchAll,
    ensure,
    create,
    open,
    close,
    remove,
    setActive,
    setSettings,
    onCreated,
    onDeleted,
  }
})
