# AI roles and the Places recommendation policy

Status: implemented as a library and a provisional Settings page, 2026-10-09, and
wired to the engine and the desktop bridge the same day (`work/d5-ai-policy-wire`). No
window draws an offer yet; see "What is wired and what is not" before relying on any of it. Every default not marked
**design** below is an engineering choice waiting on the product designer.

Code: `internal/placegraph/policy.go`, `recommend*.go`; `internal/config/desktoproles.go`,
`desktopplaces.go`; `desktop/src/features/settings/`.

## 1. Model roles the desktop exposes

Every role defaults to `deepseek/deepseek-v4.1-flash` (`config.DesktopDefaultModel`).
A desktop role pins a fixed set of engine roles (`internal/roles`); a choice is written
once to `desktop.roles.<id>` in the profile's `config.json` and read back by the engine
on every call (`config.DesktopRolesSource`). Reset removes the row.

| Section | Desktop role | Engine roles | Live in this branch |
| --- | --- | --- | --- |
| Conversation and tasks | `conversation` | the open chat's own model | yes |
| | `tasks` | worker, careful, division | yes |
| | `planning` | planner, designer, shaper | yes |
| | `checking` | auditor, repair | yes |
| Naming and summaries | `naming` — Chat titles | title | yes |
| | `worknames` — Task and job names | taskname, jobname, caption | yes |
| | `summaries` | none yet (recap, from the History lane) | no |
| Places organization | `placefiling` — Chat filing | `placefile` | yes |
| | `placesuggest` — Place suggestions | `placesuggest` | yes |
| Memory, routing and safety | `memory` | reflex, consolidate | yes |
| | `routing` | router, routerconfirm, markreader, handoff, spellout, intake | yes |
| | `safety` | guardian, sentinel | yes |

**Live** is computed, not declared: a role is live when the engine has *registered* one of
its engine roles, and registration lives in the file that makes the call. The page says
"Not in use yet. Your choice is kept for when it is." for a role that is not live.

**The split of the old "Titles and summaries" row.** `naming` keeps its id (so a saved
choice stays with chat titles); `worknames` and `summaries` carry `inherits: naming` and
run on the saved `naming` choice until chosen themselves. Reset returns them to following.
This supersedes the wording of designer decision Q8 ("Titles and summaries" writes the
recap): the recap still runs on the titles choice unless the person splits it.

Why separate roles: filing is a one-label classification made per chat, often;
suggesting is one judgement over a group, rarely, and decides whether a place exists.
Titles, work names and recaps are billed on different rhythms and a person may want a
better writer for recaps than for three-word names.

## 2. The grouping algorithm

Order of every decision: **reuse, rules, then one bounded model call.**

### Filing one chat (`Recommender.FileChat`)

1. No offer before the first settled reply, when filing offers are off, or for a chat
   already weighed (one weighing and at most one offer per chat, design 6e).
2. A chat that says too little (no folder and fewer than 3 meaningful words) is skipped.
3. Rules score every active place the chat is not in: a folder or repo source holding the
   chat's folder scores 95; a place whose chats mostly share the folder scores 85; shared
   words score at most 70, which is below the default threshold — **a shared word is a
   reason to ask, never an answer.**
4. Folder match → offer with **no model call**. No candidates → nothing, no call.
5. Otherwise the top `maxCandidates` (5) places are shown to `placefile` as labels
   `p1…p5`; the answer must be `{"place": "<label|none>", "confidence": 0-100}`.
6. Offers below `minConfidence` (75) are not made. Only existing places are ever offered.

### Organizing chats in no place (`Recommender.Organize`)

1. Merge offers (no call): active siblings whose names fold to the same words (case,
   spacing, punctuation, plural) — the older, or the person's own over an AI-created one,
   is kept.
