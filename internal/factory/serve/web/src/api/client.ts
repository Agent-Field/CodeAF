import type { ApiError, Approved, DryRunRead, Envelope, GitHubLink, Job, Me, PipelineRead, Queue, RepoList, Thread } from "./types"

// The API is same-origin at /v1 (ARCHITECTURE §7). Every failure, whether the
// server's {error} or a network fault, reaches the page as one FactoryError
// whose message is a sentence a person can read as-is.
export class FactoryError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = "FactoryError"
    this.status = status
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/v1${path}`, {
      ...init,
      headers: { Accept: "application/json", ...init?.headers },
    })
  } catch {
    throw new FactoryError("The factory could not be reached.", 0)
  }
  const body: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const words = (body as Partial<ApiError> | null)?.error
    throw new FactoryError(words || `The factory answered ${res.status}.`, res.status)
  }
  return body as T
}

function send<T>(method: string, path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

function post(path: string, body: unknown = {}) {
  return send<unknown>("POST", path, body)
}

export const api = {
  queue: () => request<Queue>("/queue"),
  job: (id: number) => request<Job>(`/jobs/${id}`),
  thread: (id: number) => request<Thread>(`/jobs/${id}/thread`),
  // The approval is recorded before it acts; the response names that event.
  approve: (id: number) => post(`/jobs/${id}/approve`) as Promise<Approved>,
  requestChanges: (id: number, words: string) => post(`/jobs/${id}/request_changes`, { words }),
  recheck: (id: number) => post(`/jobs/${id}/recheck`),
  drop: (id: number) => post(`/jobs/${id}/drop`),
  take: (id: number) => post(`/jobs/${id}/take`),
  reply: (id: number, words: string) => post(`/jobs/${id}/reply`, { words }),
  answer: (id: number, answer: { yes: boolean } | { words: string }) => post(`/jobs/${id}/answer`, answer),
  // Settings. Connect sends token even when it is empty: an empty token is
  // how the factory is told to use gh's existing login.
  github: () => request<GitHubLink>("/github"),
  connectGitHub: (token: string) => send<Envelope>("POST", "/github/connect", { token }),
  repos: () => request<RepoList>("/repos"),
  setRepos: (watched: string[]) => send<Envelope>("PUT", "/repos", { watched }),
  pipeline: (owner: string, repo: string) =>
    request<PipelineRead>(`/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/pipeline`),
  dryRun: (owner: string, repo: string) =>
    request<DryRunRead>(`/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/dryrun`),
  me: () => request<Me>("/me"),
}
