import type { ApiError, Queue } from "./types"

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

function post(path: string, body: unknown = {}) {
  return request<unknown>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

export const api = {
  queue: () => request<Queue>("/queue"),
  approve: (id: number) => post(`/jobs/${id}/approve`),
  answer: (id: number, answer: { yes: boolean } | { words: string }) => post(`/jobs/${id}/answer`, answer),
}
