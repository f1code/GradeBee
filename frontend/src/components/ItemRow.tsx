import { useState, type ReactNode } from 'react'
import { motion, AnimatePresence } from 'motion/react'
import { ChevronIcon, TrashIcon } from './Icons'

interface ItemRowProps {
  name: string
  expanded: boolean
  onToggle: () => void
  onDelete: () => void
  actions?: ReactNode
  badge?: ReactNode
  children: ReactNode
}

export default function ItemRow({
  name,
  expanded,
  onToggle,
  onDelete,
  actions,
  badge,
  children,
}: ItemRowProps) {
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  function handleDeleteClick(e: React.MouseEvent) {
    e.stopPropagation()
    setConfirmingDelete(true)
  }

  function handleConfirm() {
    setConfirmingDelete(false)
    onDelete()
  }

  function handleCancel() {
    setConfirmingDelete(false)
  }

  if (confirmingDelete) {
    return (
      <div className="delete-confirm delete-confirm-inline">
        <span>
          Delete <strong>{name}</strong>?
        </span>
        <div className="delete-confirm-actions">
          <button className="btn-secondary btn-sm" onClick={handleCancel}>
            Cancel
          </button>
          <button className="btn-danger btn-sm" onClick={handleConfirm}>
            Delete
          </button>
        </div>
      </div>
    )
  }

  return (
    <>
      <div
        className="item-row"
        onClick={onToggle}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            onToggle()
          }
        }}
      >
        <span
          className={`item-row-name${expanded ? ' item-row-name-active' : ''}`}
        >
          {name}
          {badge}
          <ChevronIcon open={expanded} />
        </span>
        <div className="item-row-actions" onClick={(e) => e.stopPropagation()}>
          {actions}
          <button
            className="icon-btn icon-btn-danger"
            onClick={handleDeleteClick}
            aria-label={`Delete ${name}`}
          >
            <TrashIcon />
          </button>
        </div>
      </div>
      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.15 }}
            style={{ overflow: 'hidden' }}
          >
            {children}
          </motion.div>
        )}
      </AnimatePresence>
    </>
  )
}