2. Fewer than `minClusterChats` (5, **design**) unplaced chats → nothing, no call.
3. Groups: chats sharing a folder first; the rest by word overlap (single-link, Jaccard
   ≥ 0.34 with ≥ 2 shared words), kept only if a core word is shared by ≥ 60% of the group.
   At most 3 groups per pass, largest first.
4. **Reuse first:** a group that a place already holds by rule becomes a *move* offer.
5. A folder group is offered a new top-level place named after the folder, no call.
6. Otherwise `placesuggest` is shown the chats (`c…`, at most 24), the existing places
   that might fit (`p…`) and the parents a new place may go under (`u…`, only those the
   caps allow). Answer: `{"belong", "chats", "use", "name", "under", "confidence"}`.
   A group the model says does not belong is not asked about again for the snooze period.

### Why not a place per chat or per message

Most chats are quick and belong nowhere (design: quick chats stay unplaced). A place per
chat would bury the dozen that matter under hundreds, and a model reading one message
cannot know whether it is the first of fifty or the only one. A new place is therefore
offered only for a *group* that rules have already found, and only when the caps allow.

## 3. Guarantees

- **Nothing reorganises without a yes.** Create, move and merge are always proposals. The
  one automatic action is filing into an *existing* place when `autoFile` is on (off by
  default) and the offer is at least `autoFileConfidence` (90) sure; it is one membership
  marked `addedBy: ai`, undoable.
- **No model output becomes an id, path or permission.** The model answers labels; an
  unknown label rejects the whole answer. The exact name of exactly one place it was shown
  counts as that place's label (deepseek/deepseek-v4.1-flash answered "Release pipeline"
  for "p1" in the first live run, and every such answer was being refused); a name it was
  not shown, or one two shown places share, is still refused. The one free text it writes, a new place's
  name, must pass the graph's name rules, be ≤ 40 characters, contain no `/ \ : * ? < > |`,
  no URL, no leading dot. A place created from an offer has **no sources and no policy**,
  so accepting can never widen what a chat may read or do.
- **AI failure keeps the current organisation.** Transport errors, malformed JSON, unknown
  labels, low confidence: no offer, no write. An accept that fails part-way undoes its own
  steps.
- **Idempotent.** An offer's id is derived from what it is about (the chat, the sorted
  group, the pair of places). Asking again returns the same offer. A group overlapping an
  open, accepted, declined-in-snooze or rejected-in-snooze group by half is not offered.
- **Accept re-checks against the graph now.** Gone places, chats filed by hand meanwhile,
  or a cap that filled close the offer (`ErrProposalGone`, `ErrPolicyLimit`) with nothing
  changed. A person may rename a new place and narrow its chats; never widen them.
- **A person's own places are never limited.** Caps count only places created by
  accepting an offer (tracked in the ledger, not inferred from the graph).
- **Cycles.** New places attach only to existing parents chosen from offered labels; merges
  go through `Store.MergePlaces`, which refuses to leave a cycle.

## 4. Policy settings and defaults

Persisted per profile as `desktop.places.<key>`; out-of-range writes are refused; a
hand-edited file is clamped to the bounds.

| Key | Default | Bounds | Source |
| --- | --- | --- | --- |
| `filingOffers` | on | — | **design** (one offer after first reply) |
| `clusterOffers` | on | — | **design** (suggest when ~5 cluster) |
| `mergeOffers` | on | — | provisional |
| `autoFile` | **off** | — | provisional |
| `minClusterChats` | 5 | 3–50 | **design** |
| `minConfidence` | 75% | 50–100 | provisional |
| `autoFileConfidence` | 90% | 60–100 | provisional |
| `maxAiTopLevel` | 6 | 0–50 | provisional |
| `maxAiSiblings` | 8 | 0–100 | provisional |
| `maxAiDepth` | 3 | 1–6 | **design** (depth 2–3) |
| `maxAiPlaces` | 30 | 0–500 | provisional |
| `maxPending` | 3 | 1–20 | provisional (filing offers are per chat, not counted) |
| `declineSnoozeDays` | 30 | 1–365 | **design** ("Not now" hides 30 days) |
| `maxCandidates` | 5 | 2–12 | provisional |
| `filingCallsPerDay` | 40 | 0–1000 | provisional |
| `clusterCallsPerDay` | 6 | 0–200 | provisional |
| `organizeEveryMinutes` | 60 | 5–1440 | provisional |

