/** Error thrown for non-2xx responses; mirrors the server's {error, message} envelope. */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

/** Minimal JSON fetch wrapper. Cookies are sent automatically (same origin). */
export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (init.body) headers['Content-Type'] = 'application/json'
  const res = await fetch(path, { credentials: 'same-origin', ...init, headers: { ...headers, ...(init.headers as Record<string, string> | undefined) } })
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = null
  }
  if (!res.ok) {
    const e = (data ?? {}) as { error?: string; message?: string }
    throw new ApiError(res.status, e.error ?? 'http_error', e.message ?? res.statusText)
  }
  return data as T
}

export function isApiError(err: unknown, status?: number): err is ApiError {
  return err instanceof ApiError && (status === undefined || err.status === status)
}
