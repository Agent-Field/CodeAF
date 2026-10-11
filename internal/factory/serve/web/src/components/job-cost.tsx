import { clock } from "@/api/brief"
import { money } from "@/api/phrase"
import type { Job } from "@/api/types"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

export function JobCost({ job }: { job: Job }) {
  const paid = job.steps.filter((s) => s.in_run && (s.cost ?? 0) > 0)
  return (
    <section className="flex flex-col gap-2" data-testid="cost">
      <h2 className="text-sm font-medium">Cost</h2>
      {paid.length === 0 ? (
        <p className="text-sm text-muted-foreground">Cost appears once a worker starts the job.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Step</TableHead>
              <TableHead>Finished</TableHead>
              <TableHead className="text-right">Cost</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {paid.map((s) => (
              <TableRow key={s.name}>
                <TableCell>{s.name}</TableCell>
                <TableCell>{clock(s.at)}</TableCell>
                <TableCell className="text-right tabular-nums">{money(s.cost ?? 0)}</TableCell>
              </TableRow>
            ))}
            <TableRow className="font-medium" data-testid="cost-total">
              <TableCell>Total</TableCell>
              <TableCell />
              <TableCell className="text-right tabular-nums">{money(job.cost)}</TableCell>
            </TableRow>
          </TableBody>
        </Table>
      )}
      <p className="text-xs text-muted-foreground">The factory only shows cost.</p>
    </section>
  )
}
