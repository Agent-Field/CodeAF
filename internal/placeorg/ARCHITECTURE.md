# placeorg — the architecture the port must have

Read with `doc.go`. This is the shape the owner's Places algorithm (the
reference implementation `places_harness.py`, 684 lines, and its doctrine)
takes inside the codeaf engine. Every lane in `PORT-TASKS.md` builds to it;
`contract_law_test.go` keeps every package to it on every pull request.

## 1. The package, and why it is called that

`internal/placegraph` is the store: what the person HAS (places, memberships,
receipts). `internal/placeorg` is the organiser: what the person SHOULD have,
proposed. The two names sit side by side on purpose, the way `placegraph` and
`desktopbridge/places_*.go` already do; "org" is the verb the person sees
("Organize now") cut to the length Go packages are. Alternatives rejected:
putting the pipeline inside `placegraph` (the store would then import a Leiden
implementation and an embedding table, and its header comment says it "does
NOT … call a model"); a name without "place" in it (the repository has two
meanings of place and the prefix is how a reader tells them apart).

Layout. The contract lives in the root package; each stage is its own
sub-package so build lanes never edit one another's files and the law can
check a package at a time:

```
internal/placeorg/            the contract: types, interfaces, Runner, journal, invariants, the law
  dedup/      kind/      anchors/    vectors/    localembed/   knn/        community/
  nest/       audit/     park/       name/       review/       link/       stream/
  steer/      correct/   journal/    parity/     (one lane owns one directory)
```

A sub-package imports `placeorg` and the standard library. It may import
`placegraph` for its types. It may NOT import `net/http`, `internal/provider`,
`internal/ai`, `internal/session`, `internal/remote` or `internal/desktopbridge`
(`TestNoStageOpensADoorItself`). The engine wires this package, never the
other way round.

## 2. The contract as types

| File | What it pins |
| --- | --- |
| `judge.go` | `Judge` is the ONLY way a stage reaches a model. `Choose(ctx, q, fallback Option) Pick` and `Write(ctx, q, valid, fallback string) Pick` cannot fail: the fallback is a required argument and is the answer whenever the model is absent, slow, over budget or outside the contract. `Judged` (`Fallbacks() []Fallback`) is required of every package that references `Judge`. The six roles this package asks. `Budget`/`Spent`. |
| `stages.go`, `stream.go`, `steer.go` | One interface per stage. Rule stages take no `Judge` and cannot reach one. `Stages` is the registry struct: one field per stage, nil is a build error the Runner reports before starting. |
| `ops.go` | The closed `OpKind` set, `Op`, `Plan`. A stage returns `[]Op`; it cannot touch the tree. |
| `tree.go` | `Tree` with read methods only; `apply` is unexported and `TestOnlyTheRunnerChangesTheTree` lets only `(*Runner).apply` call it. |
| `pipeline.go` | `Runner.Run(ctx, corpus, Options) (*Plan, error)`: stages in order, every op through `apply` (journal then tree), self-check, Plan. `Mode`, `Progress`, the three resolutions, consensus figures. |
| `invariants.go` | The self-check as code: eleven `Invariant{Name, Check}`; a Plan that fails any never leaves the package. The caps live here once. |
| `journal.go` | `Journal.Write(Entry)`; every judgment with its options and pick, every fallback, every op, every invariant result. |
| `space.go` | `SpaceID`, `Embedder`, `Vectors`, `Centroid`. A tree lives in one space. |
| `state.go` | `State`: the organiser's own file (space, thresholds, axes, lenses, exemplars, constraints, kinds, representatives, last run). |
| `chat.go` | `Chat`, `Corpus`, `Kind`; `FromEvidence` builds a chat from `placegraph.ChatEvidence`. |

The law (`contract_law_test.go`) enforces: forbidden imports; `Judged` on any
package mentioning `Judge`; no `Choose`/`Write` with a `nil`, `""` or `{}`
fallback; `apply` only inside `(*Runner).apply`. It walks sub-packages, so a
lane's package is covered the day it lands.

## 3. The pipeline mapped onto packages

