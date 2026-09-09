// The console's icon set, one map. Feather glyphs refitted to an 18px
// grid at stroke 1.5, currentColor, round caps.
//
//   Feather - https://feathericons.com
//   MIT License, Copyright (c) 2013-2023 Cole Bemis
//
// Adding one means matching the grid, or it reads as a different weight
// beside its neighbours.

// One glyph per line, keys quoted.
// prettier-ignore
export const ICONS: Record<string, string> = {
    'briefcase': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><rect x="2" y="6" width="14" height="10" rx="1.5" stroke="currentColor" stroke-width="1.5"/><path d="M12 6V4.5A1.5 1.5 0 0010.5 3h-3A1.5 1.5 0 006 4.5V6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'list': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M6.75 4.5h9M6.75 9h9M6.75 13.5h9M2.25 4.5h.007M2.25 9h.007M2.25 13.5h.007" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'pause': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><rect x="4" y="3" width="3.5" height="12" rx="1" stroke="currentColor" stroke-width="1.5"/><rect x="10.5" y="3" width="3.5" height="12" rx="1" stroke="currentColor" stroke-width="1.5"/></svg>',
    'send': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M16.5 1.5L8.25 9.75M16.5 1.5l-5.25 15-3-6.75L1.5 6.75l15-5.25z" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'target': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><circle cx="9" cy="9" r="7" stroke="currentColor" stroke-width="1.5"/><circle cx="9" cy="9" r="4" stroke="currentColor" stroke-width="1.5"/><circle cx="9" cy="9" r="1" fill="currentColor"/></svg>',
    'settings': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><circle cx="9" cy="9" r="2.25" stroke="currentColor" stroke-width="1.5"/><path d="M14.7 11.1a1.2 1.2 0 00.24 1.32l.04.04a1.46 1.46 0 11-2.06 2.06l-.04-.04a1.2 1.2 0 00-1.32-.24 1.2 1.2 0 00-.73 1.1v.12a1.46 1.46 0 01-2.91 0v-.06a1.2 1.2 0 00-.79-1.1 1.2 1.2 0 00-1.32.24l-.04.04a1.46 1.46 0 11-2.06-2.06l.04-.04a1.2 1.2 0 00.24-1.32 1.2 1.2 0 00-1.1-.73h-.12a1.46 1.46 0 010-2.91h.06a1.2 1.2 0 001.1-.79 1.2 1.2 0 00-.24-1.32l-.04-.04a1.46 1.46 0 112.06-2.06l.04.04a1.2 1.2 0 001.32.24h.06a1.2 1.2 0 00.73-1.1v-.12a1.46 1.46 0 012.91 0v.06a1.2 1.2 0 00.73 1.1 1.2 1.2 0 001.32-.24l.04-.04a1.46 1.46 0 112.06 2.06l-.04.04a1.2 1.2 0 00-.24 1.32v.06a1.2 1.2 0 001.1.73h.12a1.46 1.46 0 010 2.91h-.06a1.2 1.2 0 00-1.1.73z" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'x': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M13.5 4.5l-9 9M4.5 4.5l9 9" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'menu': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M2.25 4.5h13.5M2.25 9h13.5M2.25 13.5h13.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'panel-open': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><rect x="2.25" y="2.25" width="13.5" height="13.5" rx="1.5" stroke="currentColor" stroke-width="1.5"/><path d="M6.75 2.25v13.5" stroke="currentColor" stroke-width="1.5"/><path d="M10.5 6.75L12.75 9l-2.25 2.25" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'panel-shut': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><rect x="2.25" y="2.25" width="13.5" height="13.5" rx="1.5" stroke="currentColor" stroke-width="1.5"/><path d="M6.75 2.25v13.5" stroke="currentColor" stroke-width="1.5"/><path d="M12 11.25L9.75 9 12 6.75" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'chevron-down': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M4.5 6.75L9 11.25l4.5-4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'shuffle': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M12 2.25h3.75V6M15.75 2.25L2.25 15.75M2.25 2.25l4.5 4.5M11.25 11.25l4.5 4.5M12 15.75h3.75V12" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'code': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M12 13.5L16.5 9 12 4.5M6 4.5L1.5 9 6 13.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'sun': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><circle cx="9" cy="9" r="3" stroke="currentColor" stroke-width="1.5"/><path d="M9 1.5v1.5M9 15v1.5M3.7 3.7l1.06 1.06M13.24 13.24l1.06 1.06M1.5 9H3M15 9h1.5M3.7 14.3l1.06-1.06M13.24 4.76l1.06-1.06" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'moon': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M15.75 9.56A6.75 6.75 0 018.44 2.25 6.75 6.75 0 1015.75 9.56z" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'x-circle': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><circle cx="9" cy="9" r="7" stroke="currentColor" stroke-width="1.5"/><path d="M11.25 6.75l-4.5 4.5M6.75 6.75l4.5 4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'alert-triangle': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M7.86 2.87L1.21 14.25a1.31 1.31 0 001.14 1.97h13.3a1.31 1.31 0 001.14-1.97L10.14 2.87a1.31 1.31 0 00-2.28 0z" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/><path d="M9 6.75v3M9 12.75h.007" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'info': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><circle cx="9" cy="9" r="7" stroke="currentColor" stroke-width="1.5"/><path d="M9 12v-3M9 6h.007" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'plus': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M9 3.75v10.5M3.75 9h10.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'zap': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M9.75 1.5L2.25 10.5h6.75l-.75 6 7.5-9H9l.75-6z" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'terminal': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><rect x="2" y="2.25" width="14" height="13.5" rx="1.5" stroke="currentColor" stroke-width="1.5"/><path d="M5.25 7.5L7.5 9.75 5.25 12M9.75 12h3" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
    'server': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><rect x="2" y="2.25" width="14" height="5.5" rx="1.5" stroke="currentColor" stroke-width="1.5"/><rect x="2" y="10.25" width="14" height="5.5" rx="1.5" stroke="currentColor" stroke-width="1.5"/><path d="M5 5h.007M5 13h.007" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>',
    'wifi-off': '<svg width="18" height="18" viewBox="0 0 18 18" fill="none"><path d="M1.5 1.5l15 15M12.4 12.4A5 5 0 009 11.25a5 5 0 00-3.5 1.4M14.8 9.3a8.5 8.5 0 00-2.6-1.6M3.2 9.3a8.5 8.5 0 013.9-2M9 15h.007" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
}

// getIcon returns the markup for a glyph, or an empty string when the
// name is unknown.
export function getIcon(name: string): string {
  return ICONS[name] ?? ''
}
