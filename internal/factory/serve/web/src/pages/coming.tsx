import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

// A page the scaffold names but a later lane builds; it sits inside the real
// shell so navigation can be tried end to end today.
export function Coming({ title, what }: { title: string; what: string }) {
  return (
    <div className="mx-auto max-w-2xl">
      <h1 className="mb-4 text-xl font-semibold">{title}</h1>
      <Card>
        <CardHeader>
          <CardTitle>Coming</CardTitle>
          <CardDescription>{what}</CardDescription>
        </CardHeader>
      </Card>
    </div>
  )
}
