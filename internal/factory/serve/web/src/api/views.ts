import type { Job, JobState } from "./types"

// The three queue views of DESIGN-BRIEF §4.1. Mine is what waits on you plus
// anything assigned to you; routed questions (per-viewer gates) are not on the
// wire yet, so every needs_you job counts as waiting on the viewer.
export type View = "mine" | "unassigned" | "everything"

export const views: Record<View, { title: string; subtitle: string; path: string }> = {
  mine: { title: "Mine", subtitle: "Jobs you took, and anything waiting on you.", path: "/mine" },
  unassigned: {
    title: "Unassigned",
    subtitle: "Jobs nobody has taken. Take one and it runs on your machines.",
    path: "/unassigned",
  },
  everything: { title: "Everything", subtitle: "The whole pipeline on this factory.", path: "/" },
}

const finished: JobState[] = ["done", "dropped"]

export function inView(view: View, job: Job, me: string): boolean {
  switch (view) {
    case "mine":
      return job.state === "needs_you" || (me !== "" && job.assigned_to === me)
    case "unassigned":
      return !job.assigned_to && !finished.includes(job.state)
    case "everything":
      return true
  }
}

// Groups in the brief's order; dropped jobs sit last so nothing on the floor
// is hidden from Everything.
export const groups: { state: JobState; title: string }[] = [
  { state: "needs_you", title: "Needs you" },
  { state: "running", title: "Running" },
  { state: "queued", title: "Queued" },
  { state: "backlog", title: "Backlog" },
  { state: "done", title: "Done" },
  { state: "dropped", title: "Dropped" },
]