| # | Stage (harness) | Package | Interface | Model? | Fallback | Parity check against the harness |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | dedup (MinHash/LSH) | `dedup/` | `Deduper` | no | — | same fold count ±2% on the same corpus; representative = earliest |
| 2 | kind filter | `kind/` | `Kinder` | no | — | every probe the harness regex catches is caught; no title list anywhere |
| 3 | entity anchors | `anchors/` | `Anchorer` | no | — | ≥90% of the harness's hard-coded hits recovered as derived anchors, plus issue refs |
| 4 | embed | `vectors/` + `localembed/` | `Vectorizer`, `Embedder`, `Vectors` | no (a door) | primary → local → hashed, one space per run | kNN overlap primary vs harness ≥0.9 on the shared cache |
| 5 | kNN graph | `knn/` | `Neighbours` | no | — | edge set equal on the fixture (k=8, floor 0.35, anchor boost IDF-weighted) |
| 6 | consensus Leiden ×3 | `community/` | `Communities` | no | — | ARI ≥0.85 vs python leidenalg on the fixture graph at each resolution |
| 7 | nesting | `nest/` | `Nester` | no | — | identical parent map on the fixture |
| 8 | auditor ops | `audit/` | `Auditor` (Judged) | facet vote | KEEP (never demote on silence) | op list equal on the fixture with a canned facet answer |
| 9 | park (Chow) | `park/` | `Parker` | no | — | park-level histogram ±5% on the fixture |
| 10 | name | `name/` | `Namer` (Judged) | charter + name | first untaken c-TF-IDF candidate | candidate lists equal; names under a fake Judge equal |
| 11 | review | `review/` | `Reviewer` (Judged) | one outline pass | no change | renames applied only when valid; merges proposed only |
| 12 | links + lenses | `link/` | `Linker` | no | — | shortcut histogram equal; by-month lens on every place ≥15 chats |
| 13 | stream + births | `stream/` | `Filer`, `Guesser` (Judged) | cross-branch contest only | shallower park | holdout decisions equal under a fake Judge; judge rate ≤10% |
| 14 | steer | `steer/` | `Steerer` (Judged) | parse only | `SteerNone`, nothing changes | both harness wishes parse to the same action |
| 15 | correct | `correct/` | `Corrector` | no | — | exemplar moves the chat; neighbours-that-follow count equal |
| 16 | self-check | root `invariants.go` | `Check` | no | — | 11 of 11 on the harness corpus |

Order inside `Runner.Run`: dedup → kind → anchors → vectors → knn →
communities (fine, mid, coarse) → nest → audit → park → name → review → link →
check. Progress is published after each stage and inside the long ones
(vectors per batch, communities per run, name per place).

## 4. Data model: what maps onto placegraph, what is replaced, what stays

| Algorithm concept | Where it lives | Note |
| --- | --- | --- |
| place | `placegraph.Place` | one parent (`Parents[0]`); the graph allows a DAG, the organiser only ever makes a forest |
| home | `placegraph.Membership` with `Link == false` | one per chat, `AddedBy: ai` |
| shortcut (≤2) | `placegraph.Membership` with **new** `Link bool \`json:"link,omitempty"\`` | ADDED to placegraph: the one schema change. `ChatsIn` keeps listing links; the Home view marks them "also in". Omitted when false so an old file reads unchanged. |
| inbox | `placegraph.NowID` (unplaced) | no rows; a chat in Now is a chat with no membership |
| probe, duplicate | `placeorg.State.Kinds`, `.Representative` | the store never learns a kind; a duplicate gets the same membership rows as its representative |
| lens | `placeorg.State.Lenses[placeID]` | a view, never a Place; served by `GET /places/{id}/lenses`; journaled as nothing in the store |
| axis (steer) | `placeorg.State.Axes[placeID]` | future arrivals in the place sub-file by it |
| exemplar, constraint | `placeorg.State` | constraints apply before park; proposed ones are ledger offers |
| journal | `places-journal.jsonl` | the record; not the undo |
| undo | `placegraph.Receipt` | a Plan is ONE commit (`Store.ApplyPlan`, added), so ⌘Z takes a whole run back |
| proposal | `placegraph.Proposal` + **new** `ProposalPlan` kind | the ledger keeps offers, snoozes and the daily call count exactly as now |
| policy | `placegraph.RecommendPolicy` table | rows ADDED: organize mode, run call budget, run dollar budget, live guess on/off; nothing removed |

