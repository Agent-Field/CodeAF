# Desktop explorer of the current data model

2026-09-09. The person asked to interact with the accumulated graph, then explicitly
corrected scope: desktop only, open it in the browser, and show what today's data
structures can support rather than proposed memory or decision auditing.

The resulting [prototype source](prototypes/current-model-explorer.html) is a
read-only viewer over 5,000 deterministic synthetic records and 40 collections
(eight top-level groups, four nested folders each). It does not read the person's
real aforge home. The new viewer is not itself an implemented aforge screen.
The browser preview was opened locally at
`http://127.0.0.1:8769/trace-explorer-preview.html`.

## What the viewer actually represents

| Visible thing | Existing structure / owner |
| --- | --- |
| Folders and membership | `workspace.Collection`, `workspace.Ref`, collection memberships |
| Conversation, task, ongoing-item and file facts | `workspace.ResolvedRef`, `workspaceview` owner reads |
| Task identity and owning chat | `Ref.Kind=task`, `Ref.ID`, `Ref.SessionID` |
| Shared context, source, targets, revisions, withdrawal | `workspace.ContextRecord`, `ContextAt`, paged history |
| Ongoing work's original request, origin, grant, wake/action, limits and latest activity | `standing.Item`, `Origin`, `When`, `Action`, `Rails`, retained notes |
| Latest ongoing run location | `standing.Item.LastRun`; represented as an existing artifact/path reference |

Inspected sources on the backend review branch:
`internal/workspace/workspace.go`, `context.go`, `projection.go`, `store.go`;
`internal/workspaceview/workspaceview.go`; `internal/standing/standing.go`; and
`docs/design/workspace-foundation/BACKEND.md` / `DESIGN-STATUS.md`.
Shared context is part of the unmerged backend draft #662, not a claim about a
released product. This document does not extend backend scope.

## Three inspectable examples

1. Shared context: select the source conversation and explicit targets; open an
   immutable prior revision and see its own wording, source and target set.
   Changing the displayed revision does not revise the retained record. A source
   reference does not promise an exact message anchor, acceptance, permission or
   evidence that a particular execution consumed it.
2. Ongoing item: inspect original words, origin conversation, grant, wake condition,
   action, rails, latest check and retained run notes/location. Quiet checks
   overwrite latest-check fields; this is not a complete record of every check.
3. One file in two places: Engineering / Production and Growth / Launches both
   reach the exact same absolute-path reference. Missing paths are shown as
   unavailable; membership does not prove the file still exists.

Folders and topic views derive their lists and counts from stored membership
references plus separately labeled explicit context targets, deduplicating record
identity. Descendant aggregation is a display operation, not inheritance of
context applicability. A record can be drawn at its selected folder placement
without creating a new underlying record. Global record count is distinct from
the sum of overlapping folder counts.

## Explicitly excluded

The earlier draft's inferred-memory nodes, accepted-decision authority, per-run
context-consumption receipts, precise generic source-message anchors and adoption
of later direction are not represented as existing data. There is no inferred
causal edge. The broader graph drawing remains a conceptual model, not proof that
all its relations are stored today. The current task index is not an all-time
census; some old runs are reaped; unavailable or never-recorded information cannot
be reconstructed by adding a graph interface.

## Validation and continuation

The generated script parses and the desktop preview loads without script errors.
Native Chrome checks exercised folder/topic zoom, the shared file reached through
Growth / Launches with its original identity, context revision/source/target
changes, and the ongoing item's real field shapes and activity view. An independent
finish review caught list derivation from layout coordinates; that was corrected
to use memberships and explicit targets. Follow-up source review marked the
semantic finding resolved, disposition `ship`. No mobile work remains in scope.

The source snapshot above matches the conversation's editable fragment at
`/Users/santoshkumar/.codex/visualizations/2026/09/09/01a08653-1fcf-7880-b64f-dae46f29b86a/trace-explorer.html`.
When revising, update both copies together and regenerate the standalone preview.
The preview uses the visualization skill's renderer; its hosting server is local
and does not expose or mutate the aforge backend.

Next: use this viewer to discuss whether the current observable records answer
the person's questions. Record specific missing evidence separately from visual
layout changes. No new primitive, audit contract or implementation task is approved
by this prototype.
