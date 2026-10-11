import { railSteps } from "@/api/brief"
import type { Job, StepState } from "@/api/types"
import { cn } from "@/lib/utils"

const marks: Record<StepState, string> = {
  done: "✓",
  waiting: "⚑",
  running: "●",
  failed: "✕",
  skipped: "–",
  pending: "○",
}

// The job's pipeline in order. A backlog job has not started, so every mark
// is muted. Loop text is the step's own `until`, never a name-based guess.
export function StepRail({ job }: { job: Job }) {
  const steps = railSteps(job)
  if (steps.length === 0) return null
  return (
    <section data-testid="step-rail" className="flex flex-col gap-1">
      <ol className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
        {steps.map((s) => (
          <li
            key={s.name}
            data-testid={`rail-${s.name}`}
            className={cn("flex items-center gap-1", (job.state === "backlog" || s.state === "pending") && "text-muted-foreground")}
          >
            <span aria-label={s.state}>{marks[s.state]}</span>
            <span className={cn(s.state === "running" && "font-medium")}>{s.name}</span>
            {s.fixed && <span className="text-xs text-muted-foreground">(fixed)</span>}
            {s.custom && <span className="text-xs text-muted-foreground">· custom</span>}
            {(s.max ?? 0) > 1 && (
              <span className="text-xs text-muted-foreground">
                ↻ {s.round ?? 0}/{s.max}
                {s.until && ` until ${s.until}`}
              </span>
            )}
          </li>
        ))}
      </ol>
      <p className="text-xs text-muted-foreground">fixed: a job can never drop approve or proof</p>
    </section>
  )
}
