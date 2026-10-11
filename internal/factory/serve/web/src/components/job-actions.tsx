import { useState } from "react"

import { approveConsequence, approveLabel } from "@/api/brief"
import { jobLabel } from "@/api/phrase"
import type { Job } from "@/api/types"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"

export interface Verbs {
  approve: () => Promise<boolean>
  requestChanges: (words: string) => Promise<boolean>
  recheck: () => Promise<boolean>
  drop: () => Promise<boolean>
}

type Asking = "approve" | "changes" | "drop" | null

// Every verb confirms before it acts, and a dialog closes only when the
// factory took the verb: a refusal stays on the page as a sentence.
function Confirm({
  open,
  title,
  lead,
  body,
  go,
  verb,
  testid,
  onClose,
  children,
}: {
  open: boolean
  title: string
  lead: string
  body?: string
  go: () => Promise<boolean>
  verb: string
  testid: string
  onClose: () => void
  children?: React.ReactNode
}) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent data-testid={`${testid}-dialog`}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{lead}</DialogDescription>
        </DialogHeader>
        {body && <p className="text-sm">{body}</p>}
        {children}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button data-testid={`${testid}-confirm`} variant={testid === "drop" ? "destructive" : "default"} onClick={() => void go().then((ok) => ok && onClose())}>
            {verb}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function JobActions({ job, verbs }: { job: Job; verbs: Verbs }) {
  const [asking, setAsking] = useState<Asking>(null)
  const [reason, setReason] = useState("")
  if (job.state === "done" || job.state === "dropped") return null
  const label = approveLabel(job)
  const close = () => setAsking(null)
  return (
    <div className="sticky bottom-0 -mx-4 flex flex-wrap items-center gap-2 border-t bg-background px-4 py-3 md:-mx-6 md:px-6" data-testid="action-bar">
      <Button disabled={!label} data-testid="approve" onClick={() => setAsking("approve")}>
        {label || "Approve"}
      </Button>
      <Button variant="outline" disabled={job.state === "backlog"} data-testid="request-changes" onClick={() => setAsking("changes")}>
        Request changes
      </Button>
      <Button variant="outline" disabled={job.state === "backlog"} data-testid="recheck" onClick={() => void verbs.recheck()}>
        Re-run checks
      </Button>
      <Button variant="ghost" className="ml-auto text-destructive" data-testid="drop" onClick={() => setAsking("drop")}>
        Drop
      </Button>

      <Confirm
        open={asking === "approve"}
        title={`${label}?`}
        lead={jobLabel(job)}
        body={`${approveConsequence(job)} Recorded in the audit log as you.`}
        go={verbs.approve}
        verb={label}
        testid="approve"
        onClose={close}
      />
      <Confirm
        open={asking === "changes"}
        title="Request changes?"
        lead="The manager gets your reason and sends the job back one step."
        go={() => verbs.requestChanges(reason).then((ok) => (ok && setReason(""), ok))}
        verb="Request changes"
        testid="changes"
        onClose={close}
      >
        <Textarea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Reason" data-testid="changes-words" />
      </Confirm>
      <Confirm
        open={asking === "drop"}
        title="Drop this job?"
        lead="The job stops and its branch is deleted. The issue stays open on GitHub. Cost so far stays in the audit log."
        go={verbs.drop}
        verb="Drop job"
        testid="drop"
        onClose={close}
      />
    </div>
  )
}
