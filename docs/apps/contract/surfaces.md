# Apps · wave 2 · surfaces contract

Track `surfaces` (session codeaf-w2-surfaces). Boards: 04 (surfaces block),
09 (documents · Slides), 14 (Slides journey, phase B). The owner's boards win
over this file. This file adds to `docs/apps/CONTRACT.md`; it changes nothing
there. Same text in both repos.

Scope: the six document surfaces **Text · Canvas · Grid · Timeline · Graph ·
Map** as kit parts, plus the **Pages** pattern (the parts strip); server part
storage; Ask · Nudge · Comment; one writer per part (light lock); presence;
versions per part; Present · Share link · Export PDF / PPTX / CSV / .docx.

Not here: Shape and app versions (track shape), roles and workspace auth
(track spaces), connectors (track connect). Until spaces lands, every caller
is `svc.Owner` with role Editor; the role checks below are written against
`svc.Caller.Role` so they hold once spaces fills it.

## 1 · Words

- **Surface file** — a content file of one surface kind. Content, never
  definition (rule 01: the Builder never reads it; rule 02: the app agent
  writes it only through `parts.*`).
- **Document** — one surface instance, addressed by `doc` = its folder or
  file stem under `content/` (e.g. `decks/q4-board-deck`). A document is
  usually owned by a record (the deck record); the record holds `doc`.
- **Part** — the unit inside a document: one slide, one section, one sheet,
  one artboard, one plan, one graph, one map layer. **Part id** = its
  relative path inside the document (`slides/5.json`, `section-2.md`).
- **Part version** — integer, starts at 1, +1 on every Ask, Nudge or Restore
  of that one part. A document has no version number.

## 2 · Files on disk

`$CODEAF_HOME/apps/<app>/content/<doc>/…` (real) and
`$CODEAF_HOME/apps/<app>/sample-content/<doc>/…` (sample mode: preview,
check, Shape drafts — rule 04). `svc.Scope.Data` picks the root.
`$CODEAF_HOME/apps/<app>/parts.db` (SQLite, modernc): part versions,
comments, share links. Locks and presence are in memory only.

| Surface | Part file(s) in a doc | One part = |
| --- | --- | --- |
| Canvas | `slides/<n>.json` (or `artboards/<id>.json`) + `theme.json` | one file: `{title, status, frame: "16:9"\|"board", notes, boxes: [{id, type: text\|image\|shape\|chart\|record\|media, x, y, w, h, text?, src?, spec?, record?, style?: {tone?, font?: heading\|body}}]}` |
| Text | `section-<n>.md` (+ `doc.json` order, title) | one section file, Markdown; merge fields `{{deal.name}}`; clause numbers computed, not stored |
| Grid | `sheets/<name>.csv` + `sheets/<name>.formulas.json` | one sheet (csv + its formulas `{"C5": "=SUM(C2:C4)-B5"}`) |
| Timeline | `plan/<name>.json` | one file `{items: [{id, title, start, end, dependsOn?, lane?, record?}], view: gantt\|calendar\|shifts}` |
| Graph | `graph/<name>.json` | one file `{nodes: [{id, label, record?, x?, y?}], links: [{from, to, label?}], layout: tree\|flow\|free}` |
| Map | `places/<name>.json` | one file, GeoJSON FeatureCollection; `properties {label, record?, kind: place\|route\|live}` |

Every part file also has a sidecar status the server keeps in `parts.db`:
`status` (`Draft · In review · Final` for Canvas; free text otherwise) and
`owner`. Order of parts = `doc.json` `parts: [ids]` (written by reorder).

## 3 · Wire (Go `internal/apps/bridge/proto_surfaces.go`, TS `src/lib/types/apps-surfaces.ts`)

```
Surface   = "canvas"|"text"|"grid"|"timeline"|"graph"|"map"
PartRef   {doc, part}
Part      {doc, part, surface, version, status?, owner?, text (file body; csv for grid), extra? {formulas?}, updated_at, updated_by, by_agent bool}
DocInfo   {doc, surface, title, parts: [PartSummary], share? Share}
PartSummary {part, version, status?, title?, lock? Lock, comments_open int}
Lock      {doc, part, holder (member or "agent"), agent bool, since, idle_until, takeover? {by, until}}
Presence  {doc, member, part?, at}
Comment   {id, doc, part, anchor {box? | range? {from, to} | cell? | node? | feature? | item?}, text, by, for: "agent"|"person", state: open|resolved|sent, at, resolved_by?}
PartVersion {doc, part, version, kind: create|ask|nudge|restore|reorder, label (ask text or nudge summary), by, by_agent, at}
Share     {doc, slug, url, on bool, by, at}
```

