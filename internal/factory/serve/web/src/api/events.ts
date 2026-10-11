import { useEffect, useRef } from "react"

// useEvents listens to /v1/events and calls onChanged on every `changed`
// tick (ARCHITECTURE §5: no state rides on SSE; the page re-reads). A factory
// without the stream answers 404, and EventSource gives up on a non-200
// answer by itself (readyState CLOSED), so the hook is then a quiet no-op.
export function useEvents(onChanged: (what: string) => void) {
  const latest = useRef(onChanged)
  latest.current = onChanged

  useEffect(() => {
    if (typeof EventSource === "undefined") return
    const source = new EventSource("/v1/events")
    const changed = (e: MessageEvent<string>) => latest.current(e.data)
    source.addEventListener("changed", changed)
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) source.close()
    }
    return () => source.close()
  }, [])
}
