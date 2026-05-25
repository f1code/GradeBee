export function PencilIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
      <path d="M11.5 1.5l3 3L5 14H2v-3L11.5 1.5z" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function TrashIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
      <path d="M2 4h12M5.33 4V2.67a1.33 1.33 0 011.34-1.34h2.66a1.33 1.33 0 011.34 1.34V4m2 0v9.33a1.33 1.33 0 01-1.34 1.34H4.67a1.33 1.33 0 01-1.34-1.34V4h9.34z" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      width="16" height="16" viewBox="0 0 16 16" fill="none"
      style={{ transform: open ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform 0.2s' }}
    >
      <path d="M4 6L8 10L12 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function ThumbUpIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
      <path d="M5 15H2.67A1.33 1.33 0 011.33 13.67V8A1.33 1.33 0 012.67 6.67H5M9.33 5.33V2.67A2 2 0 007.33.67L5 6.67V15h7.47a1.33 1.33 0 001.33-1.13l.92-6a1.33 1.33 0 00-1.33-1.54H9.33z" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function ThumbDownIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
      <path d="M11 1H13.33A1.33 1.33 0 0114.67 2.33V8A1.33 1.33 0 0113.33 9.33H11M6.67 10.67v2.66A2 2 0 008.67 15.33L11 9.33V1H3.53A1.33 1.33 0 002.2 2.13L1.28 8.13A1.33 1.33 0 002.61 9.67H6.67z" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function HexBullet() {
  return (
    <svg className="hex-bullet" width="14" height="14" viewBox="0 0 14 14" fill="none">
      <path d="M7 1L12.66 4.25V10.75L7 14L1.34 10.75V4.25L7 1Z" fill="#E8A317" opacity="0.7" />
    </svg>
  )
}