### Methods (all app-scoped)

| Method | params → result | Rule |
| --- | --- | --- |
| `docs.list` | `{surface?}` → `{docs: DocInfo[]}` | read |
| `docs.get` | `{doc}` → `DocInfo` | read |
| `docs.create` | `{doc, surface, title, parts?: [{part, text}]}` → `DocInfo` | write (User+) |
| `docs.reorder` | `{doc, parts: [ids]}` → `DocInfo` | write |
| `parts.get` | `{doc, part, version?}` → `Part` | read |
| `parts.lock` | `{doc, part}` → `Lock` | write; `conflict` if held by another → result carries holder |
| `parts.takeover` | `{doc, part}` → `Lock` | asks the holder; after 30 s no answer hands over; agent holder → agent stopped |
| `parts.answer` | `{doc, part, keep: bool}` → `Lock` | holder answers a takeover |
| `parts.release` | `{doc, part}` → `{}` | holder only |
| `parts.write` | `{doc, part, text, base_version, kind: nudge\|ask, label, extra?}` → `Part` | must hold the lock; `conflict` if `base_version` stale; validates the surface format (Canvas: every box inside the frame) |
| `parts.add` / `parts.remove` | `{doc, part, text?}` → `DocInfo` | write; remove keeps versions (rule 07 spirit) |
| `parts.versions` | `{doc, part}` → `{versions: PartVersion[]}` | read |
| `parts.diff` | `{doc, part, from, to}` → `{from_text, to_text, lines: [{op: "+"\|"-"\|" ", text}]}` | read |
| `parts.restore` | `{doc, part, version}` → `Part` | write; takes and releases the lock itself; writes a new version |
| `presence.set` | `{doc, part?}` → `{}` | read; looking never locks |
| `comments.list` | `{doc, part?, state?}` → `{comments}` | read |
| `comments.add` | `{doc, part, anchor, text, for}` → `Comment` | read (Viewer may not: User+) |
| `comments.resolve` | `{id}` → `Comment` | User+ |
| `comments.send` | `{id}` → `{thread, turn}` | turns it into an Ask on that part (agent.send with `part`) |
| `share.set` | `{doc, on}` → `Share` | User+; off breaks the link at once |
| `share.open` | `{slug}` → `DocInfo` | members only; always the latest versions |

Lock rules (board 09 c): one holder per part; taken by `parts.lock` (Nudge)
or an Ask on the part; viewing/commenting never lock; released on
`parts.release`, agent done, **2 min idle** (every `parts.write`/`parts.lock`
by the holder refreshes it), or the holder's WebSocket closing (offline =
release at once). Agent lock + takeover = agent stopped, its unfinished edit
not written.

### Topics

| Topic | Event |
| --- | --- |
| `app/<id>/doc/<doc>` | `DocEvent {kind: part\|order\|lock\|presence\|comment\|share, part?, version?, lock?, presence?, comment?, share?, by}` |

One topic per document keeps the desk to one subscription per open document.
Activity lines (`"Slides agent changed slide 5 · v3"`, `"Raj nudged slide 5 ·
v4"`) are `PartVersion` events; the audit/Activity store belongs to track
spaces — we publish, they record.

### Errors

Existing codes only: `conflict` (lock held, stale base_version, takeover
pending), `invalid` (part fails its surface format), `denied` (role),
`not_found`.

## 4 · Server packages (bin)

| Package / file | Owns |
| --- | --- |
| `internal/apps/bridge/proto_surfaces.go` | wire types + method/topic names above |
| `internal/apps/svc/svc_surfaces.go` | `Parts`, `Collab` interfaces |
| `internal/apps/parts/` | docs, part files, formats per surface, versions, diff, restore, share links → `svc.Parts` |
| `internal/apps/collab/` | locks, takeover, presence, comments → `svc.Collab` |
| `internal/apps/gateway/surfaces.go` | method table for §3, registered with one hook line |
| `internal/apps/caps/surfaces.go` | role rules for §3 |
| `internal/apps/spec/kit.json` | the 7 new kit parts (§6), added in place (ruling R3) |

