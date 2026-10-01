import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SetupPage } from './SetupPage'

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <SetupPage />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function fillAndSubmit(user: ReturnType<typeof userEvent.setup>, password: string) {
  await user.type(screen.getByLabelText('Email'), 'admin@example.com')
  // Pasted rather than typed: the passwords here are up to 74 characters.
  await user.click(screen.getByLabelText('Password'))
  await user.paste(password)
  await user.click(screen.getByLabelText('Confirm password'))
  await user.paste(password)
  await user.click(screen.getByRole('button', { name: 'Create account' }))
}

describe('SetupPage password length', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('rejects a password longer than bcrypt can hash, without calling the server', async () => {
    const user = userEvent.setup()
    renderPage()

    await fillAndSubmit(user, 'a'.repeat(73))

    expect(await screen.findByText(/at most 72 bytes/i)).toBeInTheDocument()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('counts bytes, not characters: 37 accented letters are 74 bytes', async () => {
    const user = userEvent.setup()
    renderPage()

    await fillAndSubmit(user, 'é'.repeat(37))

    expect(await screen.findByText(/at most 72 bytes/i)).toBeInTheDocument()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('accepts a password of exactly 72 bytes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 201,
        json: async () => ({ id: '1', email: 'admin@example.com' }),
      }),
    )
    const user = userEvent.setup()
    renderPage()

    await fillAndSubmit(user, 'a'.repeat(72))

    await vi.waitFor(() => {
      expect(fetch).toHaveBeenCalledWith(
        '/api/setup/admin',
        expect.objectContaining({ method: 'POST' }),
      )
    })
    expect(screen.queryByText(/at most 72 bytes/i)).not.toBeInTheDocument()
  })
})
