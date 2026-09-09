// One date formatter for the whole console. The placeholder is a
// parameter: a column that may have no value wants "-", one that says
// whether something has ever happened wants "Never".
export function formatDate(value?: string | null, empty = '-'): string {
  if (!value) return empty
  return new Date(value).toLocaleString(undefined, clock24)
}

// 24-hour time everywhere. hourCycle h23 rather than hour12: false,
// which some engines resolve to h24 and render midnight as 24:05. The
// locale stays the browser's, so the date order is the reader's.
const clock24: Intl.DateTimeFormatOptions = { hourCycle: 'h23' }

// The same clock for a caller that wants its own parts.
function formatTimeParts(
  value: string | null | undefined,
  parts: Intl.DateTimeFormatOptions,
  empty = '-',
): string {
  if (!value) return empty
  return new Date(value).toLocaleString(undefined, { ...parts, ...clock24 })
}

// The time of day alone, hh:mm:ss, for a list where the date is shared.
export function formatClock(value?: string | null, empty = '-'): string {
  return formatTimeParts(value, { hour: '2-digit', minute: '2-digit', second: '2-digit' }, empty)
}
