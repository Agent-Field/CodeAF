import { Link } from "react-router"

import { phrase, jobLabel, money, age } from "@/api/phrase"
import { useQueue } from "@/api/queue"
import { groups, inView, views, type View } from "@/api/views"
import type { Job, Queue } from "@/api/types"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

// A queue view: the since line and pipeline strip over the jobs of the view,
// grouped by state in the brief's order, empty groups hidden.
export function QueuePage({ view }: { view: View }) {
  const { queue, error } = useQueue()
  const { title, subtitle } = views[view]
  const jobs = queue?.jobs.filter((j) => inView(view, j, queue.me)) ?? []

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      <div>
        <h1 className="text-xl font-semibold">{title}</h1>
        <p className="text-sm text-muted-foreground">{subtitle}</p>
      </div>
      {error && (
        <Alert variant="destructive">
          <AlertTitle>Could not read the queue</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {queue && <Overview queue={queue} />}
      {queue && jobs.length === 0 && <p className="text-sm text-muted-foreground">No jobs here.</p>}
      {groups.map((g) => {
        const rows = jobs.filter((j) => j.state === g.state)
        return rows.length > 0 && <Group key={g.state} title={g.title} jobs={rows} />
      })}
    </div>
  )
}

function Overview({ queue }: { queue: Queue }) {
  const { since, strip } = queue
  const at = new Date(since.at)
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p className="text-muted-foreground" data-testid="since-line">
        since {isNaN(at.getTime()) ? "—" : at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} ·{" "}
        {since.shipped} merged · {since.asked} need you · {money(since.spent)}
      </p>
      <div className="flex flex-wrap gap-1.5" data-testid="pipeline-strip">
        {strip.length === 0 ? (
          <span className="text-muted-foreground">No jobs in a step.</span>
        ) : (
          strip.map((c) => (
            <Badge key={c.step} variant="outline">
              {c.step} {c.jobs}
              {c.waiting ? " ⚑" : ""}
            </Badge>
          ))
        )}
      </div>
    </div>
  )
}

// On a phone the job, title, state and cost carry a row; kind, step and age
// wait for a wider screen.
function Group({ title, jobs }: { title: string; jobs: Job[] }) {
  return (
    <section data-testid={`group-${title}`}>
      <h2 className="mb-1 flex items-center gap-2 text-sm font-medium">
        {title} <Badge variant="secondary">{jobs.length}</Badge>
      </h2>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="hidden w-16 md:table-cell">Kind</TableHead>
            <TableHead className="w-28">Job</TableHead>
            <TableHead>Title</TableHead>
            <TableHead>State</TableHead>
            <TableHead className="hidden w-20 md:table-cell">Step</TableHead>
            <TableHead className="w-20 text-right">Cost</TableHead>
            <TableHead className="hidden w-24 text-right md:table-cell">Age</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {jobs.map((j) => (
            <TableRow key={j.id} data-testid={`job-${j.id}`}>
              <TableCell className="hidden text-xs uppercase text-muted-foreground md:table-cell">{j.kind}</TableCell>
              <TableCell className="font-mono text-xs">{jobLabel(j)}</TableCell>
              <TableCell className="max-w-48 truncate md:max-w-md">
                <Link to={`/jobs/${j.id}`} className="hover:underline">
                  {j.title}
                </Link>
              </TableCell>
              <TableCell className="text-muted-foreground">{phrase(j)}</TableCell>
              <TableCell className="hidden text-xs md:table-cell">{j.step ? `${j.step.index}/${j.step.count}` : ""}</TableCell>
              <TableCell className="text-right tabular-nums">{money(j.cost)}</TableCell>
              <TableCell className="hidden text-right text-muted-foreground md:table-cell">{age(j.created_at)}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  )
}
