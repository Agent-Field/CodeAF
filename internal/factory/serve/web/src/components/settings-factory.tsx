import { Fragment, useRef, useState } from "react"

import { api, FactoryError } from "@/api/client"
import { sentence } from "@/api/job"
import type { AvailableRepo, GitHubLink, PipelineRead, RepoList } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { dryRunWords, endpointMissing, githubStatus, pipelineBody, pipelineLabel } from "@/lib/settings"

function GitHubCard({ link, onConnect }: { link: GitHubLink | null; onConnect: (token: string) => Promise<void> }) {
  return (
    <Card data-testid="github-card">
      <CardHeader>
        <CardTitle>GitHub</CardTitle>
        <CardDescription>How the factory reads issues and posts pull requests.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {link && <p data-testid="github-status">{githubStatus(link)}</p>}
        <form
          className="flex flex-col gap-2 sm:flex-row"
          onSubmit={(e) => {
            e.preventDefault()
            const token = String(new FormData(e.currentTarget).get("token") ?? "")
            e.currentTarget.reset()
            void onConnect(token)
          }}
        >
          <Input
            name="token"
            type="password"
            autoComplete="off"
            placeholder="Token, or leave empty to use gh"
            aria-label="GitHub token"
            data-testid="github-token"
          />
          <Button type="submit" data-testid="github-connect">
            {link?.login ? "Reconnect" : "Connect"}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

function AccessCard({ loopback }: { loopback: boolean }) {
  return (
    <Card data-testid="access-card">
      <CardHeader>
        <CardTitle>Access</CardTitle>
        <CardDescription>How people reach this factory before they sign in.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-1">
        <p data-testid="access-mode">{loopback ? "localhost only" : "access token"}</p>
        {loopback && <p className="text-sm text-muted-foreground">no sign-in on this laptop</p>}
      </CardContent>
    </Card>
  )
}

function RepoRows({
  available,
  watched,
  onWatch,
}: {
  available: AvailableRepo[]
  watched: string[]
  onWatch: (full: string, on: boolean) => Promise<void>
}) {
  const [open, setOpen] = useState<string | null>(null)
  const [pipes, setPipes] = useState<Record<string, PipelineRead>>({})
  const [pipeError, setPipeError] = useState<Record<string, string>>({})
  const [runs, setRuns] = useState<Record<string, string>>({})
  const asked = useRef(new Set<string>())

  // Pipeline and dry run load when the row opens. A missing dry-run endpoint
  // is "not available", not a guess about which jobs would start.
  const reveal = (repo: AvailableRepo) => {
    setOpen((cur) => (cur === repo.full ? null : repo.full))
    if (asked.current.has(repo.full)) return
    asked.current.add(repo.full)
    api.pipeline(repo.owner, repo.name).then(
      (pipeline) => setPipes((cur) => ({ ...cur, [repo.full]: pipeline })),
      (e: unknown) => setPipeError((cur) => ({ ...cur, [repo.full]: sentence(e) })),
    )
    api.dryRun(repo.owner, repo.name).then(
      (run) => setRuns((cur) => ({ ...cur, [repo.full]: dryRunWords(run) })),
      (e: unknown) =>
        setRuns((cur) => ({
          ...cur,
          [repo.full]: e instanceof FactoryError && endpointMissing(e.status) ? "not available" : sentence(e),
        })),
    )
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>Pipeline</TableHead>
          <TableHead>Watch</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {available.map((repo) => {
          const shown = open === repo.full
          const pipeline = pipes[repo.full]
          const file = pipeline ? pipelineLabel(pipeline.source) : ""
          return (
            <Fragment key={repo.full}>
              <TableRow>
                <TableCell>
                  <button
                    type="button"
                    className="text-left font-medium hover:underline"
                    aria-expanded={shown}
                    data-testid={`repo-open-${repo.full}`}
                    onClick={() => reveal(repo)}
                  >
                    {repo.full}
                  </button>
                </TableCell>
                <TableCell data-testid={`repo-file-${repo.full}`}>{file}</TableCell>
                <TableCell>
                  <Switch
                    checked={watched.includes(repo.full)}
                    onCheckedChange={(on) => void onWatch(repo.full, on)}
                    aria-label={`Watch ${repo.full}`}
                    data-testid={`repo-switch-${repo.full}`}
                  />
                </TableCell>
              </TableRow>
              {shown && (
                <TableRow>
                  <TableCell colSpan={3} className="whitespace-normal">
                    <div className="flex flex-col gap-3 py-2">
                      {pipeError[repo.full] && <p className="text-sm text-destructive">{pipeError[repo.full]}</p>}
                      {pipeline && (
                        <div data-testid={`pipeline-${repo.full}`} className="flex flex-col gap-2">
                          <Badge variant="outline">{pipeline.source === "defaults" ? "defaults" : "read-only · edit in the repo"}</Badge>
                          {pipeline.source === "defaults" && <p>no file · using defaults</p>}
                          <pre className="overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap">{pipelineBody(pipeline)}</pre>
                          {(pipeline.problems ?? []).map((problem) => (
                            <p key={problem} className="text-sm text-muted-foreground">
                              {problem}
                            </p>
                          ))}
                        </div>
                      )}
                      <div>
                        <p className="text-sm font-medium">Dry run</p>
                        <p data-testid={`dryrun-${repo.full}`}>{runs[repo.full] ?? ""}</p>
                        {runs[repo.full] && runs[repo.full] !== "not available" && (
                          <p className="text-sm text-muted-foreground">Nothing starts from a dry run.</p>
                        )}
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          )
        })}
      </TableBody>
    </Table>
  )
}

// This factory: GitHub, access, and the repositories this factory watches.
// Access has no control because the host file is where that choice is made.
export function FactorySettings({
  github,
  repos,
  loopback,
  onConnect,
  onWatch,
}: {
  github: GitHubLink | null
  repos: RepoList | null
  loopback: boolean
  onConnect: (token: string) => Promise<void>
  onWatch: (full: string, on: boolean) => Promise<void>
}) {
  const available = repos?.available ?? []
  return (
    <div className="flex flex-col gap-4" data-testid="settings-factory">
      <GitHubCard link={github} onConnect={onConnect} />
      <AccessCard loopback={loopback} />
      <Card>
        <CardHeader>
          <CardTitle>Repos</CardTitle>
          <CardDescription>Each repo carries its own pipeline file on main.</CardDescription>
        </CardHeader>
        <CardContent>
          {available.length > 0 && (
            <RepoRows available={available} watched={repos?.watched ?? []} onWatch={onWatch} />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
