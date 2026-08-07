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

## Decision 5 — Autonomy is earned after ratification

Ratifying a charter authorizes the standing responsibility, not unattended
execution on day one. Every new charter begins on **probation**. When its
sentinel says yes, the thread shows what it would do and offers four durable
choices: approve this firing, not now, always allow, or never. Approval admits work
through the ordinary command queue and firing journal; the proposal itself
never compiles, plans, or splices work.

Three consecutive approved firings must independently finish green and within
the charter's per-firing rail before the charter becomes **tenured**. The
threshold is configurable with `AFORGE_TENURE_AFTER`. Always allow is an
explicit journaled override. A failed or cancelled subtree, a rejected output,
or a per-firing budget breach returns a tenured charter to probation; a second
failure demotion pauses it. “Back to asking” is the conversational, non-failure
reversion to probation.

## Decision 6 — Spend is visible and settable where you talk

`/budget` shows today: `spent $3.40 of $20 · resets midnight` and sets:
`/budget 50` (today's ceiling — a journaled raise), `/budget default 35`
(persisted config), `/budget unlimited today`. The same numbers live in
the charter cards (per-firing quotes) and the header's cost meter. Config
file and `AFORGE_DAILY_BUDGET` remain for headless; the slash command is
the conversational spelling of the same journaled events.

## Decision 7 — Presence when no terminal is open, asked exactly once

A standing goal that only fires while a window happens to be open is a
promise the system cannot keep. So the first ratified charter — and only
the first — earns one question in the resident's voice: "Should I keep
watching this when you're not here? `▸ 1 yes, always · ▸ 2 only while I'm
around`". Yes arranges a five-minute `aforge wake` for this user account
and answers with a single line; no is remembered as a decline. Both
answers are journal events, so the never-ask-twice gate survives
restarts, rebuilds, and every later charter.

The mechanism is deliberately invisible: the user never reads the words
daemon, launchd, or systemd — only the consequence ("a quiet check every
few minutes, even with no terminal open"). Three properties make it
honest rather than decorative:

- **The choice is durable before the host is touched.** "Yes" is written
  first, then the timer is arranged. A crash or a host that refuses today
  becomes a repair on the next resident check, never a lost yes.
- **Status is derived, not asserted.** Installed means the definition file
  on disk still matches byte-for-byte what this build would write; last
  wake and next check come from journalled wake events plus the fixed
  cadence. Nothing shells out to ask.
- **Ratification never fails for the offer's sake.** The charter is
  already active when the offer is attempted; a failure to post it is
  logged, not surfaced as a rejected ratification.

`aforge doctor` prints the same five rows this reasoning produces — brain,
resident, standing watch, spend, standing work — and those rows are the
grounding the head answers "who's keeping watch?" from.

## What this is not

- Not a scheduler UI. No cron-syntax dialogs; cadence is stated in words
  and echoed back in words ("every weekday at 9" → `cron` internally).
- Not a rules engine. The sentinel is judgment, not pattern-matching — a
  broad watch with a smart sentinel beats a precise trigger with none.
- Not push-notification spam. Firings obey the same interruption budget
  as everything else; the daily rail bounds the worst day by construction.
