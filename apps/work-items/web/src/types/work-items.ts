export type WorkItemStatus = 'pending' | 'done'

export type WorkItem = {
  result?: {
    processing_job_id: number
    input_title: string
    analysis_version: number
    character_count: number
    word_count: number
  }
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
  processing_job_id?: number
  error: string
}
