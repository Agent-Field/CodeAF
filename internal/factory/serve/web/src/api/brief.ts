import type { Claim, Job, Step } from "./types"

// What the job brief says, computed from the wire and nothing else. A step is
// never special-cased by name except `approve`, the one reserved word.

/** The steps of the job's pipeline, in order. */
export function railSteps(job: Job): Step[] {
  return job.steps.filter((s) => s.in_pipeline)
}

/** Steps that ran, or run now: each earns a result card. */
export function resultSteps(job: Job): Step[] {
  return job.steps.filter((s) => s.in_run && (s.result || s.state === "running" || s.state === "waiting"))
}

export function firstLine(markdown: string): string {
  const line = markdown.split("\n").find((l) => l.trim() !== "") ?? ""
  return line.replace(/^[#>*\-\s]+/, "").trim()
}

/** The step after the approve that waits, so the plan gate can say where it goes. */
function stepAfter(job: Job, name: string): string {
  const rail = railSteps(job)
  const at = rail.findIndex((s) => s.name === name)
  return at >= 0 ? (rail[at + 1]?.name ?? "") : ""
}

/** Gate-aware verb text for the approve button (DESIGN-BRIEF §4.2). Empty when no approve waits. */
export function approveLabel(job: Job): string {
  if (job.state !== "needs_you" && !job.ready) return ""
  switch (job.gate) {
    case "plan": {
      const next = job.step ? stepAfter(job, job.step.name) : ""
      return next ? `Approve plan → continue to ${next}` : "Approve plan → continue"
    }
    case "proof":
      if (job.publish === "comment") return `Approve → comment on #${job.number}`
      return job.kind === "pr" ? "Approve → mark pull request ready" : "Approve → open pull request"
    case "cap":
      return "Approve → raise the cap"
    default:
      return ""
  }
}

export function unproven(claims: Claim[] = []): number {
  return claims.filter((c) => !c.proven).length
}

/** The sentence under the approve dialog's title. */
export function approveConsequence(job: Job): string {
  switch (job.gate) {
    case "plan":
      return "Approving the plan lets the job continue."
    case "proof": {
      const claims = job.claims ?? []
      const odd = unproven(claims)
      const action = job.publish === "comment"
        ? `posts a comment on #${job.number}`
        : job.kind === "pr" ? "marks the pull request ready" : "opens the pull request"
      const lead = `Approving the proof ${action}.`
      return claims.length === 0
        ? lead
        : `${lead} ${claims.length - odd} of ${claims.length} claims have evidence${odd ? `; ${odd} ${odd === 1 ? "is" : "are"} unproven` : ""}.`
    }
    default:
      return "Approving lets the job continue."
  }
}

function fileWord(n: number): string {
  return n === 1 ? "file" : "files"
}

/** `planned 4 files · changed 4 · +164 −22`, or just the changed half when no plan named files. */
export function changedLine(job: Job): string {
  const planned = job.planned_files?.length ?? 0
  const files = job.desk?.files ?? []
  const changed = files.length
  const added = files.reduce((n, f) => n + (f.added ?? 0), 0)
  const removed = files.reduce((n, f) => n + (f.removed ?? 0), 0)
  if (planned > 0 && changed > 0) {
    return `planned ${planned} ${fileWord(planned)} · changed ${changed} · +${added} −${removed}`
  }
  if (planned > 0) return `planned ${planned} ${fileWord(planned)}`
  if (changed === 0) return ""
  return `changed ${changed} ${fileWord(changed)} · +${added} −${removed}`
}

const ciMarks: Record<string, string> = { pass: "✓", passed: "✓", success: "✓", green: "✓", fail: "✕", failed: "✕", failure: "✕", red: "✕" }

export function ciMark(ci: string): string {
  return ciMarks[ci.toLowerCase()] ?? ci
}

export function clock(iso?: string): string {
  if (!iso) return ""
  const at = new Date(iso)
  return isNaN(at.getTime()) ? "" : at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}
