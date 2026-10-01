import type {
  ApiErrorResponse,
  ProcessingJob,
  WorkItem,
  WorkItemStatus,
} from '@/types/work-items'

const apiBase = '/api'

async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, init)

  if (!response.ok) {
    let code = `http_${response.status}`

    try {
      const body = (await response.json()) as ApiErrorResponse

      if (body.error) {
        code = body.error
      }
    } catch {
      // Preserve the HTTP-derived fallback code.
    }

    throw new Error(code)
  }

  return response.json() as Promise<T>
}

export function listWorkItems(): Promise<WorkItem[]> {
  return request<WorkItem[]>('/items')
}

export function createWorkItem(input: {
  title: string
  status?: WorkItemStatus
}): Promise<WorkItem> {
  return request<WorkItem>('/items', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  })
}

export function processWorkItem(
  workItemID: number,
): Promise<ProcessingJob> {
  return request<ProcessingJob>(
    `/items/${workItemID}/process`,
    {
      method: 'POST',
    },
  )
}

export function getProcessingJob(
  jobID: number,
): Promise<ProcessingJob> {
  return request<ProcessingJob>(
    `/processing-jobs/${jobID}`,
  )
}
