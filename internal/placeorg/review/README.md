# Place review stage

`Stage` implements `placeorg.Reviewer` and `placeorg.Judged`. One
`RolePlaceReview` judgment reads the outline with three member examples per
place. Output is capped at 400 tokens and outline context at 24,000 runes.
The JSON shape matches the reference: `renames` maps current names to new
names, and `merge_suggestions` lists pairs of current names.

Renames reuse the naming stage's member spelling, word count, generic-name
and tree-wide uniqueness validator. Valid changes return `OpRename`.
Person-made places carry `Proposed: true`. Every merge returns `OpMerge` with
`Proposed: true`; unknown endpoints, self-merges and repeated pairs are ignored.
Neither stage mutates the supplied tree or corpus. Misplacement strings have
no structured chat identity in the reference, so they never become moves.

Missing, invalid or fallen-back judgment means no change. Cancellation returns
no partial operations. Only nodes shown in the bounded outline can be changed.
The shared import from `name` keeps spelling and validation rules identical.
