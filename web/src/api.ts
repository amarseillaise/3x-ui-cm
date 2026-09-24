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

/** Reads and ordinary writes. A stalled connection must not hang the UI. */
export const DEFAULT_TIMEOUT_MS = 15_000
/** Calls that make the panel write; these legitimately take longer. */
export const PANEL_WRITE_TIMEOUT_MS = 40_000

/**
 * Combines the caller's own cancellation with a deadline. Without a deadline a
 * stalled request never settles and the screen that awaits it stays frozen.
 */
function withDeadline(signal: AbortSignal | null | undefined, ms: number): AbortSignal {
  const deadline = AbortSignal.timeout(ms)
  if (!signal) return deadline
  return typeof AbortSignal.any === 'function' ? AbortSignal.any([signal, deadline]) : deadline
}

/** Turns a fetch rejection into the same shape as a server error. */
function asApiError(err: unknown): ApiError {
  if (err instanceof DOMException && err.name === 'TimeoutError') {
    return new ApiError(0, 'timeout', 'request timed out')
  }
  if (err instanceof DOMException && err.name === 'AbortError') {
    return new ApiError(0, 'aborted', 'request aborted')
  }
  return new ApiError(0, 'network', err instanceof Error ? err.message : 'network error')
}

/** Minimal JSON fetch wrapper. Cookies are sent automatically (same origin). */
export async function api<T>(path: string, init: RequestInit = {}, timeoutMs = DEFAULT_TIMEOUT_MS): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (init.body) headers['Content-Type'] = 'application/json'
  let res: Response
  let text: string
  try {
    res = await fetch(path, {
      credentials: 'same-origin',
      ...init,
      headers: { ...headers, ...(init.headers as Record<string, string> | undefined) },
      signal: withDeadline(init.signal, timeoutMs),
    })
    if (res.status === 204) return undefined as T
    text = await res.text() // the deadline covers the body too
  } catch (err) {
    throw asApiError(err)
  }
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
