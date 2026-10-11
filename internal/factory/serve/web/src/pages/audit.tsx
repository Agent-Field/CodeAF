import { useCallback, useEffect, useState } from "react"

import { actionPhrase, auditActor, auditTabs, auditTime, inAuditTab } from "@/api/audit"
import { api, FactoryError } from "@/api/client"
import { useEvents } from "@/api/events"
import { money } from "@/api/phrase"
import { useQueue } from "@/api/queue"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { Event } from "@/api/types"

// Audit log (DESIGN-BRIEF §4.4). One GET /v1/audit fills every tab; the
// record has no kind filter, and filtering here cannot disagree with Export,
// which downloads that same list as CSV.
export function AuditPage() {
  const { queue } = useQueue()
  const [events, setEvents] = useState<Event[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tab, setTab] = useState("all")

  const load = useCallback(() => {
    api.audit().then(
      (log) => {
        setEvents(log.events ?? [])
        setError(null)
      },
      (err: unknown) => setError(err instanceof FactoryError ? err.message : String(err)),
    )
  }, [])

  useEffect(load, [load])
  useEvents(load)

  const jobs = queue?.jobs ?? []
  const rows = (events ?? []).filter((event) => inAuditTab(tab, event.kind))

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-4" data-testid="audit-page">
      <div>
        <h1 className="text-xl font-semibold">Audit log</h1>
        <p className="text-sm text-muted-foreground">Every approval, answer, start, take and post. Append only.</p>
      </div>
      {error && (
        <Alert variant="destructive" data-testid="audit-error">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            {auditTabs.map((item) => (
              <TabsTrigger key={item.id} value={item.id} data-testid={`audit-tab-${item.id}`}>
                {item.title}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <Button variant="outline" data-testid="audit-export" onClick={() => void exportCSV(setError)}>
          Export
        </Button>
      </div>
      {events && rows.length === 0 && (
        <p className="text-sm text-muted-foreground" data-testid="audit-empty">
          No events of this kind yet.
        </p>
      )}
      {rows.length > 0 && (
        <Table data-testid="audit-table">
          <TableHeader>
            <TableRow>
              <TableHead>Time</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Action</TableHead>
              <TableHead className="text-right">Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((event) => {
              const who = auditActor(event.handle)
              return (
                <TableRow key={event.id} data-testid="audit-row">
                  <TableCell>{auditTime(event.at)}</TableCell>
                  <TableCell>
                    <span className="flex items-center gap-2">
                      <Avatar className="size-6">
                        <AvatarFallback>{who.replace("@", "").slice(0, 1).toUpperCase()}</AvatarFallback>
                      </Avatar>
                      {who}
                    </span>
                  </TableCell>
                  <TableCell>{actionPhrase(event, jobs)}</TableCell>
                  <TableCell className="text-right tabular-nums">{event.cost ? money(event.cost) : ""}</TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      )}
    </div>
  )
}

// Export is the record's own CSV, not a second file built in the browser.
// A failed read stays on this page as the factory's sentence.
async function exportCSV(setError: (words: string | null) => void) {
  let res: Response
  try {
    res = await fetch("/v1/audit?format=csv")
  } catch {
    setError("The factory could not be reached.")
    return
  }
  if (!res.ok) {
    const body: unknown = await res.json().catch(() => null)
    const words = (body as { error?: string } | null)?.error
    setError(words || `The factory answered ${res.status}.`)
    return
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = "factory-record.csv"
  document.body.appendChild(link)
  link.click()
  // Revoke after the click has started the download. Revoking in this turn
  // cancels it.
  setTimeout(() => {
    link.remove()
    URL.revokeObjectURL(url)
  }, 1000)
  setError(null)
}
