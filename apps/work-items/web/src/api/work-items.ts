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
export const signOutURL = '/oauth2/sign_out'

export type Session =
  | { kind: 'signed-in'; email: string }
  | { kind: 'signed-out' }
  | { kind: 'no-gateway' }

// The gateway answers JSON for a session and 401 without one. Anything else,
// such as the development server's HTML fallback, means there is no gateway.
export async function getSession(): Promise<Session> {
  try {
    const response = await fetch('/oauth2/userinfo', {
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })

    if (response.status === 401) {
      return { kind: 'signed-out' }
    }

    if (response.ok &&
        response.headers.get('Content-Type')?.includes('application/json')) {
      const body = (await response.json()) as { email?: string; user?: string }
      return { kind: 'signed-in', email: body.email ?? body.user ?? '' }
    }
  } catch {
    // Treated as no gateway.
  }

  return { kind: 'no-gateway' }
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
