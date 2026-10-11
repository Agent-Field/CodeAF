import { ExternalLink } from "lucide-react"

import { age, jobLabel, money, phrase } from "@/api/phrase"
import { ciMark, clock } from "@/api/brief"
import type { Job } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

const stateWords: Record<Job["state"], string> = {
  backlog: "Backlog",
  queued: "Queued",
  running: "Running",
  needs_you: "Needs you",
  done: "Done",
  dropped: "Dropped",
}

// Kind, state, step, cost and age as badges, then the facts the wire has; a
// fact it leaves empty draws nothing (DESIGN-BRIEF §4.2).
export function JobHeader({ job }: { job: Job }) {
  const finished = job.state === "done" || job.state === "dropped"
  const when = finished ? clock(job.desk?.changed_at) : ""
  return (
    <header className="flex flex-col gap-2" data-testid="job-header">
      <p className="font-mono text-sm text-muted-foreground">
        {job.repo} #{job.number} <span className="sr-only">{jobLabel(job)}</span>
      </p>
      <h1 className="text-xl font-semibold">{job.title}</h1>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="outline" className="uppercase" data-testid="badge-kind">
          {job.kind}
        </Badge>
        <Badge variant={job.state === "needs_you" ? "default" : "secondary"} data-testid="badge-state">
          {stateWords[job.state]}
        </Badge>
        <Badge variant="outline" data-testid="badge-phrase">
          {phrase(job)}
        </Badge>
        {job.step && (
          <Badge variant="outline" data-testid="badge-step">
            step {job.step.index}/{job.step.count}
          </Badge>
        )}
        <Badge variant="outline" data-testid="badge-cost">
          {money(job.cost)}
        </Badge>
        <Badge variant="outline" data-testid="badge-age">
          {when ? `${job.state === "done" ? "merged" : "dropped"} ${when}` : age(job.created_at)}
        </Badge>
      </div>
      <div className="flex flex-col gap-1 text-sm text-muted-foreground">
        {job.waiting_since && (
          <p className="font-medium text-foreground" data-testid="waiting-since">
            waiting on you since {clock(job.waiting_since)} ({age(job.waiting_since).replace(" ago", "")})
          </p>
        )}
        {job.asked_first_reason && <p data-testid="asked-first">asked first: {job.asked_first_reason}</p>}
        {job.assigned_to && <p data-testid="assigned-to">assigned to @{job.assigned_to}</p>}
        {job.pr && (
          <p data-testid="pr-line">
            <a className="underline" href={job.pr.url} target="_blank" rel="noopener noreferrer">
              PR #{job.pr.number}
            </a>
            {job.pr.ci && <> · ci {ciMark(job.pr.ci)}</>}
            {job.assigned_to && <> · opened as @{job.assigned_to}</>}
          </p>
        )}
      </div>
      {job.url && (
        <div>
          <Button asChild variant="outline" size="sm">
            <a href={job.url} target="_blank" rel="noopener noreferrer" data-testid="view-on-github">
              View on GitHub <ExternalLink />
            </a>
          </Button>
        </div>
      )}
    </header>
  )
}
