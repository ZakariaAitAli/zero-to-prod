export type WorkItemStatus = 'pending' | 'done'

export type WorkItem = {
  id: number
  title: string
  status: WorkItemStatus
  created_at: string
}

export type ProcessingJobState =
  | 'accepted'
  | 'succeeded'
  | 'failed'

export type ProcessingJob = {
  id: number
  work_item_id: number
  state: ProcessingJobState
  attempt_count: number
  created_at: string
}

export type ApiErrorResponse = {
  error: string
}
