import { useCallback, useEffect, useState } from "react"

import { api, FactoryError } from "./client"
import { useEvents } from "./events"
import type { Job, Message } from "./types"

export function sentence(e: unknown): string {
  return e instanceof FactoryError ? e.message : String(e)
}

// One job and its manager thread, read together and re-read on every
// `changed` tick (ARCHITECTURE §5). A thread that cannot be read leaves the
// brief standing: only the job's own failure is the page's error.
export function useJob(id: number) {
  const [job, setJob] = useState<Job | null>(null)
  const [messages, setMessages] = useState<Message[]>([])
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(() => {
    api.job(id).then(
      (j) => {
        setJob(j)
        setError(null)
      },
      (e: unknown) => setError(sentence(e)),
    )
    api.thread(id).then(
      (t) => setMessages(t.messages ?? []),
      () => {},
    )
  }, [id])

  useEffect(() => {
    setJob(null)
    setMessages([])
    reload()
  }, [reload])
  useEvents(reload)

  return { job, messages, error, reload }
}
