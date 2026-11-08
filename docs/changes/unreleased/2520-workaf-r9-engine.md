---
kind: fixed
title: An AgentField run that ended before a quit is read after restart; empty PRs read nothing to change
pr: 2520
surface: [factory, engine]
invalidates:
  - "Was true: a restarted round adopted an ended AgentField run only when job.Continuing and the step's Started was no later than the run's Began, so a step whose start had moved (a round re-entered after an earlier restart) dispatched a second run, in Manual too (r9 job 306). Is true now: agentfield_run.end_owed is set at dispatch and cleared when the step reads the end; any round adopts an owed end with no dispatch, and AgentFieldRunOwed reads the same field. A cancelled or rejected run is never adopted. Records without end_owed keep the old clock check."
  - "Was true: a pull request with 0 files read shipped and offered Merge, and an empty file list was always 'not read yet'. Is true now: a head whose file list is empty and whose changed_files/additions/deletions are 0 records files_head with no files (factory.PRChangesNothing); MergeGate refuses 'the pull request changes no files', state_presentation reads 'Nothing to change', and an empty PR the job published (Publish pr) is closed with a comment and the note 'Nothing to change: pull request #N changed no files, so it was closed.' A PR someone else opened is left open."
  - "Was true: rejected and stopped jobs' files said State \"new\". Is true now: they say \"rejected\" and \"stopped\", read back as new with that end on them."
  - "Was true: a resumed step chat showed '[carry on] <brief>' as the person's line. Is true now: session.ResumedStepBrief's lead is continuation context the display hides; the model still reads it."
  - "Was true: a reader's job showed an agentfield step's note as its node.reasoner. Is true now: it reads the step's label (api.PlainStepNotes, reader-side only)."
  - "Was true: a failed run's headline never named the failing command. Is true now: a short, plain command quoted as '`cmd` exited N' or '`cmd` failed' is named, '(cmd failed)', while the line fits FailureSummaryMost."
  - "Was true: in app mode the AgentField key and internal token and a pasted GitHub token were never written to disk (ErrAppKeys), so a desk whose keychain did not keep them lost them at every engine launch. Is true now: they are also written to the engine's own profile (its home's config.json, 0600); the app's copy still answers first, and Disconnect forgets both. gh's own token is still never written."
---
