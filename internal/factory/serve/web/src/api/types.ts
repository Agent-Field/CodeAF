// The factory's wire, as the dashboard reads it.
//
// ONE SOURCE OF TRUTH: internal/factory/api/wire.go. Every type here mirrors a
// Go type there, field for field, with the same JSON names; when wire.go
// changes, this file changes in the same commit. Times arrive as RFC 3339
// strings. Fields wire.go marks `omitempty` are optional here. Desk fields
// (what only the TUI draws) are kept so the two clients read one shape.

/** Factory names the server every response comes from. */
export interface Factory {
  address: string
  version: string
}

/** Every response carries the factory it came from; errors carry a sentence. */
export interface Envelope {
  factory: Factory
}

export interface ApiError extends Envelope {
  error: string
}

/** `GET /v1/queue`: the whole floor as one person sees it. */
export interface Queue extends Envelope {
  since: Since
  jobs: Job[]
  repos: Repo[]
  strip: Cell[]
  capacity: Capacity
  me: string
  desk?: QueueDesk
}

export interface QueueDesk {
  now: string
  sources: Source[]
  busy?: Record<number, string>
  busy_all?: string
  last_read_cost?: number
  marked?: number[]
  checkouts: Record<string, string> | null
}

export interface Since {
  at: string
  shipped: number
  arrived: number
  asked: number
  handled: number
  spent: number
  hours: number[]
  shipping?: string[]
}

export interface Cell {
  step: string
  jobs: number
  waiting?: boolean
}

export interface Capacity {
  benches: number
  daily: number
  rail: number
  queued: number
  workers?: Worker[]
}

export interface Source {
  name: string
  writes?: boolean
  repos?: string[]
  polled: string
  trouble?: string
  polling?: boolean
  reading?: string
  read?: number
  of?: number
  items?: number
}

export interface Repo {
  name: string
  watched: boolean
  checkout?: string
  pipeline?: Pipeline
  team?: string
  areas?: string[]
  habits?: string[]
  hue?: number
  ci_red?: boolean
}

export interface Pipeline {
  source?: string
  steps: StepDef[]
  by_kind?: Record<string, StepDef[]>
  policy?: string[]
  habits?: string[]
  adapt?: Record<string, string>
}

export interface StepDef {
  name: string
  kind?: string
  ask?: string
  until?: string
  max?: number
  fixed?: boolean
  custom?: boolean
  off?: boolean
  when?: string
  effort?: string
  fanout?: string
  proof?: string[]
  why?: string
  by?: string
}

export type JobState = "backlog" | "queued" | "running" | "needs_you" | "done" | "dropped"
export type Gate = "plan" | "proof" | "question" | "cap" | ""
export type StepState = "pending" | "running" | "done" | "failed" | "waiting" | "skipped"

/** One unit of work on the floor (Go: factory.Item). */
export interface Job {
  id: number
  repo: string
  number: number
  kind: string
  title: string
  url?: string
  state: JobState
  ready?: boolean
  gate: Gate
  waiting_since?: string
  asked_first_reason?: string
  // Who holds the job: the person rule (assignee, then taker, then starter).
  assigned_to?: string
  step?: StepAt
  steps: Step[]
  claims?: Claim[]
  questions?: Question[]
  cost: number
  created_at: string
  pr?: PR
  desk?: JobDesk
}

export interface JobDesk {
  product?: string
  places?: string[]
  body?: string
  author?: string
  assignee?: string
  starter?: string
  // The person who took otherwise unassigned work. assigned_to is who holds it.
  taker?: string
  tier?: string
  origin?: string
  synced?: boolean
  changed_at: string
  triage: Triage
  cap?: number
  policy?: Claim[]
  marked?: boolean
  labels?: string[]
  checks?: string
  diff?: string
  adapted?: string[]
  notes?: string[]
  talk?: string
  heard: string
  comments: Comment[] | null
  files?: File[]
  check_runs?: CheckRun[]
  base?: string
  head?: string
  head_sha?: string
  activity?: Activity[]
  run?: Run
}

export interface Run {
  started: string
  ended: string
  activity?: number[]
  log?: LogLine[]
  paused?: boolean
  findings?: number
  bench?: number
  room?: string
}

export interface StepAt {
  index: number
  count: number
  name: string
}

export interface Step extends StepDef {
  state: StepState
  round?: number
  result?: string
  cost?: number
  at?: string
  note?: string
  tasks?: number
  left_ms?: number
  chat?: string
  handle?: string
  run_kind?: string
  in_pipeline: boolean
  in_run: boolean
}

export interface Claim {
  text: string
  evidence?: Evidence
  proven: boolean
}

export interface Evidence {
  kind: string
  ref: string
}

export interface Question {
  text: string
  about?: string
  from_step?: string
  routed_to?: string
  state: string
  answer?: Answer
  kind?: string
  with?: string
  options?: string[]
  pick?: string
  why?: string
  at?: string
}

export interface Answer {
  by: string
  text: string
  event?: string
}

export interface PR {
  number: number
  url: string
  ci?: string
}

export interface Triage {
  type?: string
  size?: string
  area?: string
  readiness?: number
  est?: number
  dup_of?: number
  risk?: string[]
  read?: string
  questions?: string[]
  dup?: string
  priority?: number
  reason?: string
  triaged_at: string
}

export interface LogLine {
  at: string
  glyph?: string
  tone?: string
  text: string
}

export interface Comment {
  author: string
  body: string
  at: string
}

export interface File {
  path: string
  added?: number
  removed?: number
}

export interface CheckRun {
  name: string
  state: string
  url?: string
}

export interface Activity {
  at: string
  what: string
}

export interface Message {
  at: string
  from: string
  text: string
}

/** `GET /v1/jobs/{id}/thread`: the job's one manager thread. */
export interface Thread extends Envelope {
  messages: Message[]
}

/** `POST /v1/jobs/{id}/approve`: the audit event the approval was recorded as. */
export interface Approved extends Envelope {
  event: string
}

export interface Event {
  id: string
  at: string
  kind: string
  job?: number
  handle: string
  cost?: number
  detail?: string
}

export interface Worker {
  id: string
  name: string
  handle: string
  status: string
  job?: number
}

export interface DryRun {
  would_start: number[]
  would_ask: Ask[]
}

export interface Ask {
  job: number
  reason: string
}

// The reads below are the settings page's. They mirror the handler structs
// in internal/factory/serve (github, repos, me, pipeline), which are not
// types in wire.go. Field names are the JSON names.

/** `GET /v1/github`: how this factory reaches GitHub. */
export interface GitHubLink extends Envelope {
  login: string
  via: string
  polling_every?: string
}

/** One repository the connected account can see (`GET /v1/repos`). */
export interface AvailableRepo {
  full: string
  name: string
  owner: string
  private: boolean
  pushed: string
  open: number
  checkout?: string
}

/** `GET /v1/repos`: the watched names and every repository available to watch. */
export interface RepoList extends Envelope {
  watched: string[]
  available: AvailableRepo[]
}

/** `GET /v1/repos/{owner}/{repo}/pipeline`: the parsed pipeline, read-only. */
export interface PipelineRead extends Envelope, Pipeline {
  problems?: string[]
  checkout?: string
}

/** `GET /v1/repos/{owner}/{repo}/dryrun`. Absent until that endpoint exists. */
export interface DryRunRead extends Envelope, DryRun {}

/** `GET /v1/me`: the person looking, their machines, and unassigned-work choices. */
export interface Me extends Envelope {
  handle: string
  machines: Worker[] | null
  takes_unassigned: Record<string, boolean> | null
}
