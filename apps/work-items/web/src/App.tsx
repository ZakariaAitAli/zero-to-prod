import { useEffect, useState } from 'react'

import {
  createWorkItem,
  getProcessingJob,
  getSession,
  listWorkItems,
  processWorkItem,
  signInURL,
  signOut,
  type Session,
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

// User-facing text for a failed create, shown inside the dialog.
function describeCreateError(code: string): { title: string; description: string } {
  switch (code) {
    case 'insufficient_permissions':
      return {
        title: 'Not allowed',
        description: 'Your account is not allowed to create Work Items. The title you entered is kept.',
      }
    case 'unauthenticated':
      return {
        title: 'Sign in required',
        description: 'Your session has ended.',
      }
    default:
      return {
        title: 'Could not create the Work Item',
        description: `The request failed (${code}). The title you entered is kept; try again.`,
      }
  }
}

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
  const [createError, setCreateError] = useState<string | null>(null)
  const [title, setTitle] = useState('')
  const [creating, setCreating] = useState(false)

  const [processingItemID, setProcessingItemID] =
    useState<number | null>(null)

  const [jobsByItemID, setJobsByItemID] =
    useState<Record<number, ProcessingJob>>({})

  const [session, setSession] = useState<Session>({ kind: 'no-gateway' })
  const [signingOut, setSigningOut] = useState(false)

  async function handleSignOut() {
    setSigningOut(true)
    setError(null)

    try {
      await signOut()
      // Reload so the page reflects the ended session.
      window.location.assign('/')
    } catch (signOutError) {
      setError(
        signOutError instanceof Error
          ? `sign_out_failed: ${signOutError.message}`
          : 'sign_out_failed',
      )
      setSigningOut(false)
    }
  }

  useEffect(() => {
    let cancelled = false

    void getSession().then((result) => {
      if (!cancelled) {
        setSession(result)
      }
    })

    return () => {
      cancelled = true
    }
  }, [])

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

  // Opening or closing the dialog starts from a clean state; the entered
  // title is kept so a failed attempt can be retried.
  function handleDialogOpenChange(open: boolean) {
    setDialogOpen(open)
    setCreateError(null)
  }

  async function handleCreate() {
    const trimmedTitle = title.trim()

    if (!trimmedTitle) {
      return
    }

    setCreating(true)
    // A new attempt replaces any earlier failure.
    setCreateError(null)

    try {
      const created = await createWorkItem({
        title: trimmedTitle,
      })

      setItems((current) => [...current, created])
      setTitle('')
      handleDialogOpenChange(false)
    } catch (failure) {
      // Shown inside the open dialog: an error on the page behind the
      // modal overlay would be hidden by it.
      setCreateError(
        failure instanceof Error
          ? failure.message
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

          <div className="flex items-center gap-3">
            {session.kind === 'signed-in' && (
              <div className="flex items-center gap-3">
                <span className="text-sm text-muted-foreground">
                  {session.email}
                </span>
                <Button
                  variant="outline"
                  disabled={signingOut}
                  onClick={() => void handleSignOut()}
                >
                  {signingOut ? 'Signing out...' : 'Sign out'}
                </Button>
              </div>
            )}

            <Dialog
              open={dialogOpen}
              onOpenChange={handleDialogOpenChange}
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
                  onChange={(event) => {
                    setTitle(event.target.value)
                    // An edited title is a new attempt; the old failure is stale.
                    setCreateError(null)
                  }}
                  placeholder="Work Item title"
                  aria-label="Work Item title"
                  aria-describedby={createError ? 'create-work-item-error' : undefined}
                  disabled={creating}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') {
                      void handleCreate()
                    }
                  }}
                />

                {createError && (
                  // role="alert" (from Alert) announces the failure.
                  <Alert variant="destructive" id="create-work-item-error">
                    <AlertTitle>{describeCreateError(createError).title}</AlertTitle>
                    <AlertDescription>
                      {describeCreateError(createError).description}
                      {createError === 'unauthenticated' && (
                        <>
                          {' '}
                          <a className="underline" href={signInURL}>
                            Sign in
                          </a>
                        </>
                      )}
                    </AlertDescription>
                  </Alert>
                )}

                <DialogFooter>
                  <Button
                    variant="outline"
                    onClick={() => handleDialogOpenChange(false)}
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
        </div>

        {error === 'unauthenticated' ? (
          <Alert>
            <AlertTitle>Sign in required</AlertTitle>
            <AlertDescription>
              <a className="underline" href={signInURL}>
                Sign in
              </a>{' '}
              to view and change Work Items.
            </AlertDescription>
          </Alert>
        ) : error === 'insufficient_permissions' ? (
          // 403 from the API: signed in, but the account lacks the
          // permission for this action. The session stays valid.
          <Alert variant="destructive">
            <AlertTitle>Not allowed</AlertTitle>
            <AlertDescription>
              Your account is not allowed to do this. Ask for the required
              permission or sign in with another account.
            </AlertDescription>
          </Alert>
        ) : error && (
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
