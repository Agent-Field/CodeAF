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
      const lead = `Approving the proof ${job.kind === "pr" ? "marks the pull request ready" : "opens the pull request"}.`
      return claims.length === 0
        ? lead
        : `${lead} ${claims.length - odd} of ${claims.length} claims have evidence${odd ? `; ${odd} ${odd === 1 ? "is" : "are"} unproven` : ""}.`
    }
    default:
      return "Approving lets the job continue."
  }
}

/** `changed 4 files · +164 −22` from the files a pull request changes, when known. */
export function changedLine(job: Job): string {
  const files = job.desk?.files
  if (!files?.length) return ""
  const added = files.reduce((n, f) => n + (f.added ?? 0), 0)
  const removed = files.reduce((n, f) => n + (f.removed ?? 0), 0)
  return `changed ${files.length} ${files.length === 1 ? "file" : "files"} · +${added} −${removed}`
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