**Replaced in placegraph** (deleted once the bridge switches, lane
`t-po-bridge`): `recommend_discover.go` whole (the model-finds-a-group question:
consensus communities over kNN find the garden group the file's header
describes, and the real-chat fixture there becomes a parity test in `stream/`);
`findClusters`, `wordClusters`, `anchorClusters`, `rankCandidates` and the
caps on shown candidates in `recommend_match.go`; `fileQuestion` and
`suggestQuestion` in `recommend_model.go`; the cluster/discover half of
`Recommender.Organize`. `Recommender.FileChat` keeps its door, its idempotence
(`Considered`) and its auto-file path; its body becomes: folder rule → `Filer.File` → offer.

**Kept in placegraph, untouched**: the store, memberships, receipts and undo,
the ledger, `mergeOffers` (same-name siblings; exported as `SameNameMerges` so
the audit stage can propose them), `cleanFolder` (exported as `CleanFolder`),
`decodeObject`/`pickIndex`/`cleanSuggestedName` (exported, reused by the
Runner's Judge to read answers), policy table and settings page, rail, knows,
stale, digest, from-folder, tab groups, rollup, resolve, sources, decide.

**Added to placegraph** (lane `t-po-store-plan`): `Membership.Link`;
`Store.ApplyPlan(ops []PlanOp) (Receipt, error)` applying creates, renames,
reparents, merges, deletes, files, moves and links in ONE `mutate`, validated
as a whole first so a refusal changes nothing; `ProposalPlan` with
`Accept` applying through `ApplyPlan`; `AIPlaces` bookkeeping for every place
a plan creates.

## 5. Storage

All beside the store at `~/.codeaf/desktop/`:

| File | Owner | Shape | Size at 10k chats |
| --- | --- | --- | --- |
| `places.json` | placegraph | unchanged + `link` | — |
| `places-ai.json` | placegraph ledger | unchanged + plan proposals | — |
| `places-org.json` | `placeorg.State` | JSON, set aside when damaged | < 1 MB |
| `places-journal.jsonl` | `placeorg/journal` | append-only, one line per entry, rotated at 32 MB | a few MB per run |
| `places-vectors-<space>.bin` | `placeorg/vectors` | append-only records `(chatID, textHash, 256×float32)`; loaded whole; compacted when half is stale | 10 MB primary, 10 MB local |

Vectors are never in `places.json`. The hashed space keeps no cache.

**A vector is fetched once and never refetched.** The reference run showed
the provider's embeddings differ slightly between fetches and that alone
moves leaves (ARI 0.65 between two fetches of the same corpus). The cache is
keyed by the hash of the embedded text; only a changed text is embedded
again, and a refresh run reads the cache, never the provider, for every chat
already in it.

## 6. The organising run ("Organize now")

Entry: `POST /places/organize {"mode":"add-on"|"refresh"}` → `PlaceAdvice`
queues ONE run (a second request while one runs is answered with the running
one). `GET /places/organize` and the world-feed record `placesOrganizing`
carry `Progress`; `POST /places/organize/cancel` cancels the context. A
cancelled run applies nothing and journals `run.ended cancelled`.

Modes:

- **add-on** (default; also the idle pass): `Options.Existing` is the store
  as a Tree with every place `Person`-marked or AI-marked. The pipeline
  embeds only chats without a cached vector, parks every unplaced chat into
  the existing tree, runs `Filer.Births` over Now, and runs name/review only
  on nodes a birth made. Existing places keep their ids, names and members.
  Ops produced: file, park, inbox, link, birth (proposed), rename and merge
  (proposed), lens.
- **refresh**: a full rebuild in a shadow Tree. Person-made places are
  must-link seeds (their members stay together, the place is never
  dissolved or renamed without a proposal). AI-made places may be
  dissolved, re-nested or renamed. The Plan's summary states the difference
  ("3 places would go, 2 would be renamed, 410 chats would move").

What is applied without a yes: nothing structural. The policy decides the
rest exactly as today: `autoFile` on → filings into existing places apply
(marked AI, undoable); lenses and exemplars always apply (they change no
structure). Everything else — births, renames, merges, splits, dissolves,
interposes, constraints — is the Plan proposal the person accepts or declines
in one go, with edits (uncheck an op). Accept = `Store.ApplyPlan` = one
receipt.

Budget: `Budget{Calls, USD}` from policy (`organizeCallsPerRun` default 150,
`organizeUSDPerRun` default 0.05). Past it the Judge falls back and the Plan
is `Provisional` with the reason in its summary. Measured reference: ~14
calls, ≈$0.005 per rebuild of 500 chats; at 10k chats expect ~80 names + ~30
facet votes + 8 charters + 1 review ≈ 120 calls ≈ $0.03.

Seed: `Options.Seed` from policy or the plan id; consensus runs use
`Seed+i`. Same corpus, same seed, same space → bit-identical structure.

## 7. The stream path for each new chat

After a chat's first settled reply (the existing `afterTurn` trigger):

1. The bridge builds an `Arrival`: `Chat` from evidence, `FolderPlace` from
   the folder rule, `Vector` from one primary embedding call (cache miss) —
   the one network call on this path.
2. `Filer.File`: folder prior → hierarchical Chow over the tree's centroids
   (`State.Thresholds[space]`) → below floor: inbox; cross-branch contest
   inside the margin: `Judge.Choose` among the top five places shown by
   name (no NEW option; "none" → inbox) with fallback = the shallower park.
3. The result is the filing offer today's `FileChat` returns, or an applied
   filing under `autoFile`. Centroids update (parked chats are citizens).
4. `Filer.Births` runs over Now on the idle pass: a kNN-connected group of
   ≥`Quorum` distinct (non-duplicate, non-probe) chats → one `OpBirth`
   proposal, named by the Namer. Arrivals never birth.

Budget: `FileBudget` 50 ms excluding the embedding; the embedding call is
bounded by the role patience and falls back to the local space ONLY for the
guess, never for the filing (a filing in the wrong space is a wrong filing;
it waits for the next pass instead and the chat stays in Now).

## 8. Live guess while typing

`POST /places/guess {"draft": "...", "workspace": "...", "chatId": "..."}` →
`Guesser.Guess` in `SpaceLocal`: embed the draft with the static table
(microseconds), score against every place's local centroid, add the
continuation prior (the place opened within the last hour, +0.05) and the
folder prior (hard), return `{place, name, confidence, others[≤2]}`. Server
budget 20 ms p95 at 500 places; the desktop debounces 300 ms and shows a
pill in the composer ("→ Desk app") the person can click to change. Nothing
is written and no model is called. Local-space centroids are maintained
beside primary ones from the same memberships, so the guess and the filing
read one tree.

## 9. Corrections → exemplars → constraint proposals

Every hand move (`POST /places/{id}/members`, `members/remove`, a drag) with
`AddedBy: you` reaches `Corrector.Correct`: the chat becomes an exemplar of
the destination with weight 2 in every space's centroid; the journal records
how many of its eight nearest neighbours would now follow. When three
corrections share a pattern (same from→to, or a shared anchor among the
moved chats) an `OpConstraint` is proposed: "Chats about #412 go to Desk
app?" Confirmed constraints apply before park and are listed on the place's
Home. Nothing moves but the chat the person moved.

## 10. Steering grammar

`POST /places/steer {"scope": placeID, "sentence": "..."}` → `Steerer.Parse`
asks the Judge to choose the action and the axis among the shown options
(scope is the place the person is on; the model cannot change it) → a
validated `Steer{Action, Scope, Axis, Values}` → `Steerer.Render`:

| Sentence | Steer | Render |
| --- | --- | --- |
| "organise this by month" | axis/month | `OpAxis` + `OpLens` by-month (applied; no structure) |
| "by project" / "by repo" | axis/project or workspace | `OpAxis` + proposed `OpSplit` into anchor-value children ≥ Quorum |
| "by kind of work" | axis/kind | kind labels per chat (`RolePlaceKind`, batches of 20, closed list `KindsOfWork`, fallback verb heuristic) → proposed `OpSplit` |
| "keep papers separate from code" | separate [papers, code] | `OpAxis` custom + proposed `OpSplit` by nearest-value |
| "flatten this" | flat | proposed dissolve of children into the place |
| anything else | none | "I did not understand"; nothing changes |

The axis is stored on the place; arrivals in that place sub-file by it.

## 11. Model calls

Through the engine's role door only: `session.(*Agent).AskPlaces` accepts
the six roles in `judge.go` (lane `t-po-embed-door` widens its allow list and
registers them at `TierLow` beside `RolePlaceFile`/`RolePlaceSuggest`). The
bridge's `askThrough` closure is the `placegraph.Asker` the Runner wraps as
its `Judge`: budget check → ask → `decodeObject`/`pickIndex` → journal.
Temperature 0, reasoning off, ≤60 output tokens for a pick, ≤400 for the
review. The desktop's low tier is `deepseek/deepseek-v4.1-flash` (D4); the
e2e lane asserts the recorded model on every journal line is that one.

Embeddings: a NEW door, `session.(*Agent).EmbedPlaces(ctx, texts) ([][]float32, error)`
calling the provider's embeddings endpoint (`openai/text-embedding-3-small`,
`dimensions: 256`), billed through `addDetachedUsageAs` with role
`placeembed`. The bridge hands it to `placeorg` as an `Embedder`.

