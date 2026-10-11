import { useState } from "react"
import { Link, useParams } from "react-router"

import { api } from "@/api/client"
import { sentence, useJob } from "@/api/job"
import { useQueue } from "@/api/queue"
import { Claims } from "@/components/claims"
import { JobActions, type Verbs } from "@/components/job-actions"
import { JobCost } from "@/components/job-cost"
import { JobHeader } from "@/components/job-header"
import { JobQuestions } from "@/components/job-questions"
import { JobThread } from "@/components/job-thread"
import { Results } from "@/components/results"
import { StepRail } from "@/components/step-rail"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"

interface Notice {
  bad: boolean
  title: string
  text: string
}

// The job brief (DESIGN-BRIEF §4.2). Every verb answers on this page: a
// success is a sentence naming what the factory recorded, a refusal is the
// factory's own sentence.
export function JobPage() {
  const id = Number(useParams().id)
  const { job, messages, error, reload } = useJob(id)
  const queue = useQueue()
  const [notice, setNotice] = useState<Notice | null>(null)

  const run = async (call: () => Promise<string>, title: string): Promise<boolean> => {
    try {
      setNotice({ bad: false, title, text: await call() })
      reload()
      queue.reload()
      return true
    } catch (e) {
      setNotice({ bad: true, title: "That did not go through", text: sentence(e) })
      return false
    }
  }
  const verbs: Verbs = {
    approve: () => run(() => api.approve(id).then((r) => `Approved · recorded as ${r.event}`), "Approved"),
    requestChanges: (words) => run(() => api.requestChanges(id, words).then(() => "The manager sends the job back one step."), "Changes requested"),
    recheck: () => run(() => api.recheck(id).then(() => "Checks are running again."), "Re-running checks"),
    drop: () => run(() => api.drop(id).then(() => "The job is stopped."), "Dropped"),
  }

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6">
      <Link to="/mine" className="text-sm text-muted-foreground underline" data-testid="back">
        Back
      </Link>
      {error && (
        <Alert variant="destructive" data-testid="job-error">
          <AlertTitle>Could not read the job</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {notice && (
        <Alert variant={notice.bad ? "destructive" : "default"} data-testid={notice.bad ? "verb-error" : "verb-ok"}>
          <AlertTitle>{notice.title}</AlertTitle>
          <AlertDescription>{notice.text}</AlertDescription>
        </Alert>
      )}
      {job && (
        <>
          <JobHeader job={job} />
          <StepRail job={job} />
          <Results job={job} />
          <Claims job={job} />
          <JobQuestions
            questions={job.questions ?? []}
            answer={(words) => void run(() => api.answer(id, { words }).then(() => "Your answer is recorded."), "Answered")}
          />
          <JobThread
            messages={messages}
            reply={(words) => run(() => api.reply(id, words).then(() => "Sent to the manager."), "Reply sent")}
          />
          <JobCost job={job} />
          <JobActions job={job} verbs={verbs} />
        </>
      )}
    </div>
  )
}
