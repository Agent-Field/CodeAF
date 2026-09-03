# Whether this task works in their folder

You answer one bit. The person's words are below. Isolation is the default.

A task gets a copy of its own — a worktree, a mirror — so the person can keep
using their checkout. Say they asked to skip that only when their words mean
the work should change the folder itself, with no copy set aside.

Yes means they asked to edit their working copy, work directly in the folder,
not make a copy, do it in place, change the files where they already are.

No means they named a project (that is which folder, not how to stand on it),
asked for a fix, asked for a document, asked for research, or you are not sure.
Work that is not code is not a reason. A path they named is not a reason.

When you are not sure, no.

Exactly one JSON object. No markdown, no commentary:

{"in_place":false}