Registration lines in shared wave-1 files (one each): `svc.Services`
gets `Parts` + `Collab` fields (one line block), `server.go` Wire calls
`parts.New` / `collab.New`, gateway hook.

Later in this track, as new files plus one registration line each (ruling R4), owning session told first: `internal/apps/agent`
gets a `parts.write` tool used only for an Ask on a part (holds the agent
lock); `internal/apps/check` gets `kit/surface` and renders surfaces on
sample content.

## 5 · Desk (codeaf-desk)

| Dir / file | Owns |
| --- | --- |
| `src/lib/types/apps-surfaces.ts` | §3 types |
| `src/lib/apps/bridge/surfaces.ts` | typed client calls + `useDoc(doc)` / `useParts(doc)` subscription hook |
| `src/lib/apps/bridge/mock-surfaces.ts` | mock bridge module (in-memory docs, locks with timers, comments) |
| `src/kit/surfaces/frame/` | Pages strip, part bar (Ask · Nudge · Comment · Versions N), lock shimmer "agent is editing" / "Mona is editing", presence avatars, comment pins + thread, Versions panel (before/after, Restore), Present (⌘↵, ← → esc, N notes), Share sheet, Export ⌄ |
| `src/kit/surfaces/canvas/` … `text/` `grid/` `timeline/` `graph/` `map/` | one dir per surface |
| `src/kit/surfaces/export/` | PDF (from the same HTML), PPTX (pptxgenjs), CSV, .docx |
| `fixtures/apps/slides/` | Slides fixture app (board 14) + `content/decks/q4-board-deck/` (board 09's 8 slides) |

Every surface implements one interface; the frame owns everything else:

```ts
interface SurfaceProps {
  part: Part;                 // current version
  mode: "view" | "nudge" | "comment" | "present" | "thumb"; // comment: a click calls onAnchor, nothing edits
  theme?: unknown;            // canvas theme.json etc.
  props?: Record<string, unknown>; // the screen node's kit props: frame (Canvas), view (Timeline, Graph), tiles (Map, workspace URL)
  comments: Comment[];        // pins to draw at anchors
  onNudge(text: string, label: string, extra?: Part["extra"]): void;  // frame writes parts.write (extra: grid formulas)
  doc?: { parts: PartSummary[] };              // the other parts of the document (Grid match mode, Pages)
  loadPart?(part: string): Promise<Part>;       // fetch another part on demand; never preloaded
  loadVersion?(version: number): Promise<Part>; // an older version of this part (Text track changes, Versions)
  onAnchor(anchor: Comment["anchor"]): void;   // Comment mode click
  onOpenRecord?(record: string): void;          // click on a node/pin/item/box bound to a record
  busy?: { start: string; end: string }[];      // Timeline free/busy from the calendar connector, when present
}
type Surface = { kind: SurfaceKind; Part: React.FC<SurfaceProps>; newPart(): string; exportAs: ("pdf"|"pptx"|"csv"|"docx")[] };
```

Registered in `src/kit/surfaces/index.ts` (`surfaces: Record<SurfaceKind, Surface>`).

New dependencies (exact pins, added by the lane that needs them): canvas
`react-moveable`, export `pptxgenjs`, text `@tiptap/react` + starter kit,
grid `@ironcalc/wasm`, graph `@xyflow/react` + `elkjs`, map `maplibre-gl`,
docx `docx` + `mammoth`.

## 6 · Kit parts added

`Pages` (pattern; the parts strip), and six `surface`-kind parts `Canvas`,
`Text`, `Grid`, `Timeline`, `Graph`, `Map`. Each surface part: `bind:
["doc"]` (new binding kind `{"doc": "<$ref to record field or literal>",
"surface": "<kind>"}`), props `frame` (Canvas: `16:9|board`), `view`
(Timeline: `gantt|calendar|shifts`; Graph: `tree|flow|free`), `tiles`
(Map; from workspace config), events `open` (Graph/Map: click → record).
Kit law `kit/surface`: hand-rolled editors fail; at most one surface per
screen.

## 7 · Lanes

Task ids `w2surfaces-*`; infra `~/af-w2-surfaces-lanes`; branch
`apps/w2-surfaces` in both repos.