## 12. Performance budgets (laptop, 10k chats, 8 cores)

| Stage | Budget | How |
| --- | --- | --- |
| dedup | 2 s | 128 permutations, 5-word shingles, 16 bands |
| kind, anchors | 1 s | one pass, IDF table |
| vectors (cold) | 60 s | 157 batches of 64, 4 in flight; warm: 0 |
| kNN | 5 s, < 50 MB | blocked exact cosine, goroutine per block, K=10 kept per row |
| communities | 30 s | 3 resolutions × 8 seeded runs on 10k nodes / 100k edges, sparse co-association over edges only |
| nest, audit, park, link | 3 s | — |
| name, review | 20 s | ~120 calls, 4 in flight |
| **rebuild total** | **< 2 min** | — |
| filing | 50 ms + one embedding call | — |
| live guess | 20 ms p95 | local table |
| memory, whole run | < 150 MB | no n×n anything, anywhere |

`parity/` holds the benchmarks; a budget is a test that fails. Parity
target: the reference run's `ref/assignments.json` on `ref/emb_all.npy` +
`ref/emb_row_ids.json` (saved embeddings, so the comparison is of structure
alone), compared by ARI per level. The corpus is the owner's own library and
is NOT committed: parity tests read `PLACEORG_REF=<dir>` and skip without it;
a small synthetic fixture in `testdata/` runs everywhere.

