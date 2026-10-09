# AI roles and the Places recommendation policy

Status: implemented as a library and a provisional Settings page, 2026-10-09. The
recommender is not yet connected to a live engine call or to HTTP routes; see
"What is wired and what is not" before relying on any of it. Every default not marked
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
| Places organization | `placefiling` — Chat filing | `placefile` | no |
| | `placesuggest` — Place suggestions | `placesuggest` | no |
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
  unknown label rejects the whole answer. The one free text it writes, a new place's
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

Related, owned elsewhere: the 60-day idle "merge or archive" suggestion and its 30-day
snooze (`t-d5-be-pg-suggest-stale`, `internal/placegraph/suggest.go`); the ≥3-tab group
suggestion in the shell (`t-d5-sh-group-suggest-model`).

## 5. What is wired and what is not

| Piece | State |
| --- | --- |
| Policy, recommender, ledger (`places-ai.json` beside the graph) | built and tested |
| Settings page sections, live/inherits lines, policy rows | built; policy rows show "cannot be changed from this engine yet" until `/places/policy` exists |
| `/models/roles` `category`, `live`, `inherits`, `categories` | built |
| Engine model door for the place roles (`Agent.AskPlaces`) | patch ready, not committed: registering it would mark the rows live before anything calls them |
| `/places/policy`, `/places/proposals` routes, bridge `Recommender` | Places-routes lane / root |
| UI that shows offers (suggestion line/card) | not built; nothing is drawn |

## 6. Questions for the designer

1. Is `autoFile` ever acceptable, and if so is 90% the bar?
2. Caps: 6 top-level / 8 per parent / 30 total AI-created places — right scale for
   "5–15 active, 20–200 total"?
3. Should a declined group stay quiet for 30 days even if it doubles in size?
4. Where do Places organization settings live in the final Settings: global, or per Place?
5. Should "Summaries" default to following "Chat titles" (current) or have its own default?
