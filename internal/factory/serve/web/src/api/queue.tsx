import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react"

import { api, FactoryError } from "./client"
import { useEvents } from "./events"
import type { Queue } from "./types"

interface QueueRead {
  queue: Queue | null
  error: string | null
  reload: () => void
}

const QueueContext = createContext<QueueRead>({ queue: null, error: null, reload: () => {} })

// One read of /v1/queue feeds the whole shell: the sidebar's address and
// badges and the queue views all draw the same snapshot, so they can never
// disagree about the floor. A `changed` tick re-reads it.
export function QueueProvider({ children }: { children: ReactNode }) {
  const [queue, setQueue] = useState<Queue | null>(null)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(() => {
    api.queue().then(
      (q) => {
        setQueue(q)
        setError(null)
      },
      (e: unknown) => setError(e instanceof FactoryError ? e.message : String(e)),
    )
  }, [])

  useEffect(reload, [reload])
  useEvents(reload)

  return <QueueContext.Provider value={{ queue, error, reload }}>{children}</QueueContext.Provider>
}

export function useQueue() {
  return useContext(QueueContext)
}
