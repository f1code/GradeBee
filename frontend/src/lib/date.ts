// Formats a note's `YYYY-MM-DD` date for display.
//
// Splits the parts and builds a local Date rather than `new Date(dateStr)`, which
// parses a bare date as UTC midnight and so renders the previous day west of
// Greenwich.
export function formatNoteDate(dateStr: string): string {
  const [year, month, day] = dateStr.split('-').map(Number)
  const d = new Date(year, month - 1, day)
  return d.toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' })
}

// Formats a report's `YYYY-MM-DD` range, e.g. "Oct 1, 2026 – Oct 10, 2026".
export function formatDateRange(startDate: string, endDate: string): string {
  const fmt = (d: string) => {
    const date = new Date(d + 'T00:00:00')
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
  }
  return `${fmt(startDate)} – ${fmt(endDate)}`
}
