import { createRouter, createWebHistory } from 'vue-router'

const APP_NAME = 'iHTTP'

const routes = [
  {
    path: '/',
    component: () => import('../layouts/DashboardLayout.vue'),
    children: [
      { path: '', redirect: '/logs' },
      {
        path: 'logs',
        name: 'logs',
        component: () => import('../views/proxy/Logs.vue'),
        meta: { title: 'Request log' },
      },
      {
        // The same page: an id selects an entry inside it.
        path: 'logs/:id',
        name: 'log-entry',
        component: () => import('../views/proxy/Logs.vue'),
        meta: { title: 'Request log' },
      },
      {
        path: 'intercept',
        name: 'intercept',
        component: () => import('../views/proxy/Intercept.vue'),
        meta: { title: 'Intercept' },
      },
      {
        path: 'intercept/:id',
        name: 'intercept-item',
        component: () => import('../views/proxy/Intercept.vue'),
        meta: { title: 'Intercept' },
      },
      {
        path: 'rules',
        name: 'rules',
        component: () => import('../views/proxy/Rules.vue'),
        meta: { title: 'Rules' },
      },
      {
        path: 'decoder',
        name: 'decoder',
        component: () => import('../views/tools/Decoder.vue'),
        meta: { title: 'Decoder' },
      },
      {
        path: 'sender',
        name: 'sender',
        component: () => import('../views/sender/Sender.vue'),
        meta: { title: 'Sender' },
      },
      {
        path: 'sender/:id',
        name: 'sender-request',
        component: () => import('../views/sender/Sender.vue'),
        meta: { title: 'Sender' },
      },
      {
        path: 'automation',
        name: 'automation',
        component: () => import('../views/tools/Automation.vue'),
        meta: { title: 'Automation' },
      },
      {
        path: 'automation/:id',
        name: 'automation-job',
        component: () => import('../views/tools/Automation.vue'),
        meta: { title: 'Automation' },
      },
      {
        path: 'compare',
        name: 'compare',
        component: () => import('../views/compare/Compare.vue'),
        meta: { title: 'Compare' },
      },
      {
        path: 'scope',
        name: 'scope',
        component: () => import('../views/scope/Scope.vue'),
        meta: { title: 'Scope' },
      },
      {
        path: 'projects',
        name: 'projects',
        component: () => import('../views/projects/Projects.vue'),
        meta: { title: 'Projects' },
      },
      {
        path: 'setup',
        name: 'setup',
        component: () => import('../views/setup/Setup.vue'),
        meta: { title: 'Setup' },
      },
      {
        path: 'proxies',
        name: 'proxies',
        component: () => import('../views/proxies/Proxies.vue'),
        meta: { title: 'Proxies' },
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('../views/settings/Settings.vue'),
        meta: { title: 'Settings' },
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('../views/NotFound.vue'),
    meta: { title: 'Page not found' },
  },
]

const router = createRouter({
  history: createWebHistory('/'),
  routes,
})

const CHUNK_RELOAD = 'ihttp:chunk-reload'

router.afterEach((to, _from, failure) => {
  if (failure) return

  const pageTitle = to.meta.title as string | undefined
  document.title = pageTitle ? `${pageTitle} - ${APP_NAME}` : APP_NAME

  sessionStorage.removeItem(CHUNK_RELOAD)
})

// Every view is lazy. After an upgrade the chunk this document names is
// gone, so a click does nothing. Reload once to pick up the new shell.
router.onError((err, to) => {
  const message = String((err as Error)?.message ?? err)
  if (!/module script failed|dynamically imported module/i.test(message)) return
  if (sessionStorage.getItem(CHUNK_RELOAD)) return
  sessionStorage.setItem(CHUNK_RELOAD, '1')
  window.location.assign(router.resolve(to).href)
})

export default router
