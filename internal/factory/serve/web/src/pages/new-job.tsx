import { useEffect, useState, type FormEvent } from "react"

import { api } from "@/api/client"
import { sentence } from "@/api/job"
import { useQueue } from "@/api/queue"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"

// The floor list works when the factory's only read is Load. Names from
// GET /v1/repos are repositories you can see that may not have a job yet.
// Floor order is kept; a name already on the floor is not repeated.
function visibleRepos(floor: string[], available: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const name of [...floor, ...available]) {
    if (!name || seen.has(name)) continue
    seen.add(name)
    out.push(name)
  }
  return out
}

// New job (DESIGN-BRIEF §4.3). The floor's repositories are always listed,
// so a factory whose only read is Load still has a choice. GET /v1/repos
// adds repositories you can see that have no job yet; a factory without
// that read keeps the floor list and does not show the missing-read sentence.
export function NewJobPage() {
  const { queue, reload } = useQueue()
  const [extra, setExtra] = useState<string[]>([])
  const [repo, setRepo] = useState<string>()
  const [title, setTitle] = useState("")
  const [text, setText] = useState("")
  const [autostart, setAutostart] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sent, setSent] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.repos().then(
      (list) => setExtra((list.available ?? []).map((item) => item.full)),
      () => {},
    )
  }, [])

  const repos = visibleRepos(queue?.repos.map((item) => item.name) ?? [], extra)

  async function submit(e: FormEvent) {
    e.preventDefault()
    const name = title.trim()
    if (!repo || !name || busy) return
    setBusy(true)
    setError(null)
    setSent(null)
    try {
      const created = await api.newJob({ repo, title: name, text: text.trim(), autostart })
      reload()
      // The create answer is only an id. The GitHub number is on the job,
      // and a floor that has not stored it yet still has the id to show.
      let where = `job ${created.id}`
      try {
        const job = await api.job(created.id)
        if (job.number > 0) where = `${job.repo} #${job.number}`
      } catch {
        // The job was created; the floor may not list it on this read.
      }
      setSent(`${where} is in your Backlog`)
      setTitle("")
      setText("")
    } catch (err) {
      setError(sentence(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4" data-testid="new-job-page">
      <div>
        <h1 className="text-xl font-semibold">New job</h1>
        <p className="text-sm text-muted-foreground">Opens an issue on GitHub and puts the job in your Backlog.</p>
      </div>
      {error && (
        <Alert variant="destructive" data-testid="new-job-error">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {sent && (
        <Alert data-testid="new-job-sent">
          <AlertTitle>Sent to factory</AlertTitle>
          <AlertDescription>{sent}</AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Send a job to the factory</CardTitle>
          <CardDescription>Auto-start rules from .codeaf/factory.yaml decide when it runs.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="flex flex-col gap-4" onSubmit={(e) => void submit(e)}>
            <div className="flex flex-col gap-2">
              <span className="text-sm font-medium">Repository</span>
              <Select value={repo} onValueChange={setRepo}>
                <SelectTrigger className="w-full" data-testid="job-repo" aria-label="Repository">
                  <SelectValue placeholder="Repository" />
                </SelectTrigger>
                <SelectContent>
                  {repos.map((name) => (
                    <SelectItem key={name} value={name}>
                      {name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-2">
              <label className="text-sm font-medium" htmlFor="job-title">
                Title
              </label>
              <Input id="job-title" data-testid="job-title" value={title} onChange={(e) => setTitle(e.target.value)} />
            </div>
            <div className="flex flex-col gap-2">
              <label className="text-sm font-medium" htmlFor="job-text">
                Description
              </label>
              <Textarea id="job-text" data-testid="job-text" value={text} onChange={(e) => setText(e.target.value)} />
            </div>
            <div className="flex items-center justify-between gap-3">
              <label className="text-sm font-medium" htmlFor="job-autostart">
                Auto-start when rules allow
              </label>
              <Switch id="job-autostart" checked={autostart} onCheckedChange={setAutostart} data-testid="job-autostart" />
            </div>
            <Button type="submit" data-testid="send-job" disabled={!repo || !title.trim() || busy}>
              Send to factory
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