## 13. What the reference implementation gets wrong, and the port's fix

Verified in `places_harness.py` at the lines given.

1. **A fixed subject list** (l.121–123: codeaf, desk, wisp, carnatic…) is
   benchmark hardcoding. → `anchors/` derives anchors: issue refs, workspace
   family, repo key, high-IDF proper tokens, confirmed place names.
2. **Consensus is a no-op** (l.156: `seed=SEED` on all 8 runs → identical
   partitions → co-association is 0/1 → the final cut is the first run). →
   seeds `Seed+i`; a test asserts the 8 partitions differ.
3. **O(n²) memory** (l.144 `sims = X @ X.T`, l.153 `M = zeros((n,n))`,
   l.163 an n² Python loop, l.447 `np.ix_`): 400 MB per matrix at 10k and
   minutes of Python. → kNN lists, co-association over edges, sub-graph
   split from the kNN rows.
4. **Python-only fallback** (l.105 sentence-transformers). → `localembed/`:
   a static Model2Vec table (potion-base-8M, MIT, int8 ≈ 8 MB, go:embed)
   with a pure-Go WordPiece tokeniser; under it a hashed space with no asset.
   Decided over ONNX (a runtime dependency and 90 MB) and over "API only"
   (the owner's outage test is a requirement).
5. **Steer handles month only** (l.583), and the month path never ran: the
   scope lookup is exact-name (l.584, "codeaf" ≠ "Codeaf Ui") and the
   re-render keys a tuple with `.split(":")` (l.588), which would raise.
   No re-run modes. → the grammar in §10, add-on/refresh modes in §6.
6. **The facet fallback demotes** (l.140 returns "" → l.265 "not THING" →
   DEMOTE): an outage turns every un-anchored child into a lens. → fallback
   is KEEP.
7. **Duplicates are dropped** (l.64 keeps the earliest only); "a home for
   every chat" is checked on the survivors. → duplicates are filed beside
   their representative; the invariant counts them.
8. **INTERPOSE is journaled but never applied** (l.242 appends a line and
   changes nothing); "ops journaled" (l.656) is `len(ops) > 0` and "forest"
   (l.655) is `True`. → ops are applied only through `apply`; the invariant
   replays the journal against the tree; forest is checked.
9. **Inbox births are not implemented** (the harness only counts a "NEW"
    answer, l.563); the quorum-4 birth from Now is a rule the port builds
    (`Filer.Births`) and the harness has no parity for it.
10. **The streaming judge offers NEW** (l.560, counted as a birth at l.563),
   contradicting the inbox-quorum rule. → no NEW option; "none" → inbox.
11. **Correction weight is absolute** (l.626 adds `2·x` to a unit centroid,
    so one exemplar owns two thirds of a 100-chat place). → centroids are
    sums with counts; an exemplar counts as two members.
12. **Thresholds are one space's** (FLOOR 0.45, MARGIN 0.04 tuned to the
    API model) but used after the local fallback. → `State.Thresholds` per
    space, calibrated from the kNN similarity quantiles of that space.
