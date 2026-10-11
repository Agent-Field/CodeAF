import { useState } from "react"

import type { Me, RepoList } from "@/api/types"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { attachCommand, optedIn, takesWords } from "@/lib/settings"

// Every machine row shows the same Takes line. /v1/me keeps takes_unassigned
// on the person, and these machines pick up that one choice.
function Machines({ me }: { me: Me | null }) {
  const machines = me?.machines ?? []
  const takes = takesWords(optedIn(me?.takes_unassigned))
  return (
    <Card data-testid="machines-card">
      <CardHeader>
        <CardTitle>Your machines</CardTitle>
        <CardDescription>Workers running codeaf under your name. Each runs with your own keys.</CardDescription>
      </CardHeader>
      <CardContent>
        {machines.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Current job</TableHead>
                <TableHead>Takes</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {machines.map((machine) => (
                <TableRow key={machine.id || machine.name} data-testid={`machine-${machine.name}`}>
                  <TableCell>{machine.name}</TableCell>
                  <TableCell>{machine.status}</TableCell>
                  <TableCell>{machine.job ? `#${machine.job}` : ""}</TableCell>
                  <TableCell>{takes}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}

function Attach({ address }: { address: string }) {
  const command = attachCommand(address)
  const [copied, setCopied] = useState(false)
  return (
    <Card data-testid="attach-card">
      <CardHeader>
        <CardTitle>Attach</CardTitle>
        <CardDescription>Run this on a machine to make it one of your workers and steer jobs from the terminal.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <code className="min-w-0 flex-1 truncate rounded-md bg-muted px-3 py-2 text-sm" data-testid="attach-command">
          {command}
        </code>
        <Button
          type="button"
          variant="outline"
          data-testid="attach-copy"
          onClick={() => {
            void navigator.clipboard.writeText(command).then(
              () => setCopied(true),
              () => setCopied(false),
            )
          }}
        >
          Copy
        </Button>
        {copied && (
          <span className="text-sm text-muted-foreground" data-testid="attach-copied">
            Copied
          </span>
        )}
      </CardContent>
    </Card>
  )
}

// You: this person's machines and the command that adds one. Sign out is
// omitted on loopback; the factory keeps no session there to end.
export function YouSettings({
  me,
  repos,
  address,
  loopback,
}: {
  me: Me | null
  repos: RepoList | null
  address: string
  loopback: boolean
}) {
  const [signNote, setSignNote] = useState("")
  const takes = me?.takes_unassigned ?? {}
  return (
    <div className="flex flex-col gap-4" data-testid="settings-you">
      <Machines me={me} />
      <Attach address={address} />
      <TakeList repos={repos} takes={takes} />
      {!loopback && (
        <Card data-testid="sign-out-card">
          <CardHeader>
            <CardTitle>Sign out</CardTitle>
            <CardDescription>Ends this session. Your GitHub credential is removed from the factory.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col items-start gap-2">
            <Button type="button" variant="outline" data-testid="sign-out" onClick={() => setSignNote("not available")}>
              Sign out
            </Button>
            {signNote && <p className="text-sm text-muted-foreground">{signNote}</p>}
          </CardContent>
        </Card>
      )}
    </div>
  )
}

// The switches show takes_unassigned from /v1/me. Moving one says "not
// available": the factory has no write for that map yet, and a switch that
// only changed on screen would claim a choice the factory did not keep.
function TakeList({ repos, takes }: { repos: RepoList | null; takes: Record<string, boolean> }) {
  const [note, setNote] = useState("")
  const available = repos?.available ?? []
  return (
    <Card data-testid="take-unassigned-card">
      <CardHeader>
        <CardTitle>Take unassigned jobs</CardTitle>
        <CardDescription>Your machines pick up unassigned jobs from these repos.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {available.map((repo) => (
          <div key={repo.full} className="flex items-center justify-between gap-3">
            <span className="text-sm">{repo.full}</span>
            <Switch
              checked={Boolean(takes[repo.full])}
              aria-label={`Take unassigned jobs from ${repo.full}`}
              data-testid={`take-switch-${repo.full}`}
              onCheckedChange={() => setNote("not available")}
            />
          </div>
        ))}
        {note && (
          <p className="text-sm text-muted-foreground" data-testid="take-unavailable">
            {note}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