Related, owned elsewhere: the ≥3-tab group suggestion in the shell
(`t-d5-sh-group-suggest-model`).

### Critical cleanup policy: untouched places (design 6d, 6e) — NOT a setting

Design 6d: "Places untouched for 60 days get a quiet suggestion to merge or archive them,
on Home and in ⌘P." Interactions: "Not now hides it for 30 days." Both figures are the
designer's own, so they are **constants, not rows of the table above and not Settings
knobs**: `placegraph.StaleAfterDays = 60` and `placegraph.StaleSnoozeDays = 30`
(`internal/placegraph/stale.go`). `declineSnoozeDays` does not govern this suggestion; it
snoozes the model's offers, and the two may be tuned separately only by editing the code
and this page together. (This page used to say this rule lived in
`internal/placegraph/suggest.go`; that file never existed and no such rule ran before.)

- **Untouched** means no sign of life for 60 days, counted in 24-hour blocks and inclusive
  at the boundary: the newest of the place's creation, the last time anyone went to it,
  the last time the person spoke in a chat filed in it, and the same three for any active
  place under it. A place with none of those dates is never suggested.
- **Never suggested:** archived places, pinned places, a place with work running or a chat
  waiting on the person in it or under it, and a place whose "Not now" is still running.
- **Not now** is written by the engine to `places-stale.json` beside the graph, so every
  window agrees and a reload keeps it. It ends exactly 30 days later; an ended snooze is
  dropped on the next write. It is not a structural change: no revision, no receipt, no Undo.
- **No model is called.** It is date arithmetic on the bridge's clock; tests inject the
  clock and pin the boundaries to the nanosecond.
- **Nothing happens by itself.** The line offers Merge and Archive (the place menu's own
  writes, with their receipts and Undo) and Not now; it never archives, merges or deletes.
- **Routes:** `GET /places/stale` (longest untouched first, at most 20) and
  `POST /places/{id}/stale-snooze`. Both are absent (404) on a bridge with no snooze file.
