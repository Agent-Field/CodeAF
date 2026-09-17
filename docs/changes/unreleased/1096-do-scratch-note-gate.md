---
kind: fixed
title: a job root working in the person's directory is offered its overflow file outside that tree
pr: 1096
surface: [engine, resident]
invalidates:
  - "`leafOutputHint` offered a job root the workspace-relative `exec.SuggestPathFor` address while pointing every intermediate leaf at the run's scratch. Under `codeaf do`, whose workspace is `OwnedByPerson`, a root that took the offer left an untracked `task-<n>-<slug>.md` at the root of the person's tree; `Workspace.RecordChanges` observed it, `jobArtifacts` joined it into the record the delivery gate is held to, and the gate refused a delivery whose work was complete and committed, ending the run incomplete. A personal root now resolves that address through `Workspace.ScratchPath`, the same place an intermediate leaf writes, and the directory-already-there withdrawal is measured at the scratch spelling."
  - "Nothing is exempted by filename. `Workspace.record` still drops only paths outside the root, so a file that turns up in the person's tree while a leaf runs joins the record whatever it is named, including a name wearing the run's own `task-<n>-<slug>.md` shape."
---

A job root's deliverable is its final message; the file was only ever the second
copy for overflow a message cannot carry, and a second copy filed in the tree
the delivery is judged in is an untracked file the person's own tools then have
to explain.
