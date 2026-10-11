import type { DryRun, Pipeline, StepDef } from "@/api/types"

// The page decides loopback from the address in the browser. That is the
// same set the server leaves open without a session: localhost, 127.0.0.0/8
// and ::1. Sign out is hidden there because there is no session to end.
export function onLoopback(host: string): boolean {
  const name = host.toLowerCase().replace(/^\[|\]$/g, "")
  if (name === "localhost" || name === "::1") return true
  const parts = name.split(".")
  if (parts.length !== 4 || parts[0] !== "127") return false
  return parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255)
}

// Go prints a whole number of minutes as "1m0s". The page says that interval
// in seconds, which is how a person reads "polling every 60s".
export function pollingWords(every: string): string {
  const minutes = /^(\d+)m0s$/.exec(every)
  if (minutes) return `${Number(minutes[1]) * 60}s`
  return every
}

export function githubStatus(link: { login: string; via: string; polling_every?: string }): string {
  const who = link.login
    ? `connected as @${link.login}${link.via ? ` via ${link.via}` : ""}`
    : "not connected"
  return link.polling_every ? `${who} · polling every ${pollingWords(link.polling_every)}` : who
}

// source "defaults" is the handler's mark for a repository with no checkout,
// which is a repository with no pipeline file.
export function pipelineLabel(source: string | undefined): string {
  return source === "defaults" ? "no file · using defaults" : ".codeaf/factory.yaml"
}

function stepLine(step: StepDef): string {
  const name = step.off ? `${step.name} (off)` : step.name
  return step.ask ? `${name} — ${step.ask}` : name
}

// The endpoint returns the parsed pipeline, not the file bytes, so this is
// the read the page can show without inventing YAML.
export function pipelineBody(pipeline: Pipeline): string {
  const lines = (pipeline.steps ?? []).map(stepLine)
  for (const kind of Object.keys(pipeline.by_kind ?? {}).sort()) {
    lines.push("", kind, ...(pipeline.by_kind?.[kind] ?? []).map(stepLine))
  }
  if (pipeline.policy?.length) lines.push("", "policy", ...pipeline.policy)
  return lines.join("\n")
}

export function dryRunWords(run: DryRun): string {
  const start = run.would_start ?? []
  const ask = run.would_ask ?? []
  const started = start.map((n) => `#${n}`).join(" ")
  const asked = ask.map((a) => `#${a.job} ${a.reason}`.trim()).join(" · ")
  const startClause = started ? `: ${started}` : ""
  const askClause = asked ? `: ${asked}` : ""
  return `Under these rules, ${start.length} issues would auto-start now${startClause} · ${ask.length} would ask first${askClause}`
}

// 404 and 501 are both "the factory does not have this yet". Any other
// status is a real failure and keeps the factory's sentence.
export function endpointMissing(status: number): boolean {
  return status === 404 || status === 501
}

export function attachCommand(address: string): string {
  const target = address.trim()
  return target ? `codeaf attach ${target}` : "codeaf attach"
}

export function optedIn(takes: Record<string, boolean> | null | undefined): string[] {
  return Object.entries(takes ?? {})
    .filter(([, on]) => on)
    .map(([name]) => name)
}

export function takesWords(repos: string[]): string {
  return repos.length === 0 ? "" : `takes unassigned: ${repos.join(", ")}`
}