- **Where it appears:** design 6d says "on Home and in ⌘P". "Home" here is the root Home,
  **All places**, and nowhere else: no individual place's Home and no other surface draws
  the line, and that is intentional (the suggestion is about the set of places, not about
  one place's contents). The other door is the ⌘P sheet, shown only while its search is empty.
- **Across windows:** a snooze moves no graph revision and sends no world broadcast, so the
  graph reading cannot carry it. The supported contract: another window drops the line when
  it regains focus or becomes visible, and within 30 seconds (`STALE_REREAD_MS`,
  `useStale.ts`) while it is visible and a line is on screen. A hidden window reads nothing;
  a window with no line does not poll. Not a setting. Tested with two windows in
  `desktop/tests/ui/stale-places.spec.ts`.
- **A damaged snooze file** (bad JSON, unknown version, bad id or times, duplicates, more than
  `MaxStaleSnoozes` records, or over about 600 KB) reads as no snoozes: at worst a suggestion
  repeats. The next "Not now" keeps the bytes aside as `.damaged-<unix>` and starts fresh.

## 5. What is wired and what is not

| Piece | State |
| --- | --- |
| Policy, recommender, ledger (`places-ai.json` beside the graph) | built and tested |
| Settings page sections, live/inherits lines, policy rows | built; policy rows read and write `/places/policy` |
| `/models/roles` `category`, `live`, `inherits`, `categories` | built |
| Engine model door for the place roles (`internal/session/placeadvice.go`, `Agent.AskPlaces`) | built; registers `placefile` and `placesuggest` on the low tier, so both rows are live |
| The wire door (`Places.Ask`, `Welcome.PlaceAsk`, `internal/remote/placeask.go`) | built; the bridge asks through an open conversation's engine, which answers on the role's own model and bills it as a background errand |
| `/places/policy`, `/places/proposals` routes and the background scheduler (`internal/desktopbridge/places_policy.go`, `places_advice.go`) | built (see below) |
| UI that shows offers (suggestion line/card) | the UI lane's; nothing is drawn yet |

**When the bridge asks.** Three triggers, none of them a read:

- a turn of a conversation the bridge holds **settles** (the engine said the turn was
  done): that chat is weighed for an existing place eight seconds later, once ever;
- the Home page **asks** with `POST /places/proposals/organize` (once per opening; a
  second request inside a minute is not queued);
- the bridge has been **idle** (no turn running anywhere) for `organizeEveryMinutes`.

`GET /places/proposals` and an SSE reconnect never ask a model. One job runs at a time
per bridge; at most 32 chats wait to be weighed; every ask has a 60-second deadline and
is cancelled when the bridge closes.

**With no conversation tab open** — the ordinary state at app start, when Home shows —
the ask rides ONE reading connection (`Hello.Watch`) onto an existing saved conversation
of this desktop's workspace: one the engine host already holds, else the newest one
somebody spoke in. A reader never drives, is refused every write, starts no turn and is
never `New`, so it cannot mint a conversation; a welcome naming any other transcript is
refused. It opens only when a job actually asks (a job the rules answer opens nothing),
is cached once, and closes after two idle minutes, on a failed ask and with the bridge;
a failed attach waits five minutes. The engine's watcher allow-list carries
`Places.Ask` by name and no other model ask. An empty library opens and asks nothing.
The call is billed to that conversation's background errands.

**Evidence** is the conversation's own title, its first message, its settled replies,
its saved **recap** (the Summaries role's `recap.line` and `recap.discussed` from
`meta.json`, at most 600 characters) and its folder — except the folder every desktop
conversation runs in (the bridge's `--workspace`), which says nothing about what a chat is
and would make the folder rule offer every chat the same place. Nothing is written or
asked for here: no recap is no summary, an unreadable `meta.json` is no recap, and a stale
recap is still the conversation's own account of what it covers. A recap of an exchange
(two or more messages) proves one reply and never more.

**Grouping chats in no place.** Measured on thirteen real chats whose titles and recaps
deepseek/deepseek-v4.1-flash wrote (`internal/placegraph/testdata/`), the pairwise rule
(shared words ≥ a third) linked none of the twenty same-group pairs, and adding recap text
made it worse. No threshold was moved. Chats now also group around ONE topic word that
reaches at least `minClusterChats` of them (title, recap or opening message) and that at
least half of them carry in their TITLE — so a recap can bring in a chat titled another
way, while a recap's own vocabulary ("discussed", "recommended") anchors nothing. The
pairwise rule and the words matched against place names read titles and opening messages
only. "Wi-Fi" counts as "wifi". On that corpus the garden group of five is found and
nothing else; the five Wi-Fi chats share no single topic word (one never says Wi-Fi or
router), so they are not offered — the model is never asked about a group the rules
cannot see.

**Accepting** may carry the `offerVersion` the window read; an offer that changed since
is refused (`409 stale_offer`), one already decided or overtaken by the graph is `409
gone`, and a cap that filled is `409 policy_limit`. Declining is `POST /places/proposals/{id}/decline`.

## 6. Questions for the designer

1. Is `autoFile` ever acceptable, and if so is 90% the bar?
2. Caps: 6 top-level / 8 per parent / 30 total AI-created places — right scale for
   "5–15 active, 20–200 total"?
3. Should a declined group stay quiet for 30 days even if it doubles in size?
4. Where do Places organization settings live in the final Settings: global, or per Place?
5. Should "Summaries" default to following "Chat titles" (current) or have its own default?
