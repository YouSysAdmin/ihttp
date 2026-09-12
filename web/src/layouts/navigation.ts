// What the sidebar offers. Data plus one rule: an entry that needs a
// project is shown either way, and the page itself shows the gate when
// none is open - hiding half the menu would make the product look smaller than it is.

export interface NavEntry {
  label: string
  path: string
  // Key into the icon map - see icons.ts.
  icon: string
  // A mark beside the label: the intercept queue's count, or a warning
  // that the log is paused.
  badge?: 'intercept' | 'log-paused'
}

export interface NavGroup {
  id: string
  title: string
  entries: NavEntry[]
}

export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'proxy',
    title: 'Proxy',
    entries: [
      { label: 'Request log', path: '/logs', icon: 'list', badge: 'log-paused' },
      { label: 'Intercept', path: '/intercept', icon: 'pause', badge: 'intercept' },
      { label: 'Rules', path: '/rules', icon: 'shuffle' },
      { label: 'Hosts', path: '/hosts', icon: 'signpost' },
      { label: 'Scope', path: '/scope', icon: 'target' },
    ],
  },
  {
    id: 'tools',
    title: 'Tools',
    entries: [
      { label: 'Sender', path: '/sender', icon: 'send' },
      { label: 'Automation', path: '/automation', icon: 'zap' },
      { label: 'Decoder', path: '/decoder', icon: 'code' },
    ],
  },
  {
    id: 'workspace',
    title: 'Workspace',
    entries: [
      { label: 'Projects', path: '/projects', icon: 'briefcase' },
      { label: 'Setup', path: '/setup', icon: 'terminal' },
      { label: 'Proxies', path: '/proxies', icon: 'server' },
      { label: 'Settings', path: '/settings', icon: 'settings' },
    ],
  },
]
