import { BrowserRouter, Route, Routes } from "react-router"

import { QueueProvider } from "@/api/queue"
import { Layout } from "@/components/layout"
import { TooltipProvider } from "@/components/ui/tooltip"
import { AuditPage } from "@/pages/audit"
import { Coming } from "@/pages/coming"
import { JobPage } from "@/pages/job"
import { NewJobPage } from "@/pages/new-job"
import { QueuePage } from "@/pages/queue"
import { SettingsPage } from "@/pages/settings"

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
              <Route path="new" element={<NewJobPage />} />
              <Route path="audit" element={<AuditPage />} />
              <Route path="settings" element={<SettingsPage />} />
              <Route path="*" element={<Coming title="Not found" what="No page lives at this address." />} />
            </Route>
          </Routes>
        </QueueProvider>
      </TooltipProvider>
    </BrowserRouter>
  )
}
