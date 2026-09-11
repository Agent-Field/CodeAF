---
kind: fixed
title: A conversation's anchor writes a whole identity, so an older snapshot cannot undo it
pr: 903
surface: [chat, engine]
invalidates:
  - "`/workspace` and the model's `workspace` tool wrote meta.json by reading it, replacing two fields and saving what came back. Since #876 deferred the opening message's stamp, that read can be of a file that does not exist yet — and the anchor then saved an identity-less meta.json, which [LoadMeta] answers with a blank Meta exactly as it answers a missing file. The anchoring was on disk in a record nothing could read. The anchor now fills the identity whole when its read came back blank."
  - "A metadata transaction carrying a snapshot taken BEFORE an anchor then met that identity-less file, took [Agent.updateMeta]'s seeding branch and wrote the snapshot's ownership and workspace back over the anchoring: a conversation the person had pointed at a project came back project-less. #695's contract is unchanged and holds; what was wrong was the door that left a file the transaction had to rebuild."
  - "An anchor over a conversation that ALREADY has an identity still writes exactly the two fields it owns — the workspace and the ownership — and nothing else. The totals, the name, the model and the folders another window wrote are left where they are, which is what the folder's lock is for (#695)."
---

dev went red on the push of #876 with one test in `internal/session`:
`TestOldNamingAndSpendSnapshotsPreserveAnAnchoredWorkspace`, which is #695's law
that an old snapshot never undoes a workspace anchored after it was taken. The
failure is scheduling-sensitive rather than environment-sensitive — the deferred
stamp loses on a loaded runner a race it wins on an idle laptop, and `-cpu=1`
reproduces it every time — so what changed was not the contract but which writer
reaches meta.json first. #876 moved WHEN the stamp lands on the disk; it did not
change what a metadata transaction owes.
