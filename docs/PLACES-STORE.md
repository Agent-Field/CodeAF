# Places store plans

`internal/placegraph` owns the graph, memberships, receipts and Undo. It does
not call models or organise chats. `Store.ApplyPlan([]PlanOp)` is the store door
for a proposed organising run.

## One run, one commit

Creates use caller-bound stable place ids. Later operations may refer to those
ids. Plans support create, rename, reparent, merge, delete, file, move and link.
Deletes remove places and membership rows only; chat transcripts are never
removed. Merge uses the same implementation as `Store.MergePlaces`.

Every operation runs on a copy under one store lock. The complete graph is
validated before one atomic file replacement, one revision increase and one
receipt (`places.plan`). An invalid operation, final cycle, sibling name clash,
or size breach writes nothing and records no receipt. An empty or unchanged
plan writes nothing. Operation errors identify their position, counted from one.

Plan Undo restores the exact prior file bytes, including revision, visits and
open-place records; a plan applied to a missing file restores its absence.
It checks the committed bytes as well as the receipt revision, so any later
write (including a visit) refuses Undo. Ordinary single-action Undo keeps its
existing increasing revision and preserves current visits. Byte-exact plan Undo
is the owner algorithm's exception to that ordinary contract.

## Homes and shortcuts

`Membership.Link` is `json:"link,omitempty"`. False means a home; true means
an additional shortcut. Older rows without the field remain false and loading
an old document does not rewrite it. `ChatsIn` includes shortcuts. A link to an
existing home never demotes it; filing or moving to a shortcut promotes it to a
home. The organiser owns the one-home and two-shortcut constraints; the store
retains its existing general membership limits.

## Plan proposals

`ProposalPlan` persists its ordered `Ops` in the existing recommendation ledger.
`Recommender.Accept` checks the offered revision under the write lock, then
applies the selected operations through the same atomic plan door. `AcceptEdit.Skip`
contains zero-based operation indexes; replacement names, chats and operations
cannot widen the plan. Skipping an operation that another needs refuses the plan.
All plan-created ids are tracked in `AIPlaces`; surviving creations must fit the
existing AI caps. A person's places never count against those caps.

This library change does not expose an Organize now button or change the
existing bridge scheduler. Those belong to the bridge and desktop port lanes.

## Organizing policy

The policy table adds `organizeMode` (`add-on`, or `refresh`),
`organizeCallsPerRun` (150), `organizeUSDPerRun` (5 whole US cents = $0.05),
and `liveGuess` (on). These rows describe limits for the organiser; this store
lane does not make model calls or enable the draft suggestion surface.
