import { ChevronDown, ChevronRight } from "lucide-react"
import { useState } from "react"

import { changedLine, clock, firstLine, resultSteps } from "@/api/brief"
import { money } from "@/api/phrase"
import type { Job, Step } from "@/api/types"
import { Markdown } from "@/components/markdown"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader } from "@/components/ui/card"

function ResultCard({ step, open: initial }: { step: Step; open: boolean }) {
  const [open, setOpen] = useState(initial)
  const Chevron = open ? ChevronDown : ChevronRight
  return (
    <Card data-testid={`result-${step.name}`} className="gap-0 py-0">
      <CardHeader className="p-0">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen(!open)}
          className="flex w-full flex-wrap items-center gap-2 px-4 py-3 text-left text-sm"
        >
          <Chevron className="size-4 shrink-0" />
          <span className="font-medium">{step.name}</span>
          {step.fixed && <Badge variant="secondary">fixed</Badge>}
          {step.custom && <Badge variant="secondary">custom step</Badge>}
          {step.state === "running" && <Badge variant="outline">running</Badge>}
          {step.state === "waiting" && <Badge variant="outline">waiting</Badge>}
          {!open && <span className="min-w-0 flex-1 truncate text-muted-foreground">{firstLine(step.result ?? "")}</span>}
          <span className="ml-auto text-xs text-muted-foreground">
            {clock(step.at)} {step.cost ? money(step.cost) : ""}
          </span>
        </button>
      </CardHeader>
      {open && step.result && (
        <CardContent className="border-t px-4 py-3">
          <Markdown>{step.result}</Markdown>
        </CardContent>
      )}
    </Card>
  )
}

// One card per step that ran, the last two open. Cards remount per job so a
// refresh keeps what the person opened.
export function Results({ job }: { job: Job }) {
  const steps = resultSteps(job)
  const changed = changedLine(job)
  return (
    <section className="flex flex-col gap-2" data-testid="results">
      <h2 className="text-sm font-medium">Results</h2>
      {changed && (
        <p className="text-sm text-muted-foreground" data-testid="changed-line">
          {changed}
        </p>
      )}
      {steps.length === 0 && (
        <p className="text-sm text-muted-foreground">No results yet. Each step adds a summary here when it finishes.</p>
      )}
      {steps.map((s, i) => (
        <ResultCard key={s.name} step={s} open={i >= steps.length - 2} />
      ))}
    </section>
  )
}
