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
