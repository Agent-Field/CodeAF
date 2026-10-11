import { useState } from "react"

import { clock } from "@/api/brief"
import type { Message } from "@/api/types"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Markdown } from "@/components/markdown"

// The job's one manager thread: read here, one-shot reply here, steering in
// the TUI (DESIGN-BRIEF §3).
export function JobThread({ messages, reply }: { messages: Message[]; reply: (words: string) => Promise<boolean> }) {
  const [words, setWords] = useState("")
  const [sent, setSent] = useState(false)
  return (
    <section className="flex flex-col gap-2" data-testid="thread">
      <h2 className="text-sm font-medium">codeaf</h2>
      <p className="text-xs text-muted-foreground">This job's one manager thread. Every step's question goes to the manager first.</p>
      {messages.length === 0 && <p className="text-sm text-muted-foreground">The manager starts when someone runs the job.</p>}
      <ul className="flex flex-col gap-3">
        {messages.map((m, i) => (
          <li key={i} className="flex gap-2" data-testid={`message-${i}`}>
            <Avatar className="size-7">
              <AvatarFallback className="text-xs">{m.from === "manager" ? "af" : m.from.slice(0, 2)}</AvatarFallback>
            </Avatar>
            <div className="min-w-0 flex-1">
              <p className="text-xs text-muted-foreground">
                {m.from} {clock(m.at)}
              </p>
              <Markdown>{m.text}</Markdown>
            </div>
          </li>
        ))}
      </ul>
      <Textarea value={words} onChange={(e) => setWords(e.target.value)} placeholder="Message the manager" data-testid="reply-words" />
      <div className="flex items-center gap-3">
        <Button
          size="sm"
          disabled={!words.trim()}
          data-testid="reply-send"
          onClick={() =>
            void reply(words).then((ok) => {
              if (ok) setWords("")
              setSent(ok)
            })
          }
        >
          Reply
        </Button>
        <p className="text-xs text-muted-foreground" data-testid="reply-note">
          {sent && "Sent. "}Delivered to this job's manager in codeaf · deep steering happens in the TUI
        </p>
      </div>
    </section>
  )
}
