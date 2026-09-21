# The plan as a tree: the rail and the task page

The owner's ask (2026-09-18): "note we show nested dependencies properly,
especially in the task page, a list of ways to look at the various things
that are running; and in the right bar nested cleverly." This page is the
design; the cell that builds it is named at the end. Existing words only:
task, step, note, steer, rail, page, landing card, tab.

## The three laws

1. **One hierarchy, drawn by parent.** A row sits under the task that
   requested it, always. A dependency is a second kind of edge and is never
   allowed to move a row out of its family: a person reads "who asked for
   what" down the indentation, and "what holds what" off the words on the
   row. (This replaces the 2026-09-18 rule that drew a held row under the
   task it waits on — that rule made a child vanish from its parent's family
   the moment it waited on a cousin.)
2. **The rail follows the work.** A family with anything running or queued is
   open; a family that is entirely done or failed is folded to one line with
   its count. No key needed to see the live tree, one key (`enter`) to open
   anything folded.
3. **The page is the place to look at everything.** A task's page shows its
   whole subtree, not one level, with the same words as the rail plus the
   two things the rail has no room for: what this task waits on, and what is
   queued behind it. The root's page is therefore the whole run.

## The rail, four tasks running

```
tasks
▾ ▶ rewrite the auth flow                  $0.41
  ├ ✔ read the current flow · 6 steps
  ├ ▶ write the handler                    12 steps · $0.12
  │   $ go test ./internal/auth/...
  ├ ▶ write the middleware                 4 steps · $0.03
  │   $ cat > internal/auth/mw.go <<'EOF'
  ├ ○ write the tests                      queued · waits: write the handler
  └ ○ update the manual                    queued · waits: write the tests
▸ ✔ five chapter poem · 3 done             $0.14
▸ ✘ index the poems · 1 failed             $0.02
```

- State marks are the task-state slots of the vocabulary (`tokens.GlyphSet`),
  never literals; the shapes above stand in for them.
- `queued · waits: <title>` is unchanged. The title named is the nearest
  open task it waits on; a done dependency is not named.
- A folded family line carries `· N done` or `· N failed`; the root's own
  figures stay on its row. Selecting a folded family and pressing `enter`
  opens its page, never a second fold state.
- Depth beyond three levels draws with the same connectors; the rail does
  not truncate depth, it folds finished families.

## The page, the root of that run

```
rewrite the auth flow                      running · 2 running · 2 queued · $0.41
description
  Rewrite the auth flow: handler, middleware, tests, manual.
steps
  1 $ plandb split root --into '["read the current flow", …]'
under it
  ✔ read the current flow                  6 steps · $0.03
  ▶ write the handler                      12 steps · $0.12 · 2 queued behind it
      $ go test ./internal/auth/...
  ▶ write the middleware                   4 steps · $0.03
      $ cat > internal/auth/mw.go <<'EOF'
  ○ write the tests                        queued · waits: write the handler
    └ ○ write the fixtures                 queued · waits: write the tests
  ○ update the manual                      queued · waits: write the tests
notes
  › a note reaches the worker at its next step
```

- `under it` is the whole subtree in store order, indented with the rail's
  own connectors, each row wearing the rail's own words and figures, the live
  `$ <command>` under a running row.
- `· N queued behind it` appears on a row that other open tasks wait on
  (direct dependents only, counted honestly). It is the one glance answer to
  "what is everything waiting for". Nothing is drawn when N is zero.
- The page header's figures: `N running · N queued` over the subtree; both
  drop when zero (the emptiness law).
- `enter` on a row under `under it` opens that task's page; `esc` returns to
  the page you came from, not to the list. The breadcrumb line already reads
  `esc/← main`; on a child's page it reads `esc/← <parent title>`.
- The note composer and its receipt are unchanged and stay at the bottom.

## A child's page

```
write the tests                            queued · waits: write the handler
description
  …
waits
  write the tests · waits: write the handler       ▶ 12 steps
  write the fixtures · waits: write the tests      ○ queued
under it
  ○ write the fixtures                     queued · waits: write the tests
notes
```

- `waits` lists both directions in one section with the row's own sentence
  shape: this task's own waits first, then the tasks that wait on this one.
  The section is absent when both are empty.

## What the surface needs from the store that is not there

- `PlanTaskPage.Children` is one level (session/plandb_tasks.go, added
  2026-09-18); the page needs the subtree with a depth per row, in store
  order. `PlanTaskRow.Waits` (hard dependencies) exists; the reverse count
  is computed from the rows on the page, no store change.
- Nothing new in `internal/plandb`.

## The cell

One M cell in `internal/tui3` and `internal/session/plandb_tasks.go`,
failing tests first: (1) rail folds a finished family to one line and keeps
a live family open; nesting is by parent only, a cross-family wait stays in
its family and wears `waits:`; (2) the page's `under it` is the full subtree
with live lines and `· N queued behind it`; (3) `enter` on a subtree row
opens that page and `esc` returns to the parent's page; (4) the `waits`
section on a child's page, and the manual sections in the asker's words.
