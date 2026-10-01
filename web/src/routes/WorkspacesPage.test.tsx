import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { WorkspacesPage } from './WorkspacesPage'

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <WorkspacesPage email="admin@example.com" />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockFetchJSON(body: unknown) {
  return vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => body,
  })
}

describe('WorkspacesPage', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows an empty state when there are no workspaces', async () => {
    vi.stubGlobal('fetch', mockFetchJSON([]))

    renderPage()

    expect(await screen.findByText(/no workspaces yet/i)).toBeInTheDocument()
  })

  it('lists existing workspaces', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchJSON([
        {
          id: '1',
          name: 'prod-network',
          description: null,
          source_path: '/data/modules/prod-network',
          working_subdirectory: null,
          binary_kind: 'terraform',
          binary_version: null,
          credential_env_file: null,
          check_interval_minutes: 60,
          check_timeout_seconds: 600,
          is_enabled: true,
          last_check_id: null,
          last_check_status: null,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ]),
    )

    renderPage()

    expect(await screen.findByText('prod-network')).toBeInTheDocument()
    expect(screen.getByText('Terraform')).toBeInTheDocument()
    expect(screen.getByText('every 60m')).toBeInTheDocument()
  })

  it('explains why a check could not be started instead of failing silently', async () => {
    const workspace = {
      id: '1',
      name: 'prod-network',
      description: null,
      source_path: '/modules/prod-network',
      working_subdirectory: null,
      binary_kind: 'terraform',
      binary_version: null,
      credential_env_file: null,
      check_interval_minutes: 60,
      check_timeout_seconds: 600,
      is_enabled: true,
      last_check_id: null,
      last_check_status: null,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'POST') {
          return { ok: false, status: 409, json: async () => ({ error: 'check_in_progress' }) }
        }
        return { ok: true, status: 200, json: async () => [workspace] }
      }),
    )
    const user = userEvent.setup()

    renderPage()
    await user.click(await screen.findByRole('button', { name: 'Check now' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(/already running/i)
  })


  it('opens and closes the new workspace dialog', async () => {
    vi.stubGlobal('fetch', mockFetchJSON([]))
    const user = userEvent.setup()

    renderPage()
    await screen.findByText(/no workspaces yet/i)

    await user.click(screen.getByRole('button', { name: 'New workspace' }))
    expect(screen.getByLabelText('Name')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => {
      expect(screen.queryByLabelText('Name')).not.toBeInTheDocument()
    })
  })
})
