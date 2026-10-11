import { useEffect, useRef } from "react"

export interface QueueKeys {
  j: () => void
  k: () => void
  enter: () => void
  y: () => void
  a: () => void
  help: () => void
}

// The queue's keys (j/k/enter/y/a/?). Keys typed into a field or with a
// modifier belong to the browser or the field, and an open dialog owns the
// keyboard, so none of them fire then.
export function useQueueKeys(keys: QueueKeys, enabled: boolean) {
  const latest = useRef(keys)
  latest.current = keys

  useEffect(() => {
    if (!enabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const t = e.target as HTMLElement | null
      if (t?.closest("input, textarea, select, [contenteditable=true], [role=dialog]")) return
      const k = latest.current
      const run: Record<string, () => void> = {
        j: k.j, k: k.k, Enter: k.enter, y: k.y, a: k.a, "?": k.help,
      }
      // Enter on a focused link or button is that control's own act.
      if (e.key === "Enter" && t?.closest("a, button")) return
      const fn = run[e.key]
      if (!fn) return
      // Moving the cursor takes focus off a clicked link, so a later Enter
      // opens the job under the cursor and not that link again.
      if (e.key === "j" || e.key === "k") (document.activeElement as HTMLElement | null)?.blur()
      e.preventDefault()
      fn()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [enabled])
}
