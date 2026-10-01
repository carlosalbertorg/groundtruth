/**
 * Thin typed fetch wrapper for groundtruth's JSON API.
 *
 * Always sends credentials (the session cookie) and always sends/expects
 * JSON. Non-2xx responses throw an ApiError carrying the server's
 * `{"error": "<code>"}` body, so callers can switch on a stable code
 * rather than parsing human-readable text.
 */

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string) {
    super(`API error ${status}: ${code}`)
    this.status = status
    this.code = code
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    credentials: 'include',
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })

  if (!res.ok) {
    let code = 'unknown_error'
    try {
      const data = (await res.json()) as { error?: string }
      if (data.error) code = data.error
    } catch {
      // Response body wasn't JSON; fall back to the generic code above.
    }
    throw new ApiError(res.status, code)
  }

  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  patch: <T>(path: string, body?: unknown) => request<T>('PATCH', path, body),
  delete: <T = void>(path: string) => request<T>('DELETE', path),
}
