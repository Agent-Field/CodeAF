# aforge — the product, the journey, and how we know it works

## The pitch

aforge is not a coding CLI — it is a **resident**. Session tools (Claude Code,
Codex CLI) give you an agent that lives exactly as long as a terminal window
and forgets when it closes; you fan out terminals to fan out work. aforge
inverts every one of those choices. There is one brain, it is always the same
brain, and it remembers:

- **A permanent task graph instead of a chat scroll.** Work is durable,
  inspectable, resumable, and survives every restart.
- **It works while the terminal is closed.** Standing watches run from a
  system timer; overnight jobs land and are announced to whoever is home.
- **It learns.** Corrections become lessons in a notebook that outlives every
  job; repeated workflows compile into reusable craft (a YAML DSL in a git
  repo); its beliefs age, get contradicted, and get corrected in the open.
- **Money is visible.** Costs ride the header, big plans are priced before
  they are bought, spend is attributable per job and per day.
- **It gets better at your work**, measurably, because every job feeds the
  next one.

The unit of the product is not a session. It is a working relationship.

## The one law

**The thread is the only mouth; every other surface is eyes.**

Anything a user wants done, changed, stopped, taught, or asked is *said* in
the thread, and the head routes it. The board, the self page, and the graph
exist to be looked at. Any flow that requires navigating somewhere to *act*
is a product bug. The inverse holds too: everything the system does in
response to a user's words must end in exactly one visible reply in the
thread — silence wearing a message id is the cardinal UX sin.

## The journey

One window, one conversation, forever. `aforge` opens into the thread you
left (session resumes by default; `/new` starts fresh). Parallelism lives in
the graph, not in terminal tabs: say five things and five jobs run; the board
shows them side by side. Additional windows are honest mirrors of the same
brain — a second window says "second window", follows the same thread, and
promotes itself seamlessly when the first closes or a newer build asks for
the room. They are for a laptop-and-desk life, not for fanning out work.

## What a human can do (the catalog)

Everything below is a sentence typed (or dictated) into the thread, unless
noted. This catalog is the contract the UX tests verify.

| # | Journey | What the user does | What must happen |
|---|---------|--------------------|------------------|
| 1 | Ask a small thing | "what's 2^32?" / "summarize this file" | Direct answer in thread, no job ceremony |
| 2 | Commission work | "build me X" | Work order journaled; tasks appear on board; deliverable lands in thread as the final message, answer-first |
| 3 | Watch progress | glance at board; or ask "how's it going" | Status is a read of the live graph — including split/continued work — never a stale summary in present tense |
| 4 | Steer running work | "stop over-studying, move faster" | Redirect reaches running workers; one reply says what was done ("passed to the two running workers"); words arriving after landing are called out, not eaten |
| 5 | Correct delivered work | "that's wrong — the totals are off" | Revision spawns beside the original, inherits its workspace, distrusts the disputed claim |
| 6 | Answer its questions | type the number, arrows+enter, or click | Answer submits, appears as the user's message instantly, receipt lands in thread |
| 7 | Teach a lesson | "always verify against live data before claiming done" | Lesson lands in the notebook (stated fact); future jobs see it |
| 8 | Stand up a rule | "whenever X happens, do Y" / "remind me…" | Charter drafted, priced (per-firing and worst-day), ratified with one answer; pause/retire/stand-down equally sayable |
| 9 | Approve spend | a plan over the consent gate (`plan_consent_usd`, [LIMITS.md](LIMITS.md)) | Price shown before purchase; consent asked; refusal honored |
| 10 | Ask about money | "what did that cost?" / header glance | Per-job and windowed spend readable in thread; costs in header |
| 11 | Ask what it learned | "what did you learn this week?" | Read over the notebook/self — lessons, beliefs, retractions |
| 12 | Leave and return | close terminal, come back later | Same thread resumes; a brief covers what landed while away; overnight deliverables were announced to whoever was home |
| 13 | Second window | open aforge elsewhere | "second window" named in header; same thread; promotion on first window's exit; version handover on rebuild |
| 14 | Repeat a workflow | commission similar work again | Craft learned from the first run compiles into the second (visible: craft repo commit, faster/cheaper run) |
| 15 | Watch it learn on its own | idle time | Practice/curiosity runs appear under territory; self page shows measured self-knowledge, not vibes |
| 16 | Choose models in words | "use kimi for this" / "boost this" | Model words route; boost lane engages; recorded on usage |
| 17 | Attach things | mention a file/screenshot | Copied at mention (CAS), survives source changes; vision fallback reads images |
| 18 | Take things out | y/Y copy, /open, clickable paths | Deliverables exit the terminal without friction |
| 19 | Ask what it can do | "what can you do?" | The product carries its own pitch: journeys, in its own voice, from its live capabilities |

## The interaction grammar (what we deliberately do NOT ask of users)

- No per-task terminals. No navigating into the graph to speak to a task.
- No commands to memorize: `/new`, `/open`, `?` exist; everything else is
  language. Cue laws are few, frozen, and documented.
- No configuration ceremony: models are words, rules are charters, budgets
  are sentences.
- No trust-me claims: "verified" must carry evidence; refusals and failures
  are said in the thread, not logged into silence.

## The quick-access answer (taskbar / always-open)

The terminal is the surface today, and the design answer to "I want it one
keystroke away" is: aforge must be **excellent as a docked, narrow,
always-open pane** — a slim tmux/terminal split kept on the side, where the
thread stays readable at 60 columns, the header stays honest, and returning
attention costs zero (attach brief, resumed session, instant launch). A
native menu-bar/web surface is a future wave (the web surface exists on an
unmerged branch); nothing in the core may assume wide terminals.

## The design filter (every proposal passes this or dies)

Apple's discipline, applied: **no new nouns, no new modes, no ceremony.**

- A capability ships as *behavior*, never as *terminology*. If explaining a
  feature requires teaching a user a word we invented, the design is wrong.
  (Internally we may speak of focus, lenses, charters, craft; the user only
  ever experiences "it understood which one I meant", "I can see just that
  conversation", "it remembers my rule", "it's gotten faster at this".)
- Ambiguity is resolved the human way: it asks one short question, in plain
  words, at most once. Never a picker UI, never an error, never a mode.
- Every addition must remove more confusion than it adds. Bloat is measured
  at the surface: if the default screen gains a permanent element, the bar
  is "would a first-time user be confused for even a second?"
- Defaults over settings. Behavior over configuration. Sentences over
  commands. The product should feel inevitable, not featureful.

## How we verify (the UX test doctrine)

Journeys are tested **as a human** — by driving the real binary in a real
terminal (tmux), against a real provider, and verifying both what the screen
showed and what the journal recorded. A journey passes only when:

1. The screen showed the expected interaction (captured pane state), and
2. The journal holds the expected durable evidence (messages, commands,
   facts, charters, craft commits, usage rows), and
3. Nothing else happened — no silent extra spend, no orphaned questions, no
   unanswered user words.

The executable suite lives in `test/ux/`. Each journey above maps to one
script; the suite runs on a disposable store with a spend cap and the
cheapest capable models, and its report is written for a human reading
"which journeys are true today."
