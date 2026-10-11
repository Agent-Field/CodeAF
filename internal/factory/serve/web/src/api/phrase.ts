import type { Job } from "./types"

// The state phrase of a queue row (DESIGN-BRIEF §4.1), from what the wire
// carries today. Phrases that need the lease desk (whose worker, offline)
// fall back to the plain one until that data exists.
export function phrase(job: Job): string {
  const step = job.step?.name ?? ""
  switch (job.state) {
    case "needs_you":
      if (job.gate === "plan" || job.gate === "proof") return `approve ${job.gate}`
      if (job.gate === "cap") return "over its cap"
      return step ? `${step} · asks` : "asks"
    case "running":
      return step || "running"
    case "queued":
      return job.assigned_to ? `waiting for @${job.assigned_to}'s machine` : "waiting for someone to take it"
    case "backlog":
      return "not started"
    case "done":
      return job.pr ? "merged" : "done"
    case "dropped":
      return "dropped"
  }
}

export function jobLabel(job: Job): string {
  const short = job.repo.split("/").pop() ?? job.repo
  return `${short}#${job.number}`
}

export function money(n: number): string {
  return `$${n.toFixed(2)}`
}

export function age(iso: string, now = Date.now()): string {
  const mins = Math.max(0, Math.round((now - Date.parse(iso)) / 60000))
  if (mins < 60) return `${mins}m ago`
  if (mins < 60 * 24) return `${Math.round(mins / 60)}h ago`
  return `${Math.round(mins / 1440)}d ago`
}
