import { useState, useEffect, useRef } from 'react'

interface InlineEditProps {
  value: string
  onSave: (newValue: string) => void
  onCancel: () => void
}

/**
 * Single-field inline rename input: Enter or blur-with-change saves (trimmed);
 * Escape or blur-with-no-change cancels. Enter unmounts the input on save,
 * so the ensuing blur's `onCancel`/`onSave` decision must be driven by the
 * component's own `value`/`text` comparison, not by re-invoking the caller.
 */
export default function InlineEdit({ value, onSave, onCancel }: InlineEditProps) {
  const [text, setText] = useState(value)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
    inputRef.current?.select()
  }, [])

  function handleBlur() {
    const trimmed = text.trim()
    if (trimmed && trimmed !== value) {
      onSave(trimmed)
    } else {
      onCancel()
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter') {
      const trimmed = text.trim()
      if (trimmed) onSave(trimmed)
      else onCancel()
    } else if (e.key === 'Escape') {
      onCancel()
    }
  }

  return (
    <input
      ref={inputRef}
      type="text"
      value={text}
      onChange={e => setText(e.target.value)}
      onBlur={handleBlur}
      onKeyDown={handleKeyDown}
      className="inline-edit-input"
      data-testid="inline-edit-input"
    />
  )
}
