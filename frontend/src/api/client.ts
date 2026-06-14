import { authMode } from '../auth/config'
import { currentIDToken } from '../auth/firebase'
import { authSession } from '../auth/session'

export type APIRequestOptions = Omit<RequestInit, 'headers'> & {
  headers?: HeadersInit
  authenticated?: boolean
}

export async function apiRequest(
  path: string,
  options: APIRequestOptions = {},
): Promise<Response> {
  const { authenticated = false, headers: initialHeaders, ...requestOptions } = options
  const headers = new Headers(initialHeaders)

  if (authenticated) {
    if (authMode === 'firebase') {
      const token = await currentIDToken()
      if (!token) {
        throw new Error('Authentication is required')
      }
      headers.set('Authorization', `Bearer ${token}`)
    } else {
      if (!authSession.state.authenticated || !authSession.state.userId) {
        throw new Error('Authentication is required')
      }
      headers.set('X-User-ID', authSession.state.userId)
      headers.set('X-User-Role', authSession.state.role)
    }
  }

  return fetch(`/api${path.startsWith('/') ? path : `/${path}`}`, {
    ...requestOptions,
    headers,
  })
}

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

export async function parseResponse<T>(response: Response): Promise<T> {
  if (response.ok) {
    if (response.status === 204) {
      return undefined as T
    }
    return (await response.json()) as T
  }
  let code = 'REQUEST_FAILED'
  let message = 'The request could not be completed.'
  try {
    const body = (await response.json()) as { error?: { code?: string; message?: string } }
    code = body.error?.code || code
    message = body.error?.message || message
  } catch {
    // Keep the safe fallback message.
  }
  throw new APIError(response.status, code, message)
}
