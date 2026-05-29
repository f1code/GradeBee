// frontend/src/components/AIBadge.tsx
// Reusable badge indicating AI-generated content.

interface AIBadgeProps {
  className?: string
}

export default function AIBadge({ className = '' }: AIBadgeProps) {
  return (
    <span
      className={`ai-badge ${className}`.trim()}
      title="AI-generated — review before sharing"
      aria-label="AI-generated content"
    >
      ✨ AI-generated
    </span>
  )
}
