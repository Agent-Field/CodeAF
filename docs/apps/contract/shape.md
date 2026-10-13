# Apps · wave 2 · track shape · contract

Owner boards: `docs/apps/spec/10-shape-in-detail.*` (all of it) and the Versions
card of `12-app-settings-lifecycle.*`. The boards win over this file. Words,
copy and layout come from the boards exactly.

Scope: Shape mode (plan menu · running draft · Builder · draft bar), Show diff,
Apply with migration preview, Undo, replay on a newer version and the replay
question, failed change (Fix it · Cancel this change), "This change broke X ·
Fix it · Revert", personal apps (apply on pass + Undo toast), a User asking an
Editor ("Only editors can shape X · Ask @mona"), the app agent's Shape this
handover, and Versions… (list · Show diff · Restore).

Not here: Members/roles (spaces), Activity/audit storage (spaces), App ⌄ menu
mechanics (crews), connectors beyond fakes (connect), custom/*.tsx writing
(crews), surfaces.

Branch: `apps/w2-shape` in CodeAF and codeaf-desk. Rulings R1–R7 in the wave-2
brief apply: new code in new per-track files, one registration line per
shared wave-1 file.

## 1 · Data on disk

Under `$CODEAF_HOME/apps/<app>/`:

- `drafts/<member>/draft.json` — the draft: base version, state, the ordered
  list of changes. Each change stores its **request text and its diff**, so it
  can be replayed (board 10, "stored as requests + diff").
- `drafts/<member>/files/` — the draft's definition files (base version files
  with every passed change applied). Never the definition repo.
- `drafts/<member>/overlay.db` — the scratch copy of records. Made by copying
  `records.db` when the draft opens (and again on replay while the draft has no
  scratch writes). Draft-mode reads and writes go here; `records.db` is never
  written from a draft (rule 04).
- Versions: the store's definition repo and its `vN` tags (wave-1 B1). A
  version's metadata (request texts, who, when, change count, migration) is in
  `versions.json` next to `definition/`, written only by `internal/apps/shape`.

A draft survives restarts; it ends on Apply or Discard (folder removed).

## 2 · Draft data mode (seam other tracks consume, R6)

`internal/apps/svc/svc_shape.go`:

```go
const DataDraft DataMode = "draft" // Shape draft: reads + writes drafts/<member>/overlay.db
```

The gateway sets `Scope.Data = DataDraft` when the envelope's `app` is
`<id>@draft` (the draft of the calling member). Caps checks the bare id.
Desk: the Shape centre's runtime uses `app: "<id>@draft"` for every call.

Who honours it:

- Records (hook, R4, owned by shape): `records/overlay_shape.go` resolves the
  database path for `DataDraft` to `drafts/<member>/overlay.db`; one call line
  in the wave-1 path resolver.
- Runs and Connectors (connect owns the connector door): under `DataDraft`
  every connector call goes to the fake. Nothing real is sent before Apply.
- Agent: the app agent is not available in draft mode (`denied`).

## 3 · Bridge methods (`internal/apps/bridge/proto_shape.go`, routed from `gateway/shape.go`)

All app-scoped. `Editor` = caller's app role must be editor (Personal: always).
Everyone who can open the app may call the read methods marked `any`.

| Method | Who | params → result |
| --- | --- | --- |
| `shape.open` | Editor | `{}` → `Draft` (creates the caller's draft on the current version, or returns the open one) |
| `shape.get` | Editor | `{}` → `{draft: Draft \| null}` |
| `shape.request` | Editor | `{text, from?: "user"\|"plan"\|"agent"\|"fix"\|"broken"}` → `Change` (state `checking`; progress on the shape topic) |
| `shape.plan.edit` | Editor | `{section, id, text}` → `Change` (an in-place plan-menu edit is a change) |
| `shape.change.fix` | Editor | `{change, hint}` → `Change` (3 more rounds, request becomes "Fix: …") |
| `shape.change.cancel` | Editor | `{change}` → `Draft` (removes that change only) |
| `shape.answer` | Editor | `{change, option?, text?}` → `Change` (replay question; the answer replaces the request text) |
| `shape.diff` | Editor | `{change?}` → `{files: FileDiff[]}` (one change, or the whole draft vs base) |
| `shape.preview` | Editor | `{}` → `ApplyPreview` (the Apply sheet; migration counted on all real records) |
| `shape.apply` | Editor | `{base}` → `AppSummary` (`conflict` if `base` ≠ current version: replay first) |
| `shape.discard` | Editor | `{}` → `{}` (draft folder gone, nothing else changes) |
| `shape.replay.dismiss` | Editor | `{}` → `Draft` (hides the green replay line) |
| `shape.ask` | User/Viewer | `{text}` → `{to}` (sends the request to the Editor who applied last; lands in their Inbox as "X asks for a change to CRM"; `to` is that member) |
| `versions.list` | any | `{}` → `{versions: Version[]}` newest first |
| `versions.diff` | any | `{version}` → `{files: FileDiff[]}` (that version vs the one before) |
| `versions.restore` | Editor | `{version, confirm?}` → `AppSummary` (writes vN+1 equal to `version`; if fields with values would vanish and `confirm` is not true → `conflict` with `details.migration: MigrationLine[]`) |
| `apps.health` | any | `{}` → `AppHealth` |

Undo = `versions.restore` of the version before. Revert (broken app) = restore
of `AppHealth.broken.last_good`. Fix it (broken) = `shape.open` then
`shape.request {from: "broken", text: "Fix: <error line>"}`.

Personal apps (space `personal`): `shape.request` applies on pass — the change
becomes the next version at once and `ShapeEvent{kind: applied}` carries it
(board 10 "Shape a personal app": no draft bar, Undo toast). Migration still
asks first: a rename/remove change stops at state `ready` with the preview.

## 4 · Types (Go `proto_shape.go` ⇄ TS `src/lib/types/apps-shape.ts`, identical JSON)

```
Draft        {app, member, base, state, changes: Change[], replayed?: Replay, updated_at}
  state      open | checking | ready | question | applied | discarded
Change       {id, n, request, from, state, round, steps: ShapeStep[], files: string[],
              summary?, check?: CheckReport, question?: Question, error?, at}
  state      checking | passed | failed | question | cancelled
ShapeStep    {step: plan|write|render|behaviour, status: running|done|failed, log}
Question     {text, options: [{id, label, sub?}]}            // + "drop" option always last
Replay       {from, to, by, requests: int, failed: int, dismissed: bool}
FileDiff     {path, op: add|modify|delete, before, after}     // full texts; desk renders the diff
ApplyPreview {from, to, changes: [{n, request}], audience, needs_changed: bool,
              needs: NeedLine[], migration: MigrationLine[], records_checked: int}
  audience   "everyone in acme gets v13" | "only you"
MigrationLine{op: add|rename|remove|retype, type, field, to?, affected: int,
              line, note}          // "Rename “Value” to “Amount” · 312 deals", "nothing deleted · old name kept as alias"
Version      {n, requests: string[], by, at, changes: int, note?, current: bool, restorable: bool}
AppHealth    {version, broken?: Broken}
Broken       {version, request, error, file, line?, affected: int, last_good}
ShapeEvent   {kind: draft|change|step|question|replayed|applied|discarded, draft?, change?, version?, at}
VersionEvent {version, by, request, at}
```

Topics: `app/<id>/shape/<member>` → `ShapeEvent` (only that member subscribes;
caps enforces) · `app/<id>/version` → `VersionEvent` (every member; desk swaps
screens in place, keeps selection and scroll) · `app/<id>/health` → `AppHealth`.

Error codes: wave-1 set. `conflict` for a moved base and for a migration that
needs `confirm`; `denied` for a non-Editor; `check_failed` when a change ends
failed after its rounds.

## 5 · Server behaviour

- **Change pipeline.** request → `svc.Shaper.Shape` (Builder) with the draft
  files, kit, check reports and sample records only (rule 01) → new files →
  `svc.Check.Run` (schema · kit law · render on sample · behaviour on fakes) →
  pass: files join the draft, change `passed`, draft `ready` when no change is
  checking; fail: back to the Shaper with the report, max 3 rounds, then
  `failed` (Fix it · Cancel this change). A failed change never reaches Apply.
- **Migration.** Schema diff of `app.json` records base → draft. Rename keeps
  the old name as an alias (`value:` filters and runs keep working); remove
  hides the field (values stay stored, Restore brings it back); add sets
  defaults lazily. Counts are real (all records, not sample). Nothing is ever
  deleted on Apply.
- **Apply.** One step: dry-run the migration on a copy → merge draft files into
  the definition repo → tag vN+1 → run the migration on `records.db` → write
  `versions.json` row → publish `VersionEvent` → write the Activity line through
  spaces' Activity writer ("Mona shaped CRM · v13 · View · Undo"; until spaces
  lands it, a hub event on `app/<id>/activity`) → replay every other open draft.
- **Replay.** Another member applied: each stored request goes to the Shaper
  again on the new version, in order, each checked. All pass → `Replay` set,
  draft `ready`. A request that no longer fits → that change `question`, draft
  `question` (amber, Apply off), an Inbox line "CRM · Builder needs you".
- **Broken after Apply.** After Apply the server renders every screen on real
  records (read-only, Check's renderer). Failure → `AppHealth.broken` with
  the error, file:line and affected count; the app serves the last good
  version read-only for everyone until Fix it or Revert; each Editor gets one
  Inbox line.
- **Role.** Methods marked Editor deny others with `denied` (spaces' role
  seam; Personal = Owner = Editor until it lands).

## 6 · Go seams (`internal/apps/svc/svc_shape.go`)

```go
type Shape interface { /* one method per bridge method in §3 */ }
type Shaper interface { // implemented in builder/shape.go (new file in B5's package)
    Shape(ctx context.Context, in ShapeInput) (ShapeOutput, error)
}
type ShapeInput  struct { App string; Files spec.Files; Request string; Report *bridge.CheckReport; Round int; Sample map[string][]bridge.Record }
type ShapeOutput struct { Files spec.Files; Summary string; Question *bridge.ShapeQuestion; Cost float64 }
```

Registration lines (R1): `svc.Services` gets `Shape Shape` and `Shaper Shaper`;
`server.go` gets `all.Shape = shape.New(cfg, all)` and
`all.Shaper = builder.NewShaper(cfg, all)`. Each method in `gateway/shape.go`
registers from its own `init()` with `gateway.Register(name, Method{Handle:
handler, Allow: allow})`; its Allow enforces the role in §3 and admits the
app agent only for `apps.health`. There are no track wrappers around
`gateway.call` or `caps.Allow`, and duplicate method names panic at startup.
Gateway forwards Allows to the caps registry to avoid an import cycle.

Shape registers its private topic prefix `app/*/shape/` with
`caps.RegisterTopic` and an Allow that admits only the calling member, plus
version and health topic permissions. Client app wildcards are denied so they
cannot expose another member's draft. `caps/shape.go` registers one deny-only
rule with `caps.AddDenyRule`: `agent.send` cannot run under `DataDraft`.
Generic checks run before deny rules; deny rules run before method/topic
Allows, and audit still appends once per call.

Draft scope is a core gateway seam: `gateway/scope_draft.go` strips `@draft`
before resolving the app caller and sets `Scope.Data = DataDraft` before
caps; resolving a kept app preserves that data mode. Only spaces sets
`gateway.SetCallerResolver` from its `init()`; the default is the Personal
owner/editor, and a second Set panics. The resolver receives the upgrade
request once for socket membership (app empty), and once per app-scoped call
or subscription for the bare app id. Socket cleanup uses
`gateway.OnDisconnect(func(svc.Caller))`: hooks receive the socket caller in
registration order, and a panicking hook is logged without stopping others.

## 7 · Ownership

| Lane | Owns (new files unless noted) |
| --- | --- |
| c0-bin | `bridge/proto_shape.go`, `svc/svc_shape.go`, `gateway/shape.go`, `caps/shape.go`, `shape/stub.go`, the registration lines |
| b1-draft | `internal/apps/shape/` drafts, overlay, change store; `records/overlay_shape.go` + one call line |
| b2-apply | `internal/apps/shape/` migrate, apply, versions, health |
| b3-builder | `builder/shape*.go`, `internal/apps/shape/` pipeline + replay |
| c0-desk | `src/lib/types/apps-shape.ts`, `src/lib/apps/bridge/mock-shape.ts` + one registration line |
| d1-frame | `src/features/app-shape/` frame, plan menu, centre, Builder pane, draft bar, User lock, handover; one line each in `app-tab.tsx` / `agent-pane.tsx` to mount Shape |
| d2-apply | `src/features/app-shape/` Show diff, Apply sheet, Versions sheet (App ⌄ item via `menu-items/shape.ts`, R2), Undo, personal toast |
| d3-states | `src/features/app-shape/` replay line, question, failed change, broken bar |
