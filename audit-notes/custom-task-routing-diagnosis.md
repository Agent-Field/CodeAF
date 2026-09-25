# Custom/local provider task routing — read-only diagnosis (`santos/dev2`)

State analyzed: branch `task/trace-custom-task-routing-3f6752` @ `6d97ef781` (clean copy of `santos/dev2`). Read-only; nothing edited, committed, or pushed.

## Verdict

Discovery is the routing defect; it is the only one that breaks getting work ONTO a
custom/local provider. Once a qualified id (`<written>/<bare>`, e.g. `mybox/llama`)
reaches a node, every downstream seam already carries the custom account correctly.
A second, narrower defect loses vision capability for custom models because the
capability closures read only the default-service catalog.

## Working seams (verified end to end)

1. `propose_task{model}` → `parseTaskArguments` → `spec.modelWord` → `Agent.resolveTaskModel`
   (`internal/session/task.go:647`, resolver `internal/session/taskmodel.go:96`).
2. Node model → `newTaskAgentOn` (`internal/session/task_run.go:7519`): child Config carries
   `Sources: parent.Sources`, `APIKey`, `BaseURL`, `SupportsImages`, `SupportsParameter`,
   `ReasoningProfile`, `ContextWindowFor` (catalog func), Connect/Media/Search/Document,
   `RolesSource`, `OneModel`.
3. Child client: `newChildAgent` (`internal/session/agent.go:87`) → `childClient`
   (`internal/session/clientdoor.go:260`) → the PARENT's `modelClientPool.clientFor(model)`
   (`clientdoor.go:94`): `seatedModel` → `accountFor` → `Config.serviceFor` (`clientdoor.go:631`)
   → `modelsource.Set.For` (`internal/modelsource/modelsource.go:210`, `Split` at :379 matches the
   `Written` prefix) → minted `provider.Client` for THAT service; the child shares the pool
   (`child.clientPool = pool`), so live `setSources`/`setSeat`/`setDefaultKey` stay coherent
   family-wide.
4. Wire: `ClientConfigFor` (`internal/config/config.go:912`) strips the service segment —
   bare slug `llama` in `Model`, `BaseURL`/`APIKey` from the custom `Connected`, and
   `Direct: true` for any non-default service. Requests go out via `completerFor` →
   `completeWithNamedModel` with `ai.WithModel(wire)` (`clientdoor.go:638`, :536).
5. Tool seat: `seatTaskModelLocked` (`task_run.go:7890`) asks `SupportsParameter(model,"tools")`;
   `Catalog.SupportsParameter` (`internal/catalog/catalog.go:857`) answers `(false, false)` =
   UNKNOWN for an id absent from the catalog — a custom model is NOT falsely rescued to the
   worker tier.
6. Toolbelt: assembled in `newAgent` from the copied Config fields; other child sites
   (`orchestrate.go:1103`, `task_audit.go:3183`) also copy `Sources`. Quick tasks and divide
   parts are built by the same `newTaskAgent`.
7. Mid-session connect: `v3Process.setModelSources` (`cmd/codeaf/chatv3_process.go:316`)
   updates the shelf AND every retained agent (`Agent.SetSources`, `agent.go:715`) → pool.

## Defect 1 — task-model discovery sees only the default catalog

`session.Config.TaskModels` is wired once (`cmd/codeaf/chatv3.go:995`) to `v3TaskModels`
(`chatv3.go:2164`) = `tui3.ChatModels(v3Models(shelf))` — the shelf's LAUNCH (default/OpenRouter)
catalog only (`v3Models` → `ModelsNow()`). The per-service compartments that feed the chat
picker (`v3ModelShelf.modelsForService`, `chatv3_modelshelf.go:83`; merged in
`internal/tui3/modelservices.go:76-112` and qualified with `Connected.Qualify` →
`written/bare`) are never merged in.

Observable behavior, warm default catalog (the normal steady state):

- `propose_task{model:"mybox/llama"}` → `matchTaskModel` finds no row → `taskModelUnknown`
  refusal naming default-catalog "near" ids. Same refusal on quick `task`
  (`internal/session/task_quick.go:599`) and task retarget (`internal/session/task_room.go:720`).
- One call is wasted; a retry naming a default-catalog word then silently runs the work on the
  default service — the wrong-service outcome the refusal was guarding against.
