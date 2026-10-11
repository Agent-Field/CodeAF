import { BrowserRouter, Route, Routes, useParams } from "react-router"

import { QueueProvider } from "@/api/queue"
import { Layout } from "@/components/layout"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Coming } from "@/pages/coming"
import { QueuePage } from "@/pages/queue"

function JobPage() {
  const { id } = useParams()
  return <Coming title={`Job ${id}`} what="The job brief: steps, results, claims, questions and the approve." />
}

// Paths are real URLs: the factory answers any non-/v1 path with this page,
// so a link to /jobs/12 opens straight onto that job.
export function App() {
  return (
    <BrowserRouter>
      <TooltipProvider>
        <QueueProvider>
          <Routes>
            <Route element={<Layout />}>
              <Route index element={<QueuePage view="everything" />} />
              <Route path="mine" element={<QueuePage view="mine" />} />
              <Route path="unassigned" element={<QueuePage view="unassigned" />} />
              <Route path="jobs/:id" element={<JobPage />} />
              <Route path="new" element={<Coming title="New job" what="Send a job to the factory: repository, title, description, auto-start." />} />
              <Route path="audit" element={<Coming title="Audit log" what="Who did what to which job, at what cost, from the record." />} />
              <Route path="settings" element={<Coming title="Settings" what="This factory, your machines, and which repos they take unassigned jobs from." />} />
              <Route path="*" element={<Coming title="Not found" what="No page lives at this address." />} />
            </Route>
          </Routes>
        </QueueProvider>
      </TooltipProvider>
    </BrowserRouter>
  )
}
