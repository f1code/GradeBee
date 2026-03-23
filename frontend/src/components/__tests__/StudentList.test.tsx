import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@clerk/react', () => ({
  useAuth: () => ({ getToken: vi.fn().mockResolvedValue('tok') }),
}))

vi.mock('../../hooks/useMediaQuery', () => ({
  useMediaQuery: () => false,
}))

describe('StudentList', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  it('shows loading state initially', async () => {
    // Never-resolving fetch to keep loading
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise(() => {})))
    const { default: StudentList } = await import('../StudentList')
    render(<StudentList />)
    expect(screen.getByTestId('student-list-loading')).toBeInTheDocument()
  })

  it('renders class groups after fetch', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({
        spreadsheetUrl: 'https://sheets/s1',
        classes: [
          { name: 'Math 101', students: [{ name: 'Alice' }, { name: 'Bob' }] },
        ],
      }),
    }))

    const { default: StudentList } = await import('../StudentList')
    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list')).toBeInTheDocument()
    })
    expect(screen.getByText('Alice')).toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  it('shows error for no_spreadsheet', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce({
      ok: false,
      json: () => Promise.resolve({ error: 'no_spreadsheet', message: 'Not found' }),
    }))

    const { default: StudentList } = await import('../StudentList')
    render(<StudentList />)

    await waitFor(() => {
      expect(screen.getByTestId('student-list-no-spreadsheet')).toBeInTheDocument()
    })
  })
})
