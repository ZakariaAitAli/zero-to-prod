import { useEffect, useState } from 'react'

import {
  createWorkItem,
  getProcessingJob,
  listWorkItems,
  processWorkItem,
} from '@/api/work-items'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type {
  ProcessingJob,
  WorkItem,
} from '@/types/work-items'

const processingPollIntervalMilliseconds = 500
const processingPollAttempts = 20

function wait(milliseconds: number) {
  return new Promise((resolve) => {
    window.setTimeout(resolve, milliseconds)
  })
}

export default function App() {
  const [items, setItems] = useState<WorkItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [dialogOpen, setDialogOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [creating, setCreating] = useState(false)

  const [processingItemID, setProcessingItemID] =
    useState<number | null>(null)

  const [jobsByItemID, setJobsByItemID] =
    useState<Record<number, ProcessingJob>>({})

  async function refreshItems() {
    const result = await listWorkItems()
    setItems(result)
  }

  useEffect(() => {
    let cancelled = false

    async function loadItems() {
      try {
        const result = await listWorkItems()

        if (!cancelled) {
          setItems(result)
          setError(null)
        }
      } catch (loadError) {
        if (!cancelled) {
          setError(
            loadError instanceof Error
              ? loadError.message
              : 'unknown_error',
          )
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    void loadItems()

    return () => {
      cancelled = true
    }
  }, [])

  async function handleCreate() {
    const trimmedTitle = title.trim()

    if (!trimmedTitle) {
      return
    }

    setCreating(true)
    setError(null)

    try {
      const created = await createWorkItem({
        title: trimmedTitle,
      })

      setItems((current) => [...current, created])
      setTitle('')
      setDialogOpen(false)
    } catch (createError) {
      setError(
        createError instanceof Error
          ? createError.message
          : 'unknown_error',
      )
    } finally {
      setCreating(false)
    }
  }

  async function handleProcess(workItemID: number) {
    setProcessingItemID(workItemID)
    setError(null)

    try {
      const acceptedJob = await processWorkItem(workItemID)

      setJobsByItemID((current) => ({
        ...current,
        [workItemID]: acceptedJob,
      }))

      let currentJob = acceptedJob

      for (
        let attempt = 0;
        attempt < processingPollAttempts &&
        currentJob.state === 'accepted';
        attempt += 1
      ) {
        await wait(processingPollIntervalMilliseconds)

        const polledJob = await getProcessingJob(currentJob.id)
        currentJob = polledJob

        setJobsByItemID((current) => ({
          ...current,
          [workItemID]: polledJob,
        }))
      }

      if (currentJob.state === 'accepted') {
        throw new Error('processing_job_timeout')
      }

      await refreshItems()
    } catch (processError) {
      if (processError instanceof Error &&
          processError.message === 'work_item_already_done') {
        try {
          await refreshItems()
          return
        } catch (refreshError) {
          setError(refreshError instanceof Error ? refreshError.message : 'unknown_error')
          return
        }
      }
      setError(
        processError instanceof Error
          ? processError.message
          : 'unknown_error',
      )
    } finally {
      setProcessingItemID(null)
    }
  }

  return (
    <main className="min-h-screen bg-background text-foreground">
      <div className="mx-auto flex max-w-6xl flex-col gap-6 px-6 py-10">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-sm text-muted-foreground">
              Zero-to-Prod
            </p>

            <h1 className="text-3xl font-semibold tracking-tight">
              Work Items
            </h1>
          </div>

          <Dialog
            open={dialogOpen}
            onOpenChange={setDialogOpen}
          >
            <DialogTrigger render={<Button />}>
              New Work Item
            </DialogTrigger>

            <DialogContent>
              <DialogHeader>
                <DialogTitle>Create Work Item</DialogTitle>

                <DialogDescription>
                  Add a new Work Item to the system.
                </DialogDescription>
              </DialogHeader>

              <Input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="Work Item title"
                disabled={creating}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    void handleCreate()
                  }
                }}
              />

              <DialogFooter>
                <Button
                  variant="outline"
                  onClick={() => setDialogOpen(false)}
                  disabled={creating}
                >
                  Cancel
                </Button>

                <Button
                  onClick={() => void handleCreate()}
                  disabled={creating || !title.trim()}
                >
                  {creating ? 'Creating...' : 'Create'}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </div>

        {error && (
          <Alert variant="destructive">
            <AlertTitle>Request failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <Card>
          <CardHeader>
            <CardTitle>Current Work Items</CardTitle>
          </CardHeader>

          <CardContent>
            {loading ? (
              <div className="space-y-3">
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-10 w-full" />
              </div>
            ) : items.length === 0 ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                No Work Items yet.
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ID</TableHead>
                    <TableHead>Title</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Job</TableHead>
                    <TableHead>Result</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="text-right">
                      Actions
                    </TableHead>
                  </TableRow>
                </TableHeader>

                <TableBody>
                  {items.map((item) => {
                    const job = jobsByItemID[item.id]
                    const processing =
                      processingItemID === item.id

                    return (
                      <TableRow key={item.id}>
                        <TableCell>#{item.id}</TableCell>

                        <TableCell className="font-medium">
                          {item.title}
                        </TableCell>

                        <TableCell>
                          <Badge variant="secondary">
                            {item.status}
                          </Badge>
                        </TableCell>

                        <TableCell>
                          {job ? (
                            <Badge
                              variant={
                                job.state === 'failed'
                                  ? 'destructive'
                                  : 'secondary'
                              }
                            >
                              {job.state}
                            </Badge>
                          ) : (
                            <span className="text-muted-foreground">
                              —
                            </span>
                          )}
                        </TableCell>

                        <TableCell>
                          {item.result
                            ? `${item.result.character_count} characters, ${item.result.word_count} words`
                            : '—'}
                        </TableCell>
                        <TableCell className="text-muted-foreground">
                          {new Date(
                            item.created_at,
                          ).toLocaleString()}
                        </TableCell>

                        <TableCell className="text-right">
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={processingItemID !== null || item.status === 'done'}
                            onClick={() => {
                              void handleProcess(item.id)
                            }}
                          >
                            {processing
                              ? 'Processing...'
                              : 'Process'}
                          </Button>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </main>
  )
}
