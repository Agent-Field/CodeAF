import { Check, HandHelping, MessageCircleQuestion } from "lucide-react"
import { useNavigate } from "react-router"

import { age, jobLabel, money, phrase } from "@/api/phrase"
import { actFor, type Act } from "@/api/views"
import type { Job } from "@/api/types"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { TableCell, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"

export interface RowActions {
  approve: (job: Job) => void
  answer: (job: Job) => void
  take: (job: Job) => void
}

function finishedAt(job: Job): string {
  const at = new Date(job.desk?.changed_at ?? job.created_at)
  return isNaN(at.getTime()) ? "" : at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

function Progress({ job }: { job: Job }) {
  if (!job.step) return null
  const { index, count } = job.step
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-12 overflow-hidden rounded-full bg-muted" role="presentation">
        <div className="h-full bg-primary" style={{ width: `${Math.min(100, (index / Math.max(1, count)) * 100)}%` }} />
      </div>
      <span className="text-xs tabular-nums">
        {index}/{count}
      </span>
    </div>
  )
}

function ActionButton({ job, act, actions }: { job: Job; act: Act; actions: RowActions }) {
  const stop = (fn: () => void) => (e: React.MouseEvent) => {
    e.stopPropagation()
    fn()
  }
  if (act === "approve")
    return (
      <Button size="sm" aria-label="Approve" data-testid={`approve-${job.id}`} onClick={stop(() => actions.approve(job))}>
        <Check /> <span className="hidden md:inline">Approve</span>
      </Button>
    )
  if (act === "answer")
    return (
      <Button size="sm" aria-label="Answer" data-testid={`answer-${job.id}`} onClick={stop(() => actions.answer(job))}>
        <MessageCircleQuestion /> <span className="hidden md:inline">Answer</span>
      </Button>
    )
  return (
    <Button size="sm" variant="outline" aria-label="Take" data-testid={`take-${job.id}`} onClick={stop(() => actions.take(job))}>
      <HandHelping /> <span className="hidden md:inline">Take</span>
    </Button>
  )
}

// One job. Clicking the row opens the job; the action button keeps its own
// click. On a phone job, title, state, cost and action carry the row.
export function JobRow({ job, active, team, actions }: { job: Job; active: boolean; team: boolean; actions: RowActions }) {
  const navigate = useNavigate()
  const act = actFor(job)
  return (
    <TableRow
      data-testid={`job-${job.id}`}
      data-active={active || undefined}
      className={cn("cursor-pointer", active && "bg-muted")}
      onClick={() => navigate(`/jobs/${job.id}`)}
    >
      <TableCell className="hidden md:table-cell">
        <Badge variant="outline" className="uppercase">
          {job.kind}
        </Badge>
      </TableCell>
      <TableCell className="hidden font-mono text-xs md:table-cell">{jobLabel(job)}</TableCell>
      <TableCell className="whitespace-normal md:truncate">
        <span className="block font-mono text-xs text-muted-foreground md:hidden">{jobLabel(job)}</span>
        {job.title}
      </TableCell>
      <TableCell className="whitespace-normal text-muted-foreground">{phrase(job)}</TableCell>
      <TableCell className="hidden md:table-cell">
        <Progress job={job} />
      </TableCell>
      <TableCell className="text-right tabular-nums">{money(job.cost)}</TableCell>
      <TableCell className="hidden text-right text-muted-foreground md:table-cell">
        {job.state === "done" ? finishedAt(job) : age(job.created_at)}
      </TableCell>
      {team && (
        <TableCell className="hidden md:table-cell">
          {job.assigned_to && (
            <span className="flex items-center gap-1.5 text-xs" data-testid={`assignee-${job.id}`}>
              <Avatar className="size-5">
                <AvatarFallback className="text-[10px]">{job.assigned_to.slice(0, 1).toUpperCase()}</AvatarFallback>
              </Avatar>
              @{job.assigned_to}
            </span>
          )}
        </TableCell>
      )}
      <TableCell className="text-right">{act && <ActionButton job={job} act={act} actions={actions} />}</TableCell>
    </TableRow>
  )
}
