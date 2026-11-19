import type {
  ApiErrorResponse,
  ProcessingJob,
  WorkItem,
} from '@/types/work-items'

export class ApiError extends Error {
  processingJobID?: number

  constructor(message: string, processingJobID?: number) {
    super(message)
    this.processingJobID = processingJobID
  }
}

const apiBase = '/api'

// The security lab's ingress requires this header on state-changing requests
// that carry the session cookie; a cross-origin page cannot add it without a
// CORS preflight. The development API ignores it.
async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const headers = new Headers(init?.headers)
  headers.set('X-Requested-With', 'work-items-web')

  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    credentials: 'same-origin',
    headers,
  })

  if (!response.ok) {
    let code = `http_${response.status}`
    let processingJobID: number | undefined

    try {
      const body = (await response.json()) as ApiErrorResponse

      if (body.error) {
        code = body.error
        processingJobID = body.processing_job_id
      }
    } catch {
      // Preserve the HTTP-derived fallback code.
    }

    throw new ApiError(code, processingJobID)
  }

  return response.json() as Promise<T>
}

// Sign-in and sign-out go through the gateway, which exists only in the
// security lab.
export const signInURL = '/oauth2/start?rd=%2F'
const signOutURL = '/oauth2/sign_out'

// Sign out with a same-origin fetch, not a form post: the site sends
// Referrer-Policy: no-referrer, under which browsers send a form post's
// Origin as "null", which the ingress rejects. A fetch carries the real
// Origin and the CSRF header. The gateway answers with a redirect to "/"
// that clears the session cookie; the fetch follows it.
export async function signOut(): Promise<void> {
  const response = await fetch(signOutURL, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'X-Requested-With': 'work-items-web' },
  })

  if (!response.ok) {
    let code = `http_${response.status}`
    try {
      const body = (await response.json()) as ApiErrorResponse
      code = body.error || code
    } catch {
      // Keep the HTTP-derived code.
    }
    throw new ApiError(code)
  }
}

export type Session =
  | { kind: 'signed-in'; email: string }
  | { kind: 'signed-out' }
  | { kind: 'no-gateway' }
  | { kind: 'unavailable' }

// Only a successful HTML fallback identifies development without a gateway.
// Network errors, gateway errors and malformed identities are outages.
export async function getSession(): Promise<Session> {
  try {
    const response = await fetch('/oauth2/userinfo', {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })

    if (response.status === 401) {
      return { kind: 'signed-out' }
    }

    if (!response.ok) return { kind: 'unavailable' }

    if (response.ok &&
        response.headers.get('Content-Type')?.includes('application/json')) {
      const body: unknown = await response.json()
      if (body && typeof body === 'object' && !Array.isArray(body)) {
        const identity = body as { email?: unknown; user?: unknown }
        const email = typeof identity.email === 'string' && identity.email.trim()
          ? identity.email : identity.user
        if (typeof email === 'string' && email.trim()) {
          return { kind: 'signed-in', email }
        }
      }
      return { kind: 'unavailable' }
    }
    if (response.headers.get('Content-Type')?.includes('text/html')) {
      return { kind: 'no-gateway' }
    }
  } catch {
    return { kind: 'unavailable' }
  }

  return { kind: 'unavailable' }
}

export function listWorkItems(): Promise<WorkItem[]> {
  return request<WorkItem[]>('/items')
}

export function createWorkItem(input: {
  title: string
}): Promise<WorkItem> {
  return request<WorkItem>('/items', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  })
}

export async function processWorkItem(
  workItemID: number,
): Promise<ProcessingJob> {
  try {
    return await request<ProcessingJob>(`/items/${workItemID}/process`, {
      method: 'POST',
    })
  } catch (error) {
    if (error instanceof ApiError &&
        error.message === 'processing_already_active' &&
        error.processingJobID !== undefined) {
      return getProcessingJob(error.processingJobID)
    }
    throw error
  }
}

export function getProcessingJob(
  jobID: number,
): Promise<ProcessingJob> {
  return request<ProcessingJob>(
    `/processing-jobs/${jobID}`,
  )
}
