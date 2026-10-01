import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'

function renderApp() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockFetchJSON(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  })
}

describe('App', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('redirects to setup when no admin account exists yet', async () => {
    vi.stubGlobal('fetch', mockFetchJSON(200, { setup_complete: false }))

    renderApp()

    expect(await screen.findByText('Create the admin account')).toBeInTheDocument()
  })

  it('redirects to login once setup is complete but no session exists', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockImplementation((url: string) => {
        if (url.includes('/setup/status')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ setup_complete: true }),
          })
        }
        // /auth/me: no session yet.
        return Promise.resolve({
          ok: false,
          status: 401,
          json: async () => ({ error: 'unauthorized' }),
        })
      }),
    )

    renderApp()

    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeInTheDocument()
  })
})
