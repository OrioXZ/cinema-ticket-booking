import { authMode, developmentIdentity } from '../auth/config'
import { currentIDToken } from '../auth/firebase'

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
      if (!developmentIdentity.userId) {
        throw new Error('VITE_DEV_USER_ID is required for authenticated development requests')
      }
      headers.set('X-User-ID', developmentIdentity.userId)
      headers.set('X-User-Role', developmentIdentity.role)
    }
  }

  return fetch(`/api${path.startsWith('/') ? path : `/${path}`}`, {
    ...requestOptions,
    headers,
  })
}
