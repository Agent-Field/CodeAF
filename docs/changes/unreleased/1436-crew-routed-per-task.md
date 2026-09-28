---
kind: changed
title: a task's crew is routed per task, and /crew is the panel of what is allowed and pinned
pr: 1436
surface: [engine, chat, docs]
invalidates:
  - "The crew was a preset word — `frugal`, `balanced`, `max` — that wrote all five tier rows at once, with a family row (`models.crew.source`: `open` or `all`) and a pick row (`models.crew.pick`: `table`, `catalog`, `learn`) beside it, and a tier row could say `auto`. None of those words exists now: every seat nobody pinned is picked per task by internal/crewroute. `/crew <preset>` is refused with the four forms the command does take."
  - "The preset tables, the catalog picker package, `config/auto.go` and its catalog, index and own-cells seams, and the three shipped crew-seat default models are gone. The router's catalog seam is `config.CrewCatalog`; the Model Pool's index is no longer a crew picker."
  - "The check seat inherited the plan seat's model when only the planner was named by flag or environment. It never does: `--check-model`, `CODEAF_CHECK_MODEL`, a pin, or the router's checker."
  - "A seat nothing pinned fell to a model this build chose for everybody. It is routed; a seat nothing allowed can sit is an error that says so."
  - "A worker row a profile never wrote was read as inherited from the small-work row, with a one-time notice. There is no inheritance and no notice: an unwritten crew row is auto."
  - "The onboarding controls screen asked for a crew. It asks for the daily limit and the chat model; the crew asks nothing up front."
  - "The settings rows for the working, careful and planning tiers are one `seats` row that opens the `/crew` panel. The `crew`, `model family` and `picked from` rows are gone."
  - "`codeaf do`'s `model_source`/`plan_model_source` read `crew <preset>` or `default`. They read `--model`, the variable, `pinned` or `routed`, and `-json` also carries `class`, `crew`, `est_usd`, `check_model` and `check_model_source`."
  - "A task carried no dollar limit of its own, and the Spending tab's `per task` row read `no limit of its own`. A task is held to the per-task limit set in `/crew` ($5 by default), and the row reads that figure."
  - "On an OpenRouter balance read as low, the five tier rows nobody set read a hand-picked free crew and `/status` called that crew `free`. Only reflex and small work read free models now; the three crew seats see the OpenRouter account as out of credit before any call and are routed to free pools, and the crew line says `free routes in use (may log prompts) · credit unavailable on openrouter`."
  - "Remote protocol version 17 is replaced by 18: `Task.Start` carries the one-task effort word and `Task.RedoStronger` runs a task again on a stronger crew. An older engine refuses at the handshake rather than starting the task on the crew the person asked it not to use."
---
A task's crew — the worker that does the work, the planner that structures it
and the checker that reads the result — is picked for that task. The router
classifies the task as a bugfix, a complex fix (a bugfix whose report shows
reach), open-ended work or other, prices every
allowed model on every connected route (a subscription plan or a local model
costs nothing to route to), and sits each seat where quality minus λ times cost
is highest, λ at the knee of the curve. `--best` and `--cheap` (on `/task`, on
the conversation's hand-off as `effort`, and on `codeaf do`) move λ for one task
only. The design behind the defaults is in
`docs/design/model-pool/pareto-crewing.pdf`.

**Every model in the catalog is scored from its catalog row.** Fitted weights
shipped in `internal/crewroute/prior.json` turn a row into a quality per seat
and class with its variance. A row that publishes any index (the AA indexes or
arena Elo) is scored on its indexes alone, so a model no dearer and at least as
good on every shared index never ranks below another; a row with none is
scored from context, release date (the catalog's `created` field, now read),
licence and family, never above the population mean. Price is never read as
ability. Seats weigh a model at its score less one standard deviation of
ability; a worker's and a checker's mean ability must reach the floor when any
allowed model's does. On open-ended and other work at the knee a support
upgrade goes to the checker, the planner staying on the base model; `--cheap`
takes a stronger worker within 1.5× the cheapest one's cost. A model whose row is too thin for a finite score is not picked unless
pinned. Reach is read over a one-paragraph ask's whole text, a security fix is
a reach signal, and "wrongly", "rejected", "instead of" and a rename or version
bump read as fixes. No per-model table ships, and
estimates come from catalog prices times each seat's token profile times the
install's own cost factor. Each task's outcome — accepted, kept, redone,
failed — moves that model's score in that seat by a small bounded step,
recorded on the decision row.

**What persists is what the panel says.** `/crew` opens an interactive panel:
the three seats (`auto · usually <model>` or a pin), the allowed-models rule
stepped in place (`all`, `open`, a price ceiling typed into two boxes, or a
custom checklist of providers and models), the daily cap typed in place, and a
line for today's spend. A seat's list starts with auto and the suggested model,
opens a model's routes, and offers to widen the rule when the pick is outside
it. Every change saves at once, ticks its row and can be undone with `z` for a
few seconds. `/crew pin <seat> <model[@provider]>`, `/crew unpin <seat|all>`,
`/crew models <rule>` and `/crew cap <dollars|off>` write the same state and
open the panel on the changed row. In `/settings` → Providers the three seat
rows are one `seats` row that opens the panel. A pin outside the allowed models is refused. At the cap a chat task
does not start and `codeaf do` refuses unless given `-yes-spend`.

**No task may cost more than its limit**, $5 unless set on the panel's cap row
(`per task $5 · daily none`, `tab` between the two) or with `/crew cap task <dollars>`.
Every priced call of the task — each seat and the helpers made for it — is held
to one tally before it is made; the call that would pass the limit is not made
and the task stops on `this task reached its $5 limit · raise it in /crew`.
`-yes-spend` does not lift it, and `codeaf do` holds every run to it.

**Redo is how a crew learns.** `/redo stronger` runs the last task again with
every unpinned seat one step stronger, and the router's log records that this
class of work in this repository was under-served, so the next such task starts
a step higher, at most three steps; each accepted task of that kind takes a
step back off. Nothing escalates on
its own.

**Old profiles migrate once**, with one line: preset, pick and `auto` rows
become auto, the ids a person wrote stay pinned, and the `open` family becomes
the allowed rule `open`.
