export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly requestId?: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

function safeErrorMessage(value: string): string {
  return value
    .replace(/(bearer\s+)[\w.~-]+/gi, '$1[redacted]')
    .replace(/(api[_-]?key["'\s:=]+)[^\s,"'}]+/gi, '$1[redacted]')
    .slice(0, 400)
}

export class ApiClient {
  constructor(private readonly baseUrl = import.meta.env.VITE_API_BASE_URL ?? '') {}

  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      credentials: init.credentials ?? 'same-origin',
      headers: {
        Accept: 'application/json',
        ...(init.body ? { 'Content-Type': 'application/json' } : {}),
        ...init.headers,
      },
    })
    if (!response.ok) {
      const body = await response.text()
      throw new ApiError(
        safeErrorMessage(body || response.statusText),
        response.status,
        response.headers.get('x-request-id') ?? undefined,
      )
    }
    if (response.status === 204 || response.headers.get('content-length') === '0')
      return undefined as T
    const body = await response.text()
    return body ? (JSON.parse(body) as T) : (undefined as T)
  }
}

export function namespacePath(namespace: string, suffix: string): string {
  return `/api/v1/${encodeURIComponent(namespace)}/${suffix.replace(/^\//, '')}`
}
