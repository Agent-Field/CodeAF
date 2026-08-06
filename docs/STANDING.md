# Standing goals — recognition, ratification, and ambient presence

M3's mechanism is settled (ARCHITECTURE.md Decision 5: watches, sentinels,
rails). This document settles its *interface*, because the interface is the
feature: a standing responsibility you have to ceremonially declare is one
you will forget to declare. The governing critique (user's, of a `/goal`
command elsewhere): "anything I give, I expect to be a goal — I may not know
when to give goal and when not." Classification of one's own words is the
machine's job.

## Decision 1 — Recognition, never declaration

There is no `/goal` command. The compiler — which already reads scale from
structure — gains a temporal reading: does this ask *end*, or does it
*stand*? "Whenever a PR opens…", "remind me when…", "keep the suite green",
"every morning…", "watch this folder" — durable intent is recognized from
ordinary words, exactly as project-vs-task already is. An episodic ask
compiles to a job; a standing ask compiles to a **charter**.

**Rejected:** a mode, a command, a special syntax. The user speaks; the
system classifies; misreads are corrected in conversation like any other
compiler assumption.

## Decision 2 — The charter, and the one question that is always asked

A charter is the compiled form of standing intent:

- **invariant** — the user's verbatim words, permanent anchor
- **watch** — what wakes it: `cron:` schedule | `file:` glob | `graph:`
  predicate (node settled/failed, spend threshold) | `poll:` a checked
  condition (PRs, URLs) on a cadence
- **sentinel** — the cheap wake-time judgment: "is the invariant
  threatened / has the condition occurred?" — one small model call, so a
  watch can be broad and the action still precise
- **action** — what a firing splices (re-grounded at fire time; the world
  moves)
- **rails** — per-firing budget quote, firing-rate limit, expiry. Mandatory
  by construction; firings draw from the daily dollar rail like all work.

Everything else in aforge assumes-and-declares. A charter is the one
exception: **standing = spending forever**, which crosses the consequence
gate, so ratification is explicit — the compiler plays back the charter as
a card and asks once. The ask is *selectable*, not an essay question:

    watch PRs on Agent-Field/aforge — review each new one, post a summary
    fires: on new PR (polled ~2m)   costs: ~$0.15/firing, ≤10/day   expires: never
    ▸ 1 yes, stand this up      ▸ 2 change the cadence      ▸ 3 once, not standing

Options + free text; up/down + enter or a number key; the same interactive
option pattern becomes available to *every* compiler askback (the generic
"meta-prompting" surface — questions arrive with choices whenever choices
are enumerable, because picking beats composing).

## Decision 3 — Ambient presence: felt, not seen

Standing goals are furniture, not conversation. Apple's ambient rule:
visible exactly when relevant, otherwise one quiet line.

- The rail gains a **standing** section above tasks: one dim line per
  charter — `⏱ pr-watch · last fired 2h · 3 today`. Breathing only while a
  sentinel is evaluating or a firing is active.
- A **firing is an ordinary job**: it splices with `origin: trigger`
  pointing at its charter, gets a normal card in the thread (dock while
  running, settles in place) — but *charter-born cards land in the thread
  only when they carry a question, a delivery worth reading, or a failure*;
  routine "checked, nothing to do" firings stay in the rail and the
  journal. The interruption budget holds.
- Reminders are the degenerate charter (cron watch, expiry after one
  firing, action = say something): "remind me tomorrow at 9" just works,
  and the reminder arrives as an attention message.
- Management is conversational and clickable, never a config file: "stop
  watching PRs" retires it (the head recognizes charter references);
  clicking a standing line opens its charter card — history of firings,
  cost, pause / retire / edit-cadence as options. `/standing` lists; the
  card is the editor.

## Decision 4 — The system may propose, through the same door

The retrospective already detects recurring asks. A recurring pattern
(same-shaped job ≥3 times, or a recurring correction) may **propose** a
charter — rendered as the same ratification card, marked `proposed ·
noticed you ask this most mornings`, default-declined, never silently
armed. One proposal per reflection run; declining teaches (a declined
proposal is a notebook fact and is not re-proposed). This is 3.6's
"standing goals become learned" landing through the only legitimate gate:
the user's explicit yes.

## Decision 5 — Spend is visible and settable where you talk

`/budget` shows today: `spent $3.40 of $20 · resets midnight` and sets:
`/budget 50` (today's ceiling — a journaled raise), `/budget default 35`
(persisted config), `/budget unlimited today`. The same numbers live in
the charter cards (per-firing quotes) and the header's cost meter. Config
file and `AFORGE_DAILY_BUDGET` remain for headless; the slash command is
the conversational spelling of the same journaled events.

## What this is not

- Not a scheduler UI. No cron-syntax dialogs; cadence is stated in words
  and echoed back in words ("every weekday at 9" → `cron` internally).
- Not a rules engine. The sentinel is judgment, not pattern-matching — a
  broad watch with a smart sentinel beats a precise trigger with none.
- Not push-notification spam. Firings obey the same interruption budget
  as everything else; the daily rail bounds the worst day by construction.
