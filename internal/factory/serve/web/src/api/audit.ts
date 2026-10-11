import type { Event, Job } from "./types"

// The five tabs of the audit log. Kinds not listed here (a drop, a post, a
// request for changes) stay on All: the record has more kinds than the tabs,
// and a tab must not hide an event by pretending it belongs somewhere else.
export const auditTabs = [
  { id: "all", title: "All" },
  { id: "approvals", title: "Approvals" },
  { id: "answers", title: "Answers" },
  { id: "starts", title: "Starts" },
  { id: "takes", title: "Takes" },
] as const

const tabKinds: Record<string, readonly string[]> = {
  approvals: ["approved"],
  answers: ["answered"],
  starts: ["started", "auto_started"],
  takes: ["took"],
}

export function inAuditTab(tab: string, kind: string): boolean {
  if (tab === "all") return true
  return tabKinds[tab]?.includes(kind) ?? false
}

// An empty handle is an action the factory took by itself. The table says
// `factory` so the User column is never a blank cell.
export function auditActor(handle: string): string {
  const name = handle.trim().replace(/^@/, "")
  return name ? `@${name}` : "factory"
}

// The record stores the factory job id. A person knows the GitHub number, so
// the phrase uses that when this floor already has the job.
function issueRef(event: Event, jobs: Job[]): string {
  const job = jobs.find((item) => item.id === event.job)
  const n = job && job.number > 0 ? job.number : event.job
  return n ? `#${n}` : ""
}

function words(...parts: string[]): string {
  return parts.filter(Boolean).join(" ")
}

function join(lead: string, tail: string): string {
  return tail ? `${lead} · ${tail}` : lead
}

// One phrase per record kind, built only from the event's own fields. The
// page does not keep a second spelling of what happened.
export function actionPhrase(event: Event, jobs: Job[]): string {
  const ref = issueRef(event, jobs)
  const detail = event.detail?.trim() ?? ""
  switch (event.kind) {
    case "approved":
      return join(words("approved", ref), event.id)
    case "request_changes":
      return join(words("requested changes on", ref), event.id)
    case "auto_started":
      return detail ? `${words("auto-started", ref)} by rule ${detail}` : words("auto-started", ref)
    case "posted":
      return detail ? `posted ${detail}` : words("posted", ref)
    case "answered":
      return join(words("answered", ref), detail)
    case "dropped":
      return join(words("dropped", ref), detail)
    case "took":
      return words("took", ref)
    case "started":
      return words("started", ref)
    default:
      return words(event.kind, ref, detail)
  }
}

export function auditTime(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return ""
  return at.toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })
}