13. **SPLIT children are display-only** (l.476 writes `tree_view`; park,
    centroids and the stream never see them). → one Tree; every node is
    indexed.
14. **The probe rule is a regex of this person's probes** (l.72). →
    structural signals only (length, no reply, repetition, canned opening).
15. **Names are Title-Cased by code** (l.369 `capitalize()`), so "codeaf"
    becomes "Codeaf". → keep the spelling that appears most in member titles.
16. Lens parent names go stale after a review rename (l.497 reads the
    pre-review `names`); a chat's date is `created` in one place and `ts` in
    another (l.509 vs l.596). → one Tree, one `Chat.Created`.

## 14. Wire contract (bridge, lane `t-po-bridge`; desktop lanes build to this)

| Method, path | Body → reply |
| --- | --- |
| `POST /places/organize` | `{mode}` → `{run, started}`; 409 `running` with the run id if one is running |
| `GET /places/organize` | → `{running: bool, progress: Progress, last: RunSummary, plan?: {id, summary, ops: [...], invariants}}` |
| `POST /places/organize/cancel` | → `{cancelled: bool}` |
| `POST /places/proposals/{planId}/accept` | `{skip: [opIndex...]}` → existing accept reply plus `receipt` (one) |
| `GET /places/tree` | → the forest with per-node `{id, name, depth, chats, parked, lenses: [names], axis}` for the plan review |
| `GET /places/{id}/lenses` | → `{lenses: [{name, axis, value, chats: [...]}]}` |
| `POST /places/guess` | `{draft, workspace, chatId}` → `Guess` |
| `POST /places/steer` | `{scope, sentence}` → `{steer, understood: bool, plan?: {...}}` |
| world feed record `placesOrganizing` | `Progress` |

The chat Home view marks a membership with `link: true` as "also in".

## 15. Person-facing words (the manual law and the vocabulary law)

Organize now · Add to what's there / Start fresh · "Reading 1,240 chats" ·
"Grouping" · "Naming (12 of 30)" · "Checking" · "also in" (a shortcut) ·
"views" (lenses: "by month", "by project") · Now (the inbox) · "I did not
understand that; try 'by month' or 'by project'". Never: auditor, verdict,
cluster, centroid, embedding, Leiden, consensus, fallback. The manual pages
(lane `t-po-manual`) quote these strings as the code spells them.

## 16. Lane ownership

One lane, one directory under `internal/placeorg/`, plus the files named in
its task. The root package is frozen after this document lands except for
`invariants.go` bodies (audit lane) and `pipeline.go` bodies (pipeline lane).
Engine and bridge files are owned by the lanes `PORT-TASKS.md` names and by
nobody else.
