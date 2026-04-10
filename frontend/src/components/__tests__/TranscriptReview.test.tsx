import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import TranscriptReview from '../TranscriptReview'

describe('TranscriptReview', () => {
  const defaultProps = {
    transcript: 'Today I observed that Emma did great on her math test. Jacob was struggling with reading.',
    noteLinks: [
      { name: 'Emma', noteId: 1, studentId: 10, className: 'Class A' },
      { name: 'Jacob', noteId: 2, studentId: 11, className: 'Class A' },
    ],
  }

  it('renders transcript text', () => {
    render(<TranscriptReview {...defaultProps} />)
    expect(screen.getByText(/Today I observed/)).toBeInTheDocument()
  })

  it('renders student note links', () => {
    render(<TranscriptReview {...defaultProps} />)
    expect(screen.getByText('Emma')).toBeInTheDocument()
    expect(screen.getByText('Jacob')).toBeInTheDocument()
  })

  it('shows class name for each student', () => {
    render(<TranscriptReview {...defaultProps} />)
    expect(screen.getAllByText('Class A')).toHaveLength(2)
  })

  it('renders nothing when transcript is empty', () => {
    const { container } = render(<TranscriptReview transcript="" noteLinks={[]} />)
    expect(container.firstChild).toBeNull()
  })
})
