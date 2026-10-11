import { money } from "@/api/phrase"
import type { Queue } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"

// Since line, pipeline strip, repo chips and capacity line (DESIGN-BRIEF §4.1).
export function QueueHeader({
  queue,
  repos,
  hidden,
  toggle,
}: {
  queue: Queue
  repos: string[]
  hidden: Set<string>
  toggle: (repo: string) => void
}) {
  const { since, strip, capacity } = queue
  const at = new Date(since.at)
  const time = isNaN(at.getTime()) ? "—" : at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
  return (
    <div className="flex flex-col gap-2 text-sm">
      <p className="text-muted-foreground" data-testid="since-line">
        since {time} · {since.shipped} merged · {since.asked} need you · {money(since.spent)}
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
      <div className="flex flex-wrap gap-1.5" data-testid="repo-chips">
        {repos.map((r) => {
          const on = !hidden.has(r)
          return (
            <button
              key={r}
              type="button"
              aria-pressed={on}
              data-testid={`repo-${r}`}
              onClick={() => toggle(r)}
              className={cn(
                "rounded-md border px-2 py-0.5 text-xs",
                on ? "bg-secondary text-secondary-foreground" : "text-muted-foreground line-through",
              )}
            >
              {on ? "✓ " : ""}
              {r}
            </button>
          )
        })}
      </div>
      <p className="text-muted-foreground" data-testid="capacity-line">
        {capacity.queued} queued · {capacity.benches} {capacity.benches === 1 ? "bench" : "benches"} ·{" "}
        {money(capacity.daily)} of {money(capacity.rail)} today
      </p>
    </div>
  )
}
