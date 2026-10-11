import { useState } from "react"

import { jobLabel } from "@/api/phrase"
import type { Job } from "@/api/types"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"

export const keyHelp: [string, string][] = [
  ["j / k", "move down / up"],
  ["enter", "open the job"],
  ["y", "approve the job under the cursor"],
  ["a", "answer the job under the cursor"],
  ["?", "show these keys"],
]

export function KeysDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="keys-dialog">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>On a queue view.</DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[6rem_1fr] gap-y-2 text-sm">
          {keyHelp.map(([key, what]) => (
            <div key={key} className="contents">
              <dt>
                <kbd className="rounded border bg-muted px-1.5 py-0.5 font-mono text-xs">{key}</kbd>
              </dt>
              <dd className="text-muted-foreground">{what}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}

// A question gate is answered yes, no, or in words.
export function AnswerDialog({
  job,
  onClose,
  send,
}: {
  job: Job | null
  onClose: () => void
  send: (job: Job, answer: { yes: boolean } | { words: string }) => void
}) {
  const [words, setWords] = useState("")
  const question = job?.questions?.find((q) => q.state !== "answered")?.text
  const done = (answer: { yes: boolean } | { words: string }) => {
    if (job) send(job, answer)
    setWords("")
  }
  return (
    <Dialog open={job !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent data-testid="answer-dialog">
        <DialogHeader>
          <DialogTitle>{job ? `Answer ${jobLabel(job)}` : "Answer"}</DialogTitle>
          <DialogDescription>{question ?? "The job is waiting for your answer."}</DialogDescription>
        </DialogHeader>
        <Textarea
          value={words}
          onChange={(e) => setWords(e.target.value)}
          placeholder="Or answer in words"
          data-testid="answer-words"
        />
        <DialogFooter>
          <Button variant="outline" data-testid="answer-no" onClick={() => done({ yes: false })}>
            No
          </Button>
          <Button variant="outline" data-testid="answer-yes" onClick={() => done({ yes: true })}>
            Yes
          </Button>
          <Button disabled={words.trim() === ""} data-testid="answer-send" onClick={() => done({ words })}>
            Send
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
