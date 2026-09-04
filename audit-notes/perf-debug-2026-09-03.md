# Aforge v3 chat — live UX/orchestration audit (2026-09-03)

Branch: `santosh/performance-update` (worktree `aforge-v2-perf`, base `origin/dev` @ 713945e3).
Method: drove the real v3 TUI in tmux as a user would (vague asks, interrupts, "undo",
"commit", "continue the failed task", "why a different folder"), chat model
deepseek-v4-flash-0731, crew balanced, with `AFORGE_DEBUG=1` + `AFORGE_CALL_LOG_BODIES=1`.

## TL;DR — the critical findings, ranked

CORRECTNESS / TRUST (fix first):
- F31/F32 Work is reported landed when it is ORPHANED — "task 5's commit edd9138 arrived
  as a merge result" but the commit is on no branch; `comeHome` can strand it and the
  chat still claims success. Confident wrongness a user cannot see without `git log`.
- F26 A trivial ask ("commit everything") was auto-escalated to a task and the commit
  NEVER happened — over-orchestration silently dropped the deliverable.
- F20/F21 A 429-rescued stream persisted MOJIBAKE as the assistant turn (and into the
  transcript); the hedge burned $ on 2 dead lanes and delivered a corrupted winner.

CONTINUITY (the "obvious" that's missing):
- F23/F25 A failed/finished task has NO first-class continue — "just continue task 4"
  spawned a fresh re-derivation, new brief, new worktree, instead of resuming context.
- F18 The chat spawned the SAME task twice (task 2 and task 3, both "error handling").

USER-MODEL MISMATCH (aforge vs Claude Code/Codex assumptions):
- F22/F30 The chat works in a hidden per-project copy, not the folder you launched from.
- F16/F26 The chat model — not a dedicated planner — decides WHEN to task and the brief,
  and does it on routine asks.
- F28 It asks the user to disambiguate "ground"/place instead of inferring the folder.
- F29 "undo that" did nothing and asked what shape I wanted.

COST / LATENCY (dominated by orchestration, not work):
- F1/F11/F14 System prompt ~15.5k + 31-34 tools/turn; spend is dominated by the
  mastermind (glm-5.3, $0.005-0.007/call), the checker (qwen3.8-27b) and duplicate fires.
- F13/F17 ONE interrupt fans out to ~6 calls incl. a 57-message re-read + 2 planner
  passes + duplicate title calls.
- F5/F6 Duplicate full-context fires and a speculative "reflex" model on most turns.
- F24 The debug call-log rotates after ~80 calls with bodies on (32MB cap) — too small
  to actually debug a real session.

## Test setup (verified)

- Binary: `aforge-v2-perf/bin/aforge` built from 713945e3 via `make build`.
- Isolated state root: `AFORGE_HOME=/tmp/aforge-perf-debug` (does NOT touch ~/.aforge).
- Isolated profile: `AFORGE_PROFILE_DIR=/tmp/aforge-perf-debug`.
- Chat model: `deepseek/deepseek-v4-flash-0731`; crew: `balanced` (the shipped default).
- Approvals: all `allow` in the isolated profile (no interactive gating).
- Scratch workspaces: `/tmp/aforge-perf-ws*` (tiny python repo, git-init'd).
- Debug record lands at `$AFORGE_HOME/logs/trace/<run-id>/`; per-call metrics at
  `$AFORGE_HOME/logs/calls.jsonl`; full request/response bodies need
  `AFORGE_CALL_LOG_BODIES=1`.

## Confirmed findings (each verified against logs before being written here)

### F1 — System prompt is ~15.5k tokens on the FIRST turn of a trivial task
- Evidence: run `50e814b451a67395` ("fix the bug in util.py"), first call
  `6dec07b6`: `prompt_tokens: 15571`, `messages: 2`, `tools: 31`.
- The whole task (find/read/edit/verify) cost ~15.6k prompt tokens per turn,
  growing only ~60-100 tokens per tool round-trip — so >99% of the context is
  the fixed system+tool preamble, not the work.
- 31 tools are offered to a chat model on every turn.

### F2 — Provider prompt caching is doing the heavy lifting; without it cost is 5x
- First call: 15571 prompt, cost $0.00125522 (no cache).
- Subsequent turns: `cached_tokens` ~15360-15872 of ~15.7-15.9k prompt, cost ~$0.00027.
- i.e. the design LEANS on server-side prefix caching; any cache miss (TTL,
  provider switch, lane change) re-bills the full 15.5k. Lane changed mid-run
  (DigitalOcean -> Parasail) and cache still hit, but this is fragile.

### F3 — A dead "reflex" call is fired and cancelled on a trivial task
- Run `50e814b451a67395`: `mistralai/mistral-nemo` tag=`reflex` errored with
  `decode stream: context canceled` (2000ms, ttft 1864ms).
- A second model was spun up speculatively and killed. Needs investigation:
  what is reflex for here, and why is it racing a trivial edit task?

### F4 — A separate untagged 2-message call (171 prompt tokens) runs after the turn
- `b36a9b5c`: 2 messages, no tools, 171 prompt / 61 completion — looks like a
  title/summary side-call on the chat model. Small, but it is an extra call per
  turn worth accounting for.

## Open questions to chase (not yet confirmed)
- Does the chat model propose tasks, and is there a separate planner? (user hypothesis)
- Token waste in tool-call compaction / worktree handling on bigger tasks.
- Time-to-first-token and idle gaps during interactive (tmux) use.
- What the 31 offered tools are and whether the set can be task-scoped.

## Log of runs
| run id | scenario | surface | notes |
|---|---|---|---|
| 50e814b451a67395 | fix util.py add() bug | `--once` | F1-F4 above |

## Interactive session in tmux (run see below), chat model deepseek-v4-flash-0731, crew balanced, YOLO

Session flow: fix util.py bug -> pytest tests -> memoize fibonacci -> "spin it off as a task".

### F5 — TWO identical chat-model calls fired at the same instant on a fresh session
- 14:33:04 twice: `turn` msg:2 prompt:15703 and prompt:15687 (85 and 52 reasoning tokens),
  both ~$0.001, both ~1400ms — the same first turn sent twice, ~1s apart.
- Same pattern again at 14:37:25 (msg:29, one canceled) and 14:33:42 (msg:10 canceled).
- This is the hedged/rescue watcher firing a second arm and both landing, or the
  turn being raced. Either way: duplicate full-context requests on the wire.

### F6 — "reflex" side-model (mistral-nemo) is fired ~10 times, mostly canceled, all speculative
- Called on nearly every turn; often `context canceled` after ~2s (ttft ~1.9s).
- When it does answer it returns ~10-140 completion tokens (~390-870 prompt tokens).
- Reads like a speculative "greetings/small-talk/fast-answer" classifier, but on real
  coding turns it burns a second model call per turn and usually gets killed.
- Each of these still costs ~$0.00002-0.00005 and adds latency noise.

### F7 — A "catch-all" untagged deepseek call runs after EVERY user turn (title/summary)
- 2-message, ~136-928 prompt tokens, always fired, always tiny. Fine individually but
  it is a hidden per-turn cost that scales with prompt size (~850+ tokens by turn 4).

### F8 — Tool count jumps 31 -> 34 after first turn
- First turn offers 31 tools; subsequent turns offer 34 (extra 3 appear — likely
  task/fork tools unlocked once one is mentioned). Unstable prefix = cache misses.

### F9 — Cache discipline is good BUT the prompt growth per turn is linear and never compacted
- Turn prompts: 15.5k -> 18.4k -> 20.3k -> 22.5k tokens; cached hits ~95% after first
  turn of each message, but the absolute prompt grows ~2k/user message and there is
  no compaction: old tool results stay in full.

### F10 — propose_task path: chat model drafts the brief, then a SEPARATE heavy "task planner" evaluates it
- Screen showed "thought for 6s · 1001 tok" and propose_task hung ~32s ("connecting").
- Log: chat model's propose_task call at 14:37:25 (22.5k prompt), then a `task`-tagged
  glm-5.3-flash call at 14:37:54 with FRESH 20.5k prompt (cache miss, full system again).
- So the handoff duplicates the whole context: chat context (~22.5k) AND task planner
  context (~20.5k) for one decision. A shared planner that only receives the decision
  payload would avoid re-paying system prompt.

### F11 — After the task, a THIRD model (qwen3.8-27b, the "careful"/checker tier) re-reviews the chat
- 14:38:38-14:39:10: 5 `turn` calls on qwen, growing to 9k prompt, biggest single
  completion: 1196 tokens, total ~$0.011 on qwen alone for ONE review pass.
- Plus glm-5.3 (mastermind tier) 1-msg call (1325 prompt, $0.0019) at 14:39:20.
- The crew's "checker" runs even when the user did not ask for verification; the chat
  then reports with citations. This is the orchestration layer spending ~4x the
  per-turn cost on self-audit.

### F12 — Grand totals for this small session
- ~70 calls; cost ~$0.023; tokens: prompt ~700k, cached ~580k (81% cache hit),
  completions ~4.6k. The spend is dominated by duplicated full-context fires (F5)
  and the post-task checker (F11), not the work itself.

## Next: read the bodies
- AFORGE_CALL_LOG_BODIES=1 was on; call body request/response JSON is in the calls
  records now, and `aforge logs --body <id>` prints them. Plan: dump system prompt,
  the 31-34 tool list, propose_task payload, and the checker (qwen) payloads.

## Interrupt / redirect (the real stall) — 14:45

Flow: sent a 3-part request, then Esc'd mid-think, then "wait, never mind the big-number part. Just do the error handling".

### F13 — ONE interrupt fires ~6 model calls at once
At 14:45:16-17, in the same second, all of:
- glm-5.3 (mastermind) `9674f2ab` msg:1 — a full "WHAT WAS ASKED / WHAT HAS BEEN DONE" replan.
- glm-5.3 (mastermind) `3affe0bf` msg:1 — ANOTHER planner pass ("WHERE THE CONVERSATION HAD GOT TO").
- deepseek `ae4f7473` msg:2 — a "Name this piece of work in two or three words" title call.
- deepseek `4f3d9ad6` msg:2 — the SAME title call again (duplicate).
- deepseek `0fd0c870` msg:57 — a "[handing over]" full-conversation handoff summary (57 messages re-sent).
- reflex mistral-nemo + a glm-5.3-flash `task` worker.
So one user interrupt fans out to 6+ calls including a full 57-message re-read.

### F14 — The stall the user SEES is a mastermind call, not the worker
glm-5.3 `3affe0bf` took 36.7s / 1229 tokens; `9674f2ab` 10s / 409 tokens.
The UI sat "working" for 1m07s on a redirect whose actual edits were 3 files.
Perceived slowness = planner/orchestration latency, not the task.

### F15 — The planner misreads state: goal recorded as "move the data across"
`3affe0bf`'s prompt shows `<state> goal: move the data across` — the session has
never moved any data. State summarisation is corrupting the goal across turns.

### F16 — A bare user redirect is AUTO-ESCAPATED into a task mid-turn
The chat: made 3 edits inline, then decided "this is changing more than a quick edit"
and spawned task 2 by itself. This is exactly the "chat model proposes the task" the
user flagged — the decision + the brief both came from the chat model under pressure,
not a dedicated planner looking at history.

### F17 — Duplicate "name this work" calls and a duplicate planner pass
`ae4f7473` and `4f3d9ad6` are the identical title prompt; `9674f2ab`/`3affe0bf` are
two mastermind replans for one interrupt. No dedup between the interrupt paths.

## Cost picture (running total, this one conversation)
~$0.04 spent; per-turn spend is dominated by mastermind (glm-5.3, $0.0047-$0.007 per
call) and full-context re-fires, NOT by deepseek worker turns (~$0.0003 each).

### F18 — The chat spawns the SAME task twice (task 2 AND task 3, both "error handling")
- 14:45 redirect spawned task 2 ("error handling"); at 14:48 the chat again said
  "this looked like work, so task 3 started: error handling" and made ANOTHER pass of
  edits (app.py, test_app.py, test_cli.py) for the same goal.
- No check that an identical open task already exists -> duplicate spend (~$0.02 for
  task 3's pass) and two workers racing the same files.

### F19 — The chat model, not a planner, decides both WHEN to task and the brief — repeatedly
- Task 2 and task 3 were both auto-spawned by the chat mid-turn ("this is changing
  more than a quick edit"), each time re-deriving the brief from the chat's own
  context instead of one planner owning the decision over the full history.

## Stream corruption + provider fragility — 14:53

### F20 — A turn's final text is persisted as MOJIBAKE after a 429 rescue
- deepseek on lane Makora 429'd 3x (hedged); hedge fired OpenInference; DigitalOcean
  answered (188 compl tokens). UI showed `rescued` then the reply rendered as garbage:
  `intentionally …..25 B4 third¹ .25". as it would: structuresheetrotation Oazu"`.
- The GARBAGE is stored in transcript.jsonl (confirmed on disk), so it is not a render
  glitch — a rescued/degraded stream was accepted as the assistant turn and persisted.
- `waste_usd` on the failed lanes: ~$0.0029 burned on lanes that errored or were cancelled.

### F21 — The hedge/rescue wastes money and can deliver a corrupted winner
- 3 provider lanes fired for one turn (Makora, DigitalOcean, OpenInference), two errored,
  one won. The win was the corrupted one. No sanity check on the rescued stream before
  it is accepted as the answer.

## Folder-open behaviour (user's complaint)

### F22 — A chat does NOT work in the folder you launched it from; it works in a per-project COPY
- Probe: `cd /tmp/af-folder-a && aforge chat --once "which directory…"` answers
  `$AFORGE_HOME/v3/projects/-tmp-af-folder-a/<id>/work` — a sandboxed copy, NOT /tmp/af-folder-a.
- Same for folder-b -> its own hashed copy. So edits the chat makes do NOT land in the
  folder the user is sitting in; they land in a hidden mirror and must be merged home.
- This is by design for task worktrees (task_run.go: "A node edits files while the person
  is editing files"), but for the CHAT ITSELF it reads as "aforge opened a random folder".

## Task-continuation model (the deep one)

### F23 — Design gap: a FAILED task has no first-class "continue", only "propose again"
- task_audit.go has a repair loop (one repair round, same worktree, fresh worker) — good.
- But once it lands TaskFailed, the graph's only advice is "re-run that work first"
  (task.go:656) i.e. propose_task AGAIN as a NEW task: new worktree, new brief, no memory
  of the failed worker's steps beyond the report text.
- task.md (worker prompt) has NO vocabulary for resuming/continuing a prior task: the
  worker is told it is "a task, not a conversation" handed "one brief and one acceptance".
- So the obvious thing a user expects — "the task failed, keep going from where it got" —
  is not expressible; the chat re-derives a fresh brief from its own (lossy) context and
  pays a full new worktree + planner + audit.

### F24 — The debug call-log rotates away after ~80 calls when bodies are on
- `AFORGE_CALL_LOG_BODIES=1` makes each record ~40KB; the 32MB cap (calllog.go:76)
  rotates after ~80 calls. One moderate debugging session blew past it in <30 min,
  moving history to `calls.1.jsonl` (kept: exactly one generation). For "deep log
  analysis" this is the wrong default — bodies + a 32MB cap cannot coexist for a
  real session. (History recovered from calls.1.jsonl.)

## Continuation probe — live result — 14:57

### F25 — "continue task 4" did NOT continue anything; it re-derived and re-spawned
- Sent: "task 4 - the broken.py one - did it fail? just continue it and finish it".
- The chat did NOT resume task 4. It made its own pass: read state, found only a
  trailing-newline diff, wrote broken.py itself (8 tool calls, 35s, ~$0.01).
- Its own words: "Nothing left for task 4. The remaining M entries … are our own
  uncommitted work from the earlier error-handling and CLI sessions."
- So the obvious user intent ("pick the failed task back up") has no path; the chat
  silently re-does the work inline rather than resuming the task's context/worktree.

### F26 — "commit everything" was ESCALATED into task 5 and the commit never happened
- Sent: "commit everything with a sensible message". The chat staged the files
  (gitignore + 5 files), then said "this is changing more than a quick edit · moving
  it to a task" and spawned task 5 ("property commit").
- `git log` afterwards: only init + two task merges; the staged work was NEVER committed.
- A one-command ask was turned into a task, the task didn't run to a commit, and the
  deliverable (a commit) silently did not happen. Over-orchestration ate a trivial ask.

### F27 — Status line shows model-y euphemisms: "taking stock · 4s"
- The rail's phase label reads "taking stock" — LLM-ish filler leaking into UX copy.

## THE REAL-USER MENTAL MODEL GAP (the deepest finding)

A person coming from Claude Code / Codex carries these assumptions. Aforge breaks them.

| User assumes (from Claude Code/Codex) | What aforge does | Finding |
|---|---|---|
| It edits the folder I'm sitting in | Edits a hidden `~/.aforge/v3/projects/<hash>/work` copy, merges home | F22 |
| "continue / retry that" resumes the failed thing | Spawns a NEW task, fresh worktree, re-derived brief | F23, F25 |
| A small ask ("commit", "fix this") is done inline | Auto-escalates to a task, sometimes dropping the ask | F16, F18, F26 |
| The reply I read is what the model said | A rescued/degraded stream can persist as mojibake | F20 |
| Interrupt = stop this turn, keep context | Fires ~6 calls incl. a 57-message re-read + 2 planner passes | F13, F17 |
| The tool picks one sensible place | Asks "which place?" / "which ground?" when >1 candidate | F28 (below) |
| Cost is roughly proportional to my ask | Planner/checker/reflex/hedge fan-out dominates small asks | F5,F6,F11,F14 |

The through-line: aforge is built around a WORKTREE+TASK+AUDIT mental model that is
internally coherent but INVISIBLE and SURPRISING to a Claude-Code-shaped user. The user
never asked for tasks, worktrees, auditors or planners — they typed a sentence. Every
time the harness translates that sentence into its own machinery it either spends
(money, latency) or surprises (wrong folder, dropped commit, duplicated task).

### F28 — Place/ground resolution asks the user to disambiguate what it could infer
- task.go:538 "Ask the person which [place], then propose this again with `ground` set
  to their answer." A user who said "fix it" in a folder expects the tool to use THAT
  folder, not to be handed a clarifying question about "ground" — a word no user typed.

## Real-user probes — 15:02

### F29 — "undo that" un-did NOTHING and then asked me what shape I wanted
- Sent "undo that" (a classic Claude-Code move, expecting the last action reverted).
- The chat replied "Nothing is committed now. Say the word if you want a different
  shape — commit again, or reset the staging too." and `git log`/`status` were UNCHANGED
  (still staged, still no commit). The tool neither knew what "that" was nor did anything.

### F30 — "why did you open a different folder" gets a defence, not a fix
- The chat explained the sandbox ("task N runs in its own copy under aforge's internal
  tree … because propose_task copies the repo so the task cannot damage your tree") —
  which is the DESIGN, offered only because asked. A user who never asked for tasks
  still had their work routed through hidden copies.

### F31 — The chat CLAIMED a commit landed that is in fact ORPHANED
- It said "task 5's commit edd9138 arrived as a merge result". Reality: `edd9138`
  ("add fibonacci CLI with error handling and tests", parent d911142) exists as a git
  object but is on NO branch — `git branch --contains edd9138` is empty.
- So task 5 made a real commit in the sandbox and the merge home never fastened it to
  the user's `main`. The model then ASSERTED success. Deliverable silently lost + a
  false claim on top. This is the worst class of bug: confident wrongness.

### F32 — comeHome CAN strand a branch yet the chat reports success anyway
- task_run.go:6090 comeHome merges the task branch into the ground; the failure roads
  return mergeConflicted with a sentence. But the CHAT's message to the user said
  "arrived as a merge result" while `git branch --contains edd9138` is empty — so either
  the conflicted/aborted sentence was not surfaced, or the model read a success-shaped
  notice and relayed it. Either way the USER is told work landed that did not.
- This compounds F26/F31: the task-happy path + a merge that can quietly strand + a
  model that asserts success = the exact "confident wrongness" a user cannot catch
  without running `git log` themselves (which the tool is supposed to save them from).

## Cleanup done
- tmux session `afdbg` killed.
- Scratch workspaces /tmp/aforge-perf-ws*, /tmp/af-folder-* and isolated state
  /tmp/aforge-perf-debug left IN PLACE for now so the findings can be re-verified
  (they contain the orphaned-commit evidence). Delete when the audit is signed off.
- The user's real ~/.aforge was NEVER touched (all runs used AFORGE_HOME=/tmp/aforge-perf-debug).

## Second battery (fresh session, /tmp/af-ux cart app)

### F33 — First turn stalled ~1m40s; a later turn waited 129s on ONE lane with no hedge
- "add a discount code system": chat did ls + 3 reads, then the turn hung "all lanes
  slow · still waiting"; the log shows a `turn` call with `wait_s: 129.5`, `arms: None`
  — hedging did NOT fire to rescue it. Total to first real output: ~2m49s for a vague ask.

### F34 — Result-quality slip: __pycache__ binaries committed, then removed
- The discount-code task committed `__pycache__/*.pyc` (commit 07c9388) and a second
  commit (1d7f73b) had to remove them. The worker did not respect "don't commit build
  artifacts". Left an orphan-ish branch `task/discount-code-entry-531f2f` too.
- POSITIVE: the actual code was idiomatic and minimal, tests pass (6), and it correctly
  removed the now-dead `apply_discount`. Good craftsmanship, poor hygiene.

### F35 — (confirming F22) session opened in hashed v3 path, not /tmp/af-ux
- Status line shows the transcript at `…/v3/projects/-private-tmp-af-ux/<hash>/…`; edits
  did reach /tmp/af-ux via merge, but the "where am I" answer is the sandbox, not the
  folder the user launched.

### F36 — Steering a running task leaves the repo in an inconsistent half-state
- With task 2 ("discount code entry") running in a parallel tree, I steered: "make the
  max discount 15%". The chat edited cart.py/test_cart.py on main (SAVE20->SAVE15), then
  spawned task 3 ("discount cap test") on the SAME files.
- End state: main still has SAVE20; the SAVE15 fix is UNCOMMITTED in the working tree;
  task 3 exists but its change never landed; tasks 2 and 3 branches both linger.
- Three writers (chat + task2 + task3) on overlapping files with no single owner, and
  the user's visible tree does not reflect what they asked for. A user would be lost.

## COST/VALUE ledger for this battery so far
- "add a discount code system" (vague): ~2m49s, ~$0.008, net +68 lines, tests pass.
- Steering to SAVE15: ~$0.01, and the change is NOT durably committed.
- Trust erodes: each turn costs cents and minutes but the user cannot rely on the
  on-disk state matching what they were told.

### F37 — Self-explanation is genuinely good, and self-corrects cross-task staleness
- Asked "can you explain what you just did?": it produced an accurate numbered account,
  AND noticed that task 2's merged README still documented SAVE20 (now contradicting the
  SAVE15 code) — then fixed and committed the cap+README together (ec19248). Final state
  is clean and consistent.
- So the model's reasoning is strong; the FAILURES are orchestration (F36) and trust
  (F31), not intelligence. The gap is that the user had to ASK for the reconciliation —
  it did not happen unprompted at the moment task 2 merged with a now-stale README.

## MODEL / ROLE ALLOCATION (cost per role across the whole debug run, ~$0.109)

| model (role) | tag | calls | prompt tokens | cost | what it was doing |
|---|---|---|---|---|---|
| qwen3.8-27b (careful) | turn | 6 | 59k | $0.0406 | post-hoc AUDIT of finished work |
| glm-5.3-flash (worker) | task | 30 | 696k | $0.0296 | doing the actual task work |
| glm-5.3 (mastermind) | (none) | 6 | 13k | $0.0277 | replan / "what was asked vs done" passes |
| deepseek-v4-flash (chat) | turn | 9 | 242k | $0.0089 | the conversation itself |
| mistral-nemo (reflex) | reflex | 4 | 4k | $0.0001 | speculative fast classifier |

### F38 — The two most expensive roles are ORCHESTRATION, not work
- qwen3.8-27b "careful" checker = $0.0406 (37% of spend) just AUDITING finished work;
  glm-5.3 mastermind = $0.0277 (25%) on replan/summarise passes. Together ~62% of the
  spend is coordination/verification, ~27% is the work itself (task worker), ~8% is chat.
- The chat model (deepseek, the one the user experiences) is the CHEAPEST line item.

### F39 — Cheap/expensive are arguably INVERTED on the decision that matters
- The high-stakes decision — WHEN to spawn a task and what its brief is — is made by the
  CHEAP chat model (deepseek) mid-turn (F16/F18/F26), while the EXPENSIVE models only
  audit after the fact. If a planner is wrong, the auditor cannot recover the wasted
  worktree. The user's instinct ("a task's own planner should propose tasks") maps to
  moving that decision OFF the chat model onto a deliberate role.
- The reflex model (mistral-nemo) is nearly free but almost always cancelled — pure
  latency noise, negligible cost, real UX drag.

### F40 — Auditor refusals burn paid turns on prompt-engineering friction
- qwen calls at 15:18:36/41 show the auditor REFUSING its own verification command
  ("an auditor runs ONE verification command with no shell composition, and '>' is in
  python3 -m pytest …"), retrying with the same constraint. A too-strict auditor
  contract turned one check into several paid round-trips.

## BLIND SUB-AGENT CORROBORATION (product-agnostic / blind-user / result-quality)

Three independent agents, none reading the product framing, converged on the same core:

- result-quality-audit.md: logic is correct (both pytest suites pass) but "bookkeeping
  integrity" fails — orphaned commit, self `reset --soft` undo while claiming otherwise,
  committed .pyc binaries, `git merge -s ours` burying provenance.
- ux-qa-framework.md (product-agnostic): top risks all map to my instrumented findings —
  reporting fidelity (= F31/F32), interrupt integrity (= F13/F36), undo completeness
  (= F29), silent context loss, cost opacity, approval-mode ambiguity.
- blind-user-qa.md: onboarding never says it edits files / approval cards; the DEFAULT
  chat model hung 90s on first prompt (had to /model to grok-4.6); approval prompts
  DENY-BY-DEFAULT on a hidden ~10s timer (a late keypress "killed a proposed task");
  undo-by-chat stalled 3min on a dead lane; quit needed 4 ctrl+c. Output quality was
  "genuinely good" once it landed; ~25min waited for a trivial edit.

### F41 — Approval cards deny-by-default on a hidden timer
- The compliant read is a duty of care problem: a user who hesitates >~10s is recorded
  as "denied", and one user's task was killed by it. Default must not be "no".

### F42 — The default chat model can hang at first prompt (blind user switched to grok)
- Onboarding's default deepseek-v4-flash hung 90s with no rescue; the blind user had to
  discover /model. First-run should ship a model that answers, not just a cheap one.

## CONSOLIDATED PRIORITY (fix order)
1. TRUST: never claim landed/committed unless the merge fastens to the user's branch (F31/F32).
2. DROP: don't auto-escalate small asks into tasks (F16/F26); route that decision to a planner (F39).
3. CONTINUITY: add first-class continue/resume for failed tasks (F23/F25).
4. FOLDER: edit the folder the user is in; surface the sandbox only when real isolation is needed (F22).
5. INTERRUPT: dedup the fan-out per Esc (F13/F17); one planner pass, not two.
6. APPROVAL: stop deny-by-default timer (F41); make the mode explicit.
7. COST: cap orchestration roles (auditor + mastermind = 62% of spend) (F38/F40); tame hedge (F21).
8. FIRST-RUN: default model must answer (F42); onboarding must say it edits files.

---

# SOLUTIONS — architecture, bug, and rethinking (with self-critique)

First, a correction to my own earlier framing. I wrote "the chat model is a shadow
planner, add a dedicated planner." Reading the code, that was too glib: a route judge
(route_judge.go), a trap-scored checkpoint sketch, and a measured digest budget ALREADY
exist, and the benchmark already killed the pre-turn conversion because "reading the
request" can't judge the work. The real problems are narrower and harder.

## A. Where I was wrong (self-critique)
- "Add a planner" — wrong shape. The planner exists; the issue is the DECISION still
  lands on the chat model / a racing verdict at the wrong moment, not its absence.
- "It wastes tokens everywhere" — partially wrong. The digest budget (5k tokens vs the
  measured 57-91k raw read) is a deliberate, measured win. Waste is concentrated, not
  general: the duplicate hedge fires, the fan-out on interrupt, and the auditor-tier
  spend. Precision beats the broad claim.

## B. Bug-level fixes (surgical, high-value, low-risk)
1. **comeHome must never report success when the branch didn't fasten** (F31/F32).
   Make the merge outcome a hard precondition of the "landed" notice: if
   mergeIntoGround() did not land, the user-facing sentence MUST say so and keep the
   branch name. Add a regression test: stranded branch => notice contains the branch.
2. **Dedup the interrupt fan-out** (F13/F17). One Esc fired ~6 calls incl. two
   mastermind replans and two identical title calls. Serialize the interrupt handlers
   behind a single "what changed" decision.
3. **Deny-by-default approval timer** (F41). A hidden ~10s timeout recording "denied"
   (and killing a task) is backwards. Default must be pause/keep-waiting, never "no".
4. **Rescued-stream validation** (F20/F21). A hedge winner that is mojibake must be
   rejected (sanity check: decode/printable ratio / finish_reason) before persistence.
5. **Debug log rotation vs bodies** (F24). When AFORGE_CALL_LOG_BODIES=1, raise or
   disable the 32MB cap; bodies + 32MB cannot coexist for a real session.
6. **Auditor refusal loop** (F40). The auditor refusing its own single-command
   constraint and retrying burns paid turns; relax to "one pipeline" or pre-validate.

## C. Architecture-level fixes (the real leverage)
1. **Move the task-spawn decision off the chat model and off the request-race, onto the
   checkpoint sidecar that reads the WORK.** route_judge.go already proved the request
   alone can't judge; checkpoint.go already reads the work. The bug (F16/F26: "commit"
   became a task) is that the conversion still triggers on work that is one command.
   Fix: a hard floor — if the ask maps to a single command or a known-trivial verb set
   (commit, undo, a one-file edit), the checkpoint never converts. Work-or-words is in
   the prompt; it needs a cheap structural gate, not more prompt.
2. **First-class task continuation** (F23/F25). The worktree + checkpoint + journal
   already persist a task's state; what's missing is a `continue <id>` verb that re-arms
   the SAME node with its brief + auditor evidence instead of proposing a new task.
   This is an addition, not a rearchitecture — the seams exist.
3. **A single-writer invariant per file set** (F36). Chat + task2 + task3 wrote the same
   files concurrently; the user's tree ended inconsistent. Enforce: while a task holds a
   file's worktree, the chat's edit to that file is either blocked or auto-routed into
   that task. One owner per file at a time.

## D. Fundamental rethinking (the honest part)
The deepest issue is not any bug — it is that aforge's core model (worktree + task +
audit + planner) is a *correct and well-engineered* answer to a question the user did
not ask. A person types a sentence; the harness translates it into machinery that then
surprises them (wrong folder, dropped commit, task they didn't want). Two genuine options:

- **Option 1 — collapse the default surface to "edit in place, in the open", and make
  tasks OPT-IN.** Chat edits the launched folder directly; worktrees/tasks/audit only
  engage for explicit "as a task" or genuinely large/multi-part work. This trades the
  elegant isolation for predictability, and it matches what Claude-Code-shaped users
  expect. RISK: it gives up the merge-safety the worktree model bought, and re-opens
  the "half-finished sweep in your tree" problem task_run.go was written to prevent.
- **Option 2 — keep the machinery but make it INVISIBLE and TRUTHFUL.** The user never
  hears "task/worktree/audit"; they only see "done, here, verified" that is always true,
  and a one-line honest note when it isn't. This keeps the engineering but fixes the
  trust gap. RISK: it is the harder build — every silent failure road (F26/F31/F36)
  must be found and made truthful, and a single remaining confident-wrong report undoes
  the whole promise.

My read: Option 2 is the real product, but it is gated on B.1 and C.1 being TRUE, not
best-effort. If comeHome can still strand a branch while claiming success, no amount of
surface polish makes the model trustworthy. Do B first; they are cheap and they are the
difference between "aforge that surprises" and "aforge you can trust".

## E. What I am NOT recommending (and why)
- NOT "bigger planner model": the spend is already 62% orchestration (F38); adding
  planner horsepower to a decision that should be a structural gate is the wrong lever.
- NOT "fewer tools": 31-34 tools is large but the cache makes the per-turn marginal cost
  small; the waste is duplicate fires and audit tier, not the tool list size.

---

# PARETO PASS — cost · wall-time · quality (self-critical, model-role deep dive)

## First: the measured split (the only numbers that matter)
- By tag:  `turn` 71.5% of spend · `other` (planner/route/audit side-calls) 15.1% ·
  `task` (real work) 13.3% · `reflex` 0.1%.
- By model: qwen3.8-27b (TierHigh auditor) 66.6% · glm-5.3 (mastermind) 14.1% ·
  glm-5.3-flash (worker) 13.3% · deepseek (chat) 5.9% · mistral-nemo (reflex) 0.1%.
- The AUDITOR tier alone is ~2/3 of the bill. The work itself is ~13%.

## Where I criticize my own earlier proposals
1. "Move the task-spawn decision to a smarter planner" — WRONG as stated. roles.go
   already routes decisions to dear tiers deliberately (routerconfirm on mastermind,
   auditor on TierHigh) with a measured rationale ("a wrong verdict is not an economy
   question"). Adding MORE intelligence to the spawn decision is the opposite of the
   Pareto fix. The bug was never intelligence; it was that a one-command ask reached
   the conversion path at all.
2. "Reduce orchestration spend" — partially wrong. Some of it is the deliberate price
   of an honest verdict (the auditor). Cutting it blindly would trade quality for cost.
   The Pareto-correct cut is the WASTE inside it, not the role.
3. The "intelligence-score / deterministic role assignment" idea — I am NOT convinced.
   The registry is already deterministic (role -> tier -> crew row). A 0-1 intelligence
   scalar would ADD a second, fuzzier allocation axis on top of one that is already
   explicit and tunable per crew. That is bloat, not simplification. Drop it.

## The actual Pareto problems (and the minimal fix for each)
1. **qwen auditor is 67% of spend but a slice of it is PURE WASTE.** 4 of its calls were
   literally `task naming` and several were self-refusals ("auditor runs ONE command with
   no '>'") retrying. You do not need the dear auditor to NAME a task or to fight its own
   command contract. FIX: route task-naming off the auditor tier; pre-validate the
   auditor's command so it never self-refuses. Quality-neutral, cost down.
2. **Wall time is lost to serialization, not model speed.** A first turn waited 129s on
   one lane with `arms: None` (hedge didn't fire); the interrupt fired a 57-message
   re-read. Neither is a model-choice problem. FIX: fire the hedge sooner on a stall;
   dedup the interrupt fan-out. Both cut wall time without touching cost or quality.
3. **The turn loop is 71% of spend and re-reads the full conversation on every tool
   round-trip** (compl/prompt = 0.047 — the model reads ~21x more than it writes).
   Quality of the final code is good, so this is the cost of correctness — but it is
   ALSO where a mid-conversation compaction would pay for itself many times over.
   FIX: compact/trim old tool results earlier (they are the bulk of the re-read), not
   the system prompt. Cost down, quality held, wall time down.

## Roles: keep them, fix the routing, don't add axes
- The five tiers are NOT bloated — each has a measured job. What is off is a few ROUTING
  decisions (auditor doing naming; the spawn decision reaching the conversion path on
  trivial asks), not the tier count.
- Removing a tier would force one model to serve two rhythms (volume vs decision), which
  is exactly the mistake roles.go warns against. So: no new axis, no removed tier.
- The one genuinely questionable role is `reflex` — 8 calls, $0.0003, mostly cancelled.
  It costs ~nothing but adds latency noise and a second in-flight call per turn. It is
  the closest thing to bloat, but the saving from removing it is trivial; leave it unless
  the latency noise is proven to matter.

## Bottom line for the Pareto
The biggest, cheapest, quality-neutral wins are NOT model choices:
  - Stop the auditor doing non-audit work (naming, self-refusals).  [cost]
  - Fire the hedge on stalls; dedup the interrupt re-read.            [wall time]
  - Compact old tool results mid-turn.                                [cost + wall time]
Each reduces cost or wall time WITHOUT reducing quality — the definition of moving
toward the Pareto front rather than along it. The roles stay; the waste goes.

---

# POST-FIX RE-AUDIT (waves 1-4 binary, 2026-09-04) — independent blind QA + reproduction

Three blind subagents + my own tmux reproduction on the rebuilt binary. Headline:
the wave-2 trust fixes WORK (integrity audit: no orphans, claims reconcile), but the
DEFAULT (non-YOLO) approval mode still creates heavy friction, and two real gaps remain.

## Confirmed fixed
- F31/F32 orphans: integrity audit — no orphaned commits, claims match git reality.
- F34 pycache: proactively gitignored before commit.
- F26 commit: small asks done inline (09 floor works).
- F07 approval expiry: code + tests confirm expiry PAUSES, never denies.

## NEW / REMAINING issues (post-fix)

### R1 — "continue task N" does NOT invoke the continue verb (the model narrates instead)
- Repro: spawned a task (fix divide bug, "do NOT commit"), it landed uncommitted, then
  sent "continue task 1". The model produced a 3-tool-call narrative about the task's
  state but NEVER called tasks{id,continue} or propose_task (verified in the call log).
- So issue 10's machinery exists and is unit-tested, but the chat model does not reach
  for it on the plain phrase "continue task N". Discoverability/wiring gap: either the
  tool hint doesn't surface continue strongly enough, or the model needs a nudge.
- ALSO: structured-QA found the same ("continue task 1 ... ends with 'Please continue'").

### R2 — Default (non-YOLO) approval mode still reads as auto-deny to a blind user
- Blind user (default profile): "every action hits a y/n/a prompt that AUTO-DENIES after
  10s, so simple things took minutes of babysitting". My code read says expiry pauses
  (07 fixed). The gap: either the y/n/a TASK-PROPOSAL card has a different timeout that
  still denies, or the prompt flood (one prompt per action) makes the 10s window
  un-winnable in practice. The structured-QA (approvals allow) saw approval PASS — so this
  is specifically the DEFAULT mode's UX, and it is still bad.

### R3 — A task told "do NOT commit" can still auto-merge on land (structured-QA FAIL)
- My repro honored "do NOT commit" (left uncommitted). But structured-QA saw a /task
  auto-merge as commit e312c8e despite the instruction. So merge-on-land can override an
  explicit "don't commit" in some paths. Needs a guard: an explicit no-commit instruction
  must suppress the auto-merge.

### R4 — Control-token leak into the reply (structured-QA)
- Raw `</｜DSML｜parameter>` / `</｜DSML｜invoke>` tokens printed into the user-facing reply
  during the continuation probe — the model's tool-call grammar leaked into rendered text.

### R5 — Residual: orphan task branches + autostash debris still left behind
- Blind user: final tree clean BUT left an orphan task branch, two autostashes, dangling
  commits. Integrity is better (no lost claimed work) but the repo is still littered.

### R6 — Latency floor is high for trivial asks
- ~12s for "OK", ~63s for a one-line edit. Cost is cheap and proportional; wall-time is not.

### R7 — First interactive launch over a dumb pipe renders a blank alt-screen (no onboarding text)

---

# POST-REBASE RE-AUDIT (rebased onto dev @ 79f3e44f + waves 1-4, binary 6aaee5c4) — 2026-09-04

Independent blind QA + integrity audit on the rebased binary. This is the honest
scorecard after the rebase. Pre-existing upstream failures noted (TestC3,
TestAWorktreeTaskBindsContractPaths fail on clean dev too — macOS symlink, NOT ours).

## FIXED (verified independently)
- do-NOT-commit: honored (power() stayed ` M calc.py`); one near-miss self-corrected on Esc-deny.
- Small asks (commit/undo/fix) run inline, no surprise task spawn (09 floor works).
- Esc interrupt: fast, recovers gracefully (03 works).
- Git trust: every claim matched `git log`/`git status`; __pycache__ never committed;
  NO orphaned commits (05 works); integrity audit: claims reconcile exactly.
- Mojibake / control tokens: zero in transcripts (06 works).
- Approval expiry: countdown pauses and waits forever, no deny-timer (07 works).

## STILL BROKEN / REMAINING (the honest gaps)
### R1 — "continue task N" STILL does not resume (the headline gap)
- continue machinery exists and is unit-tested, but the CHAT never reaches it: either no
  task forms (work runs inline) so there is no id, or the model narrates instead of
  calling tasks{id,continue}. Blind QA: "No task 1 in this project" + inline redo.
- ROOT: the verb is exposed but the model doesn't invoke it on the plain phrase, AND
  cross-session continues are refused by design (no graph). Needs a discoverability fix
  and a clear honest answer when the task isn't continuable.

### R2 — Default-mode approval friction is still bad
- Tool prompts correctly PAUSE (no deny-timer), BUT `propose_task` times out as
  "denied by the person: default" (inconsistent + misattributed — a deny nobody gave),
  and prompts hide behind a minutes-long "forming…" spinner until Esc.
- Approval prompts required even for read-only `git status` / `tasks`.

### R5 — Agent litter: verify_assignment.py, task-2-*.md reports, __pycache__ left untracked
- Not an integrity violation, but the tree is not clean of assistant debris.

### R6 — Default deepseek-v4-flash lane was dead (60s+ stalls) until re-pinned
- Responsiveness: warm `chat --once` 3.6s and cost proportional, but the default model's
  lane stalled repeatedly; blind user had to pin claude-sonnet-4. First-run rescue (13)
  did not fully save this.

### R7 — Two Ctrl+C sometimes won't quit (needed /quit); `\n` vs `\r` submit quirk (PTY)
### R8 — The task died on the 10-min deadline (exit 3) mid-continuation.

## Bottom line for the Pareto
- TRUST floor: now solid (claims reconcile, no orphans, do-not-commit honored). The
  worst class of bug (confident-wrong) is gone.
- COST: proportional and cheap.
- WALL-TIME + CONTINUITY: the remaining pain. The default-mode approval friction (R2),
  the dead default lane (R6), and "continue doesn't continue" (R1) are what a user will
  actually feel next. R1 and R2 are the highest-value next fixes.
