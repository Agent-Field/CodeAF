import { useMemo, useState } from "react"
import { useNavigate } from "react-router"

import { api, FactoryError } from "@/api/client"
import { useQueue } from "@/api/queue"
import { actFor, groups, inView, ordered, views, type View } from "@/api/views"
import type { Job } from "@/api/types"
import { AnswerDialog, KeysDialog } from "@/components/queue-dialogs"
import { JobRow, type RowActions } from "@/components/job-row"
import { QueueHeader } from "@/components/queue-header"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useQueueKeys } from "@/hooks/use-queue-keys"

// A queue view: the header over the jobs of the view, filtered by repo chip,
// grouped by state in the brief's order, empty groups hidden.
export function QueuePage({ view }: { view: View }) {
  const { queue, error, reload } = useQueue()
  const navigate = useNavigate()
  const { title, subtitle } = views[view]
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [cursor, setCursor] = useState(0)
  const [help, setHelp] = useState(false)
  const [answering, setAnswering] = useState<Job | null>(null)
  const [failure, setFailure] = useState<string | null>(null)

  const jobs = useMemo(
    () => (queue ? queue.jobs.filter((j) => inView(view, j, queue.me) && !hidden.has(j.repo)) : []),
    [queue, view, hidden],
  )
  const flat = useMemo(() => ordered(jobs), [jobs])
  const repos = useMemo(() => {
    if (!queue) return []
    const names = new Set([...queue.repos.filter((r) => r.watched).map((r) => r.name), ...queue.jobs.map((j) => j.repo)])
    return [...names].sort()
  }, [queue])
  // The Taken by column is for a team: it shows once anyone holds a job.
  const team = queue?.jobs.some((j) => j.assigned_to) ?? false
  const at = flat[Math.min(cursor, flat.length - 1)]

  // A verb's failure is a sentence from the factory; success re-reads the floor.
  const attempt = (call: Promise<unknown>) =>
    call.then(
      () => {
        setFailure(null)
        reload()
      },
      (e: unknown) => setFailure(e instanceof FactoryError ? e.message : String(e)),
    )
  const actions: RowActions = {
    approve: (job) => void attempt(api.approve(job.id)),
    answer: setAnswering,
    take: (job) => void attempt(api.take(job.id)),
  }
  const toggle = (repo: string) =>
    setHidden((h) => {
      const next = new Set(h)
      if (!next.delete(repo)) next.add(repo)
      return next
    })

  useQueueKeys(
    {
      j: () => setCursor((c) => Math.min(c + 1, flat.length - 1)),
      k: () => setCursor((c) => Math.max(c - 1, 0)),
      enter: () => at && navigate(`/jobs/${at.id}`),
      y: () => at && actFor(at) === "approve" && actions.approve(at),
      a: () => at && actFor(at) === "answer" && actions.answer(at),
      help: () => setHelp(true),
    },
    !help && answering === null,
  )

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      <div>
        <h1 className="text-xl font-semibold">{title}</h1>
        <p className="text-sm text-muted-foreground">{subtitle}</p>
      </div>
      {(error || failure) && (
        <Alert variant="destructive" data-testid="queue-error">
          <AlertTitle>{error ? "Could not read the queue" : "That did not go through"}</AlertTitle>
          <AlertDescription>{error ?? failure}</AlertDescription>
        </Alert>
      )}
      {queue && <QueueHeader queue={queue} repos={repos} hidden={hidden} toggle={toggle} />}
      {queue && jobs.length === 0 && <p className="text-sm text-muted-foreground">No jobs here.</p>}
      {groups.map((g) => {
        const rows = jobs.filter((j) => j.state === g.state)
        return (
          rows.length > 0 && (
            <Group key={g.state} title={g.title} jobs={rows} active={at?.id} team={team} actions={actions} />
          )
        )
      })}
      {queue && (
        <p className="text-xs text-muted-foreground" data-testid="key-hint">
          j/k move · enter open · y approve · a answer · ? all keys
        </p>
      )}
      <KeysDialog open={help} onOpenChange={setHelp} />
      <AnswerDialog
        job={answering}
        onClose={() => setAnswering(null)}
        send={(job, answer) => {
          setAnswering(null)
          void attempt(api.answer(job.id, answer))
        }}
      />
    </div>
  )
}

function Group({ title, jobs, active, team, actions }: { title: string; jobs: Job[]; active?: number; team: boolean; actions: RowActions }) {
  return (
    <section data-testid={`group-${title}`}>
      <h2 className="mb-1 flex items-center gap-2 text-sm font-medium">
        {title} <Badge variant="secondary">{jobs.length}</Badge>
      </h2>
      <Table className="md:table-fixed">
        <TableHeader>
          <TableRow>
            <TableHead className="hidden w-16 md:table-cell">Kind</TableHead>
            <TableHead className="hidden w-24 md:table-cell">Job</TableHead>
            <TableHead>
              <span className="md:hidden">Job</span>
              <span className="hidden md:inline">Title</span>
            </TableHead>
            <TableHead className="md:w-40">State</TableHead>
            <TableHead className="hidden w-24 md:table-cell">Step</TableHead>
            <TableHead className="w-16 text-right">Cost</TableHead>
            <TableHead className="hidden w-20 text-right md:table-cell">Age</TableHead>
            {team && <TableHead className="hidden w-24 md:table-cell">Taken by</TableHead>}
            <TableHead className="w-12 md:w-32" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {jobs.map((j) => (
            <JobRow key={j.id} job={j} active={j.id === active} team={team} actions={actions} />
          ))}
        </TableBody>
      </Table>
    </section>
  )
}
