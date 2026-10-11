import type { Evidence, Job } from "@/api/types"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

// Evidence reads as its kind and ref; a ref that already starts with the kind
// is not said twice, and a claim with neither says "none".
function evidenceText(e?: Evidence): string {
  if (!e || (!e.kind && !e.ref)) return "none"
  return e.kind && !e.ref.startsWith(e.kind) ? `${e.kind} ${e.ref}`.trim() : e.ref
}

export function Claims({ job }: { job: Job }) {
  const claims = job.claims ?? []
  return (
    <section className="flex flex-col gap-2" data-testid="claims">
      <h2 className="text-sm font-medium">Claims</h2>
      {claims.length === 0 ? (
        <p className="text-sm text-muted-foreground">Claims are written at proof.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Claim</TableHead>
              <TableHead>Evidence</TableHead>
              <TableHead className="w-24">Proven</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {claims.map((c, i) => (
              <TableRow key={i} data-testid={`claim-${i}`}>
                <TableCell className="whitespace-normal">{c.text}</TableCell>
                <TableCell className="font-mono text-xs whitespace-normal">
                  {evidenceText(c.evidence)}
                </TableCell>
                <TableCell>
                  {c.proven ? (
                    <Badge variant="secondary">✓ proven</Badge>
                  ) : (
                    <Badge variant="outline" className="border-destructive text-destructive">
                      unproven
                    </Badge>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  )
}
