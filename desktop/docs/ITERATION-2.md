# Iteration 2: decisions, knowledge and attention

Task: `t-d6i2-qa-docs`. Design authority: v3 `codeaf Iteration 2.dc.html`
(I2.1–I2.14), `codeaf Decisions.dc.html` (12a–12e), Components I2 and
Interactions I2. These rules supersede conflicting iteration-1 text in
[PLACES-ARCHITECTURE.md](PLACES-ARCHITECTURE.md),
[the tab architecture](../src/features/tabs/ARCHITECTURE.md) and
[DESIGN-COVERAGE.md](DESIGN-COVERAGE.md). The design files are read-only.
This document describes the target contract and its implementation seams;
a seam or a passing component test does not establish end-to-end availability.

## Attention and navigation: I2.1–I2.6, I2.11

The Inbox rail row and tab are superseded by Next up (I2.1). The frame owns one
“N need you elsewhere · ⌘J” pill, outside every tab. It excludes the current
conversation and disappears at zero. Hover lists pending questions; click or
Next up opens each question in its actual conversation, with its tray open.
Blocking questions come first, irreversible questions last, then age within each
class. Skip moves a question to the back without answering. The walk ends with
“You're clear”. Accept N suggestions covers reversible, non-costly questions
with a suggested answer; the current implementation delays sending until the
Undo toast expires. Closed-but-running work belongs under Live on its place Home.

The chat header counts this conversation and its tasks only (“needs you here”).
Next up holds what places could not decide: irreversible work, low confidence,
capped discussions and always-ask questions. Removing a question requires a real
recorded answer; confidence alone never clears attention.

A new blocking question elsewhere draws a four-second frame banner. Background
system notifications cover blocking questions only; activation starts Next up at
that exact question. The dock badge counts total needs-you, including the current
conversation, rather than borrowing the pill's elsewhere count.

Focus history records foreground tab switches, place switches and in-tab drills,
not background opens. Back/forward restore place, tab, scroll and draft. After an
unrequested jump, a temporary strip chip offers “Back to …” until the next
self-directed action. A drill has its own parent chip inside the tab header.
Neither scope adds permanent back/forward buttons.

## Shortcut table: I2.4–I2.6

Labels and matching live in `src/design/keyboard.ts`; surface handlers run before
workspace and app handlers. Fields retain caret movement and overlays own Escape.

| Action / scope | macOS | Linux / Windows |
| --- | --- | --- |
| Next up, window | ⌘J | Ctrl+J |
| Focus history back / forward | ⌘[ / ⌘] | Alt+Left / Alt+Right |
| Up one level, place Home outside a field | ⌘↑ | Ctrl+Up |
| Previous / next message, chat outside a field | ⌘↑ / ⌘↓ | Ctrl+Up / Ctrl+Down |
| Leave Next up / close active overlay | Esc | Esc |
| Place Home | ⌘0 | Ctrl+0 |
| Go to / All places | ⌘P / ⌘⇧P | Ctrl+P / Ctrl+Shift+P |

⌘I is removed; the Inbox shortcut is superseded (I2.6). ⌘[ is focus history,
never Home's parent action. Control+Tab remains tab switching on every platform.

## Decide gate and learning: I2.7–I2.8, I2.14

The design permits place decisions about questions, permissions, task starts and
stops, reprioritising, steering chats and promoting memory. Each verb still needs
its real engine operation and receipt; eligibility is not execution support.

The session gate runs before a question becomes visible. It resolves the chat's
real place membership, checks that the question still exists, applies ask mode,
stakes and confidence, then records and resolves an allowed automatic answer once.
Irreversible work always goes to the person; always-ask and explicit policy refusals
also preserve the question. Unfiled chats cannot borrow an unrelated place's trust.

`internal/decide` owns confidence, per-kind learning, escalation and the durable
ledger. The threshold defaults to 90 percent, inherits through the graph and can
be set per place. Multi-place routing starts at the nearest shared ancestor;
parent traversal is ordered, cycle-safe and bounded to three parent hops before
returning to the person. A place thinks only when a question reaches it; no
renderer timer or Home visit triggers a model call. The live gate calls graph escalation after a deciding place falls below its
threshold. Judgements, confirmations, landing and clarification questions remain
personal by their own session policy; the target design does not remove that
current refusal.

New kinds propose instead of deciding. The proposed key is selected in the normal
tray or Next up, with measured confidence and reason when supplied. There is no
Home proposal checklist. A kind graduates only after 20 personal answers exist
and at least 18 of the last 20 agreed. Automatic answers do not count as personal
agreements. Overturn resets only that kind's learning window; other kinds keep
their trust. Always ask me remains a separate person-controlled setting.

## Receipts and reaching beyond a chat: I2.9, I2.12, I2.14

Every automatic answer records its origin, reason, confidence, action, question
identity and reversibility. The transcript places “Allowed automatically by … ·
Why?” below the permission it answered; other decisions say “Decided”. Unknown
reasons or percentages draw nothing. Consecutive actions share “Did N things”,
expandable to the individual receipts. Persisted receipts survive replay.
Place Home lists them under “Decided automatically”, newest first.

Why? exposes By, Because and Sure. The design calls for Overturn plus “Next time:
always ask me / keep deciding”. Overturn lists dependent work and offers to notify,
pause or leave it; it never silently cascades. Missing engine reversal callbacks
must refuse rather than display a successful reversal. The transcript integration
currently exposes the explanation but does not wire the overturn controls;
`desktop-transcript-decisions.md` and `desktop-overturn-flow.md` document this limit.

There is one way to talk: an ordinary chat, including a chat started from Home.
Work inside the chat can act directly; reaching another chat, task or place first
shows a plan with Go, Edit, Cancel. Execution uses stable target ids and is
idempotent. Closed chats remain targets; deleted targets produce a skipped receipt.
Undo is offered only for steps with actual reversal support, never invented for a
sent note or a stopped task.

## What a place knows: I2.13–I2.14

Instructions, notes, memories and learned rules share the editable “What [place]
knows” list, with provenance for each line. Home shows three lines and All expands
in place. Remember in a filed chat saves to its place with an exact Undo receipt;
explicit general-memory scopes retain their own behavior. `placegraph.Resolve`
feeds both the engine context and Using, excluding replaced lines. Updates apply
at the next turn, without rewriting a running turn's context.

Newer contradictory knowledge wins; the replaced line is struck through for seven
days. Two personal statements from the same day ask once. Duplicate lines merge;
sixty unused days makes a line eligible for “still true?”. Confirming it restarts
that interval. Learned lines expose their answer evidence and remain editable.
The bridge replacement operation names the superseded line: it does not infer
contradictions from arbitrary prose. Home mutations require real owner callbacks;
without them the controls are absent.

## Councils: I2.10, I2.14

A discussion is an ordinary chat named “[Place] with [Place]”, filed in both
places, with messages labelled by place. It appears under Live while running,
then Recent and History. Typing pauses it until sending, which steers the
conversation. A settled outcome has one Decided card with only recorded turns,
cost and filing facts.

`internal/council` owns the discussion record and runner. Each discussion caps at
six place turns and $0.25, then escalates rather than fabricating an outcome. A
running or paused pair/topic cannot reopen; after it ends, the same pair/topic
cannot reopen until one hour after its original opening. Parent routing or the
person receives escalation through the caller's supplied seam. A renderer council
component alone does not prove the runner or escalation is attached.

## Architecture and evidence

| Owner | Source of truth / seam | Evidence to inspect |
| --- | --- | --- |
| Session gate | `internal/session/decide_hook.go`, `decide_wire.go`; `internal/decide/` | `decide_hook_test.go`, `decide_wire_test.go`; confidence, learning, escalation and overturn tests |
| Desktop decision reads/writes | `internal/desktopbridge/decisions.go`, `decide_attention.go`; `features/decisions/`; typed `engine-client.ts` | `decisions_flow_test.go`, `decisions_test.go`, `tests/ui/decisions.spec.ts` |
| Knowledge | `internal/placegraph/knows*.go`, bridge `knows_routes.go`, `remember_knows.go`; `features/places/knows/` | knowledge store/rules tests, bridge route tests, `KnowsList.test.tsx` |
| Discussions | `internal/council/`, bridge `council_routes.go`; `features/council/` | store/runner and route tests, `tests/ui/council-view.spec.ts` |
| Window attention/navigation | shell providers, `features/nextup/`, `features/focus-history/`, `design/keyboard.ts`; world attention stream | `tests/ui/nextup-walk.spec.ts`, `strip-nextup-seam.spec.ts`, keyboard tests |

The engine owns decisions, knowledge and question identity. The bridge exposes
narrow authenticated routes; Rust remains a native notification/window adapter.
Mocks are acceptance fixtures only. The renderer neither stores provider keys nor
invents decisions, receipts, counts or work. Values remain in `tokens.json`, icons
use `Icon.tsx`, and shared controls own hover, press and keyboard focus behavior.

## Manual and coverage

`internal/manual/chat/desktop-iteration-two.md` is the searchable overview, with
its own probes in `internal/manual/chat_desktop_iteration_two_test.go`. Feature
pages remain authoritative for live limits; shared desktop pages and the shared
probe table are deliberately untouched by this lane.

The I2 override table in [DESIGN-COVERAGE.md](DESIGN-COVERAGE.md) records the
superseded d5 Inbox presentation tasks and maps their surviving obligations to Next up and Home.
Historical totals are not recomputed into current acceptance counts. No new design
assumption is introduced by this documentation task; no Open ledger row is needed.
