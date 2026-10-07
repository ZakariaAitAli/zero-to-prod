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

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, init)

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