- Asymmetry: a `task.model` settings row naming `mybox/llama` does NOT refuse
  (`defaultTaskModel` returns the row as written when it matches nothing), routes correctly —
  while the same id as an explicit argument is refused. The matcher is the only gate.
- With `TaskModels` nil or the catalog still warming, the word travels as written and routes
  CORRECTLY through `Sources.For` — the empty list works better than the warm one.

## Defect 2 — capability closures are blind to custom services (the "tools" loss)

`SupportsImages` = `v3SeesImages(proc.Shelf)` (`chatv3.go:2238`) reads only `ModelsNow()`
plus the default service's disk cache; the comment states an absent id is "a no". So a
custom/local model that publishes image input gets `image.go:113` refusals and
`tools_doc.go:595` vision rung off — in the conversation and in every child. The belt is
fine; the gate lies. Same shape, degraded-but-safe: `ContextWindowFor` answers 0 for custom
ids (`internal/session/loop.go:4329` → conservative window, early compaction),
`ModelPrice` 0 (no latency price ceiling), `ReasoningProfile` unknown (adapter learns by
being told no), `SupportsParameter` unknown (optional knobs not sent — safe direction).

## Smallest changes (not implemented)

1. `v3TaskModels` (`cmd/codeaf/chatv3.go:2164`): also append, for each non-default service in
   `settings.Sources.All()[1:]`, `proc.Shelf.modelsForService(service)` rows qualified with
   `service.Qualify(row.ID)`, then the same `ChatModels` filter. No network: compartments are
   never-fetching and seeded by `setSources`. `settings.Sources` is already in scope at the
   Config literal (`chatv3.go:925`).
2. `v3SeesImages` (`chatv3.go:2238`): consult the same compartments for qualified ids before
   answering no. (Capability fix; not needed for routing.)
3. Optional, one line in the task child literal: `TaskModels: parent.TaskModels` so a node's
   own sub-proposals validate against the same list instead of the nil = take-as-written
   fallback.

## Regression assertions

1. Resolver (session): `TaskModels` = defaults + `mybox/llama` → `resolveTaskModel("mybox/llama").model`
   is `mybox/llama` (exact rung); with defaults carrying `other/llama-x` and the custom row,
   `resolveTaskModel("llama")` returns exactly `mybox/llama` (tail rung semantics).
2. Admission: `stageTask` on a proposal naming `mybox/llama` is admitted, not
   `bare.Settled(problem)`; quick-task ask and `tasks` retarget accept the same id
   (both go through the one resolver).
3. Seat: with `SupportsParameter("mybox/llama","tools")` answering `(false,false)`,
   `seatTaskModelLocked` returns the model unchanged, writes no `taskModelRescueNote`, leaves
   `node.ran` untouched.
4. End-to-end child routing (the load-bearing one): managed parent with `Sources` = default
   (scripted completer) + custom `mybox` service; `newTaskAgent` for a node on `mybox/llama`;
   capture the child's first request and assert: `clientAccount.id` = the custom row's id,
   request `BaseURL` = the custom address, bearer = the custom key, wire model = `llama`
   (no `mybox/` prefix), and `Direct` client in use. `retiredpin_wire_test.go` /
   `task_carry_test.go` show the seams to read.
5. Surface merge (cmd/codeaf test): shelf whose launch catalog holds `anthropic/claude-x`
   and whose `mybox` compartment holds `llama` → `v3TaskModels()` contains BOTH
   `anthropic/claude-x` and `mybox/llama`.
6. Vocabulary law: `Sources.For` falls back to the DEFAULT service with the whole id as wire
   slug when the segment is not a connected `Written` (modelsource.go:210-217) — pin it, so a
   typo'd segment stays a provider-side 404 rather than silent discovery drift, and so the
   argument vocabulary stays `Written`-qualified exactly as the picker shows.
7. Settings-row asymmetry: `defaultTaskModel` with a `task.model` row `mybox/llama` returns it
   as written (already true — pin so the fix doesn't change it).

## Not covered

- No edits, commits, pushes, or PRs (brief is read-only). The claude CLI was not run, so the
  `CLAUDE_CODE_OAUTH_TOKEN` standing order did not come into play.
- Verified by reading and cross-referencing, not by running a live custom-provider session.