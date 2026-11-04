export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

let onUnauthorized: () => void = () => {}
export const setUnauthorizedHandler = (fn: () => void) => (onUnauthorized = fn)

export async function api<T = any>(method: string, path: string, body?: unknown, headers?: Record<string, string>): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: { ...headers } }
  const h = init.headers as Record<string, string>
  if (method !== 'GET') {
    h['X-Requested-With'] = 'genkan'
    if (body instanceof FormData) init.body = body
    else {
      h['Content-Type'] = 'application/json'
      if (body !== undefined) init.body = JSON.stringify(body)
    }
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch {
    throw new ApiError(0, 'network')
  }
  if (res.status === 204) return undefined as T
  let data: any = null
  const text = await res.text()
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  if (!res.ok) {
    // 401 from /api/auth/* (wrong password) is a form error, not an expired session
    if (res.status === 401 && !path.startsWith('/api/auth/')) onUnauthorized()
    throw new ApiError(res.status, (data && data.error) || res.statusText || 'error')
  }
  return data as T
}

export const get = <T = any>(p: string, headers?: Record<string, string>) => api<T>('GET', p, undefined, headers)
export const post = <T = any>(p: string, b?: unknown) => api<T>('POST', p, b ?? {})
export const patch = <T = any>(p: string, b: unknown) => api<T>('PATCH', p, b)
export const put = <T = any>(p: string, b: unknown) => api<T>('PUT', p, b)
export const del = <T = any>(p: string) => api<T>('DELETE', p)
