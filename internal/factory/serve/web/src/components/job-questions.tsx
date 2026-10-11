import { useState } from "react"

import type { Question } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

function QuestionItem({ q, index, answer }: { q: Question; index: number; answer: (words: string) => void }) {
  const [words, setWords] = useState("")
  const open = q.state !== "answered"
  return (
    <li className="flex flex-col gap-1.5 rounded-md border p-3" data-testid={`question-${index}`}>
      <p className="text-sm">{q.text}</p>
      <div className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
        {q.from_step && <Badge variant="secondary">from {q.from_step}</Badge>}
        <Badge variant="outline">{open ? "open" : "answered"}</Badge>
        {q.about && <span>about {q.about}</span>}
        {q.routed_to && <span>routed to @{q.routed_to.replace(/^@/, "")}</span>}
      </div>
      {open ? (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            if (words.trim()) {
              answer(words)
              setWords("")
            }
          }}
        >
          <Input value={words} onChange={(e) => setWords(e.target.value)} placeholder="Answer in a line" data-testid={`question-words-${index}`} />
          <Button type="submit" size="sm" disabled={!words.trim()} data-testid={`question-answer-${index}`}>
            Answer
          </Button>
        </form>
      ) : (
        q.answer && (
          <p className="text-sm">
            @{q.answer.by}: {q.answer.text}
          </p>
        )
      )}
    </li>
  )
}

export function JobQuestions({ questions, answer }: { questions: Question[]; answer: (words: string) => void }) {
  return (
    <section className="flex flex-col gap-2" data-testid="questions">
      <h2 className="text-sm font-medium">Questions</h2>
      <p className="text-xs text-muted-foreground">Questions go to the job's manager first; you see the ones it could not answer.</p>
      {questions.length === 0 ? (
        <p className="text-sm text-muted-foreground">None for this job.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {questions.map((q, i) => (
            <QuestionItem key={i} q={q} index={i} answer={answer} />
          ))}
        </ul>
      )}
    </section>
  )
}
