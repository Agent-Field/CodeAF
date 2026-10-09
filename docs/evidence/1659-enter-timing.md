# #1659 enter-timing measurement — home input Enter vs inside-conversation Ctrl+T

Measurement proof for PR #1743 ("chat: keep home conversation transitions responsive", issue #1659), manager directive #1790715358822565435 + global #551. Run by task t-28 (agent 28) on 2026-10-03, measurement window 22:11-22:26 UTC, all execution on the Spark over `ssh spark`. #197 evidence protocol: exact SHAs, exact commands, exit codes, honest error bounds.

## Headline

- Real-keypress timing collected for both flows on both trees: **8 runs per flow per tree** (mandated minimum 3), every run in its own fresh isolated profile.
- **The two trees are indistinguishable in these flows at this method's resolution** (per-row error bounds 3.2-19.0 ms):
  - Flow A (home input Enter): fix mean **10.9 ms**, dev mean **9.3 ms** (medians 9.4 / 9.0).
  - Flow B (inside-conversation Ctrl+T): fix mean **22.3 ms**, dev mean **16.2 ms** (medians 21.7 / 16.0).
- **Claim-support verdict: NO — the fixed tree's timing, measured as specified here, does NOT support a "Fixes #1659" claim.** It shows no regression on the fixed tree, but it demonstrates no improvement either. The flat result is fully explained by the PR's own change entries (section 6): the three cost sources they describe are all absent in the mandated fresh-empty-profile protocol. A supplementary probe that constructed one of those cost sources (300 generated foreign-skill folders per run) separated nothing either (section 5).
- Consistent with the person's standing instruction ("Do not claim Fixes #1659 until actual timing evidence supports it"), **no "Fixes #1659" claim is made here**, and nothing on GitHub was touched (no comments, no pushes, no PR edits, no merges; PR #1743 untouched by this task).

## 1. Flows measured, and the observable ready boundary

Both boundaries are defined by observable pane state (tmux capture-pane content), never by internal function timing.

- **Flow A — HOME INPUT Enter.** From the home screen, type a message in the input box, press Enter. The home/conversation transition visibly completes when the home body is gone and the conversation chrome is drawn.
  - Ready markers: `What would you like to work on?` **absent** (home body gone) AND `project:` **present** (conversation footer, e.g. `project: ~/work` shown in the capture).
  - In every run the transition repainted atomically: the "home body gone" flip and the ready markers appeared in the same capture (flip_ms == ms in all 16 mandated A rows).
- **Flow B — INSIDE-CONVERSATION Ctrl+T.** From inside the conversation (1.0 s after flow A's boundary, same session), press Ctrl+T, which opens the New chat roster view.
  - Ready markers: `esc keeps the chat you were in` **and** `recent sessions` both present (the New chat view's own lines, with the just-sent message's row in the roster).
- Marker shapes were verified identical on both trees against the saved per-run captures: home captures contain the home body and no `project:` on both trees; A-ready captures show `esc interrupt` + `project: ~/work` + the model status bar; B-ready captures show the New chat roster on both trees.
## 2. Trees measured (exact SHAs)

| tree | SHA (`git rev-parse HEAD`) | source | build |
|---|---|---|---|
| dev tip | `06a3283dd74385bf3c86ffc799c47b51b24fb0f7` | fresh clone `/home/santosh/src/codeaf-measure-dev`, branch `dev` as of 2026-10-03 22:02 UTC | `make build` → `bin/codeaf`, `-X ...buildinfo.rev=06a3283`, exit 0 |
| fixed tree | `e23a74f277b01d6d33d2639561946fc31aab1ce7` | fresh clone `/home/santosh/src/codeaf-measure`, branch `fix/1659-enter-responsiveness` (head == `git rev-parse e23a74f27`) | `make build` → `bin/codeaf`, `-X ...buildinfo.rev=e23a74f`, exit 0 |

The fixed tree is PR #1743's head: `06a3283d..e23a74f27` = 10 commits (`f6aa4e3f`, `b33964c2`, `a97deac1`, `7ec6f2a1` merge, `f47ed08d`, `8d0d4cd8`, `4db96bee`, `c6f1d217`, `44a13cf3`, review-fix `e23a74f2`), diff 26 files, +695/-71 (mostly tests, docs, and the before/after screen recordings in `docs/media/1659/`).

All Spark clones are this task's own (`/home/santosh/src/codeaf-measure`, `/home/santosh/src/codeaf-measure-dev`; a blobless no-checkout clone `/home/santosh/src/codeaf-full` was used only for read-only `git log` / `git show`). The colleague's note that the shared `~/src/codeaf` checkout carries uncommitted changes and aborts checkouts was honored: it was not used for any build.

## 3. Methodology

**Real keypresses only.** The TUI is driven through tmux: `tmux send-keys` injects the key through the PTY input path (literal text `hello timing test`, then `Enter` / `C-t`). No synthetic function timing, no benchmarks, no internal instrumentation of any kind.

**Per-run protocol (driver on the Spark; verbatim in Appendix B). In the driver the run's profile dir is called `D`; here it is `PROFILE`:**

1. Fresh isolated profile: `PROFILE=$(mktemp -d /tmp/m1659-run.XXXXXX)`, workspace `PROFILE/work`, launch `env HOME=PROFILE CODEAF_HOME=PROFILE OPENROUTER_API_KEY=<fabricated> <tree's bin/codeaf>` in a tmux session (`-x 100 -y 30`, cwd `PROFILE/work`). The run's entire world lives under PROFILE; the run ends with `tmux kill-session` and `rm -rf PROFILE`.
2. Wait for the home screen: poll `tmux capture-pane -p` for `What would you like to work on?` (up to 300 polls; not measured).
3. `tmux send-keys` the literal text `hello timing test`; sleep 0.5 s.
4. **t0 = `date +%s%N` immediately before `tmux send-keys -t SESSION Enter`** (flow A).
5. **No-sleep poll loop:** each iteration stamps `date +%s%N`, runs `tmux capture-pane -p`, tests the boundary markers; the first matching capture's timestamp is the boundary. Loop cap 2000 polls (no row hit it).
6. Sleep 1.0 s (now inside the conversation), then **t0 = `date +%s%N` immediately before `tmux send-keys -t SESSION C-t`** (flow B) and the same poll loop with flow B's markers.

**Error bound / resolution (stated honestly).** Per row, the reported error bound is `t(first ready capture) - t(last non-ready capture)` — the interval the true boundary provably lies within (where the first capture was already ready, the bound is `t(ready) - t0`). Observed bounds: **3.2-19.0 ms** (median about 5 ms). `tmux send-keys`'s own execution sits inside the measured interval (a few ms). Several runs completed at the very first capture (polls=0): those transitions are faster than one poll step and are reported at this resolution and no better. This polling resolution is the method's error bound and all comparisons must be read against it.

**Fairness.** The box was shared and busy during the window (`load average` 4.2-12.6, 94-96 users; recorded in the driver logs) — the dominant noise source. Runs were round-robin interleaved with alternating tree order (fix,dev then dev,fix, ...) so load drift biases both trees equally. Flow B is measured 1.0 s after flow A's boundary in the same session, i.e. genuinely from inside the conversation.

**Screen state discovery (how the boundaries were chosen).** Two discovery runs (fresh profiles, real keys) established: the home screen (`What would you like to work on?` + `Choose a starting point or type your request.`), the conversation chrome after Enter (status bar, `esc interrupt`, `project: ~/work`), and the Ctrl+T New chat roster (`esc keeps the chat you were in`, `recent sessions`, `New chat · first message starts the conversation`). Discovery scripts `/tmp/m1659-disc.sh`, `/tmp/m1659-disc2.sh` on the Spark.
## 4. Mandated results — ms per run (flow x tree x run)

| flow | tree | run | ms | error bound (ms) | polls |
|---|---|---|---|---|---|
| A home Enter | fix | 1 | 15.7 | 11.7 | 1 |
| A home Enter | fix | 2 | 9.7 | 6.6 | 1 |
| A home Enter | fix | 3 | 8.9 | 5.2 | 1 |
| A home Enter | fix | 4 | 9.0 | 5.3 | 1 |
| A home Enter | fix | 5 | 21.1 | 4.7 | 3 |
| A home Enter | fix | 6 | 4.0 | 4.0 | 0 |
| A home Enter | fix | 7 | 10.0 | 6.4 | 1 |
| A home Enter | fix | 8 | 8.6 | 5.4 | 1 |
| A home Enter | dev | 1 | 3.2 | 3.2 | 0 |
| A home Enter | dev | 2 | 9.0 | 6.1 | 1 |
| A home Enter | dev | 3 | 22.3 | 6.4 | 3 |
| A home Enter | dev | 4 | 13.2 | 3.9 | 2 |
| A home Enter | dev | 5 | 3.9 | 3.9 | 0 |
| A home Enter | dev | 6 | 4.4 | 4.4 | 0 |
| A home Enter | dev | 7 | 9.0 | 5.3 | 1 |
| A home Enter | dev | 8 | 9.6 | 5.4 | 1 |
| B Ctrl+T | fix | 1 | 21.4 | 14.7 | 1 |
| B Ctrl+T | fix | 2 | 25.5 | 17.1 | 2 |
| B Ctrl+T | fix | 3 | 37.4 | 19.0 | 2 |
| B Ctrl+T | fix | 4 | 20.3 | 5.2 | 3 |
| B Ctrl+T | fix | 5 | 22.0 | 6.4 | 3 |
| B Ctrl+T | fix | 6 | 9.2 | 5.6 | 1 |
| B Ctrl+T | fix | 7 | 21.9 | 9.8 | 2 |
| B Ctrl+T | fix | 8 | 20.3 | 6.2 | 3 |
| B Ctrl+T | dev | 1 | 21.6 | 6.7 | 3 |
| B Ctrl+T | dev | 2 | 15.8 | 5.2 | 2 |
| B Ctrl+T | dev | 3 | 14.8 | 5.9 | 2 |
| B Ctrl+T | dev | 4 | 11.9 | 8.9 | 1 |
| B Ctrl+T | dev | 5 | 22.0 | 7.2 | 2 |
| B Ctrl+T | dev | 6 | 8.1 | 4.1 | 1 |
| B Ctrl+T | dev | 7 | 16.1 | 5.3 | 2 |
| B Ctrl+T | dev | 8 | 19.6 | 5.9 | 3 |

### Summary

| flow | tree | n | mean ms | median ms | min | max | error bounds seen |
|---|---|---|---|---|---|---|---|
| A home Enter | fix (e23a74f27) | 8 | 10.9 | 9.4 | 4.0 | 21.1 | 4.0-11.7 |
| A home Enter | dev (06a3283d) | 8 | 9.3 | 9.0 | 3.2 | 22.3 | 3.2-6.4 |
| B Ctrl+T | fix (e23a74f27) | 8 | 22.3 | 21.7 | 9.2 | 37.4 | 5.2-19.0 |
| B Ctrl+T | dev (06a3283d) | 8 | 16.2 | 16.0 | 8.1 | 22.0 | 4.1-8.9 |

Every difference between the trees lies inside the method's error bound. Flow A: 10.9 vs 9.3 ms means, medians 9.4 vs 9.0. Flow B: 22.3 vs 16.2 ms means — and if anything the fixed tree reads *slower* here, which is noise, not signal (bounds 5-19 ms on a shared busy box). No run anywhere showed a freeze, hang, or outlier beyond 37.4 ms.
## 5. Supplementary probe — one cost source constructed (clearly separate from the mandated table)

The mandated protocol (fresh **empty** profiles) carries none of the workloads the PR's change entries name. To test whether that is why the table is flat, one probe constructed cost source #3 (the foreign-skill folder scan) **inside still-fresh, still-isolated profiles**: before launch, the driver generated 300 foreign-skill folders per run (100 each in the profile's `.claude/skills`, `.codex/skills`, and the workspace's `.claude/skills`, each `SKILL.md` about 11 KB; `find PROFILE -name SKILL.md | wc -l` = 300, logged per run). Same keys, same boundaries, same polling; 4 runs per flow per tree (P1 from driver3, P2-P4 from driver4).

| flow | tree | P1 | P2 | P3 | P4 | mean | median |
|---|---|---|---|---|---|---|---|
| A home Enter | fix | 18.9 | 8.2 | 7.9 | 7.5 | 10.6 | 8.1 |
| A home Enter | dev | 13.3 | 7.1 | 3.6 | 9.3 | 8.3 | 8.2 |
| B Ctrl+T | fix | 23.6 | 20.2 | 6.4 | 19.9 | 17.5 | 20.1 |
| B Ctrl+T | dev | 52.8 | 4.1 | 9.2 | 3.7 | 17.5 | 6.7 |

(values in ms; error bounds 3.1-49.0 ms, same method). **The probe separates nothing.** Either the scan's cost lands outside these boundaries (the change entry itself scopes it to "before the first message could build"), or 300 folders sit below the noise floor of a loaded shared box. Either way this probe does not demonstrate the fix.

Probe provenance, recorded for honesty: probe driver3 reused a loop variable and therefore ran only its first round, mislabeling the dev rows `run=101` in its CSV (the data is real; relabeled P1 here). Driver4 fixed the bug and supplied P2-P4.

## 6. What the trees actually differ in (why the table is flat)

`git log 06a3283d..e23a74f27` and the PR's own change entries in `docs/changes/unreleased/` name the three behavior changes and, crucially, the conditions they fix:

1. **"enter in the home input no longer freezes the screen while a shared conversation opens"** — "On a **shared legacy connection**, pressing enter in the home input ran the whole launch assembly ... **inside the keystroke**, and typing and drawing froze for as long as the engine took." (the freeze condition is a shared/hosted connection; local doors already opened off-loop.)
2. **"home's rescan defers while a conversation door is in flight"** — "adding rescan cost to the transition **on busy profiles**."
3. **"the foreign-skill scan runs beside a new conversation instead of inside it"** — "Opening a conversation used to scan every foreign skill folder synchronously before the first message could build ... The scan runs in the background now; the first message waits up to two seconds for it."

Plus the review-fix commit `e23a74f27` itself: deletes the never-read `homeView.owed` field (no behavior) and swaps a bare `recover()` for `guard.Recover` in the foreign-skill pass (panic logging only). **Neither changes transition latency in a healthy run.**

In the mandated protocol none of the three cost sources exists: the connection is local (not a shared legacy connection), the profile is empty (home's "places" walk — `worldSeam(a.world, a.placesRoot(), a.hosted())` in `internal/tui3/home.go` — has nothing to walk), and there are no foreign-skill folders anywhere. Both trees therefore run the same cheap path, and the measurements match that prediction exactly.
## 7. Claim-support verdict (explicit)

**Does the fixed tree's timing support a "Fixes #1659" claim? No.**

- What the numbers support: the fixed tree `e23a74f277b01d6d33d2639561946fc31aab1ce7` is **no slower** than dev `06a3283dd74385bf3c86ffc799c47b51b24fb0f7` in flows A and B under the mandated protocol; all 32 mandated measurements complete in 3.2-37.4 ms on both trees with overlapping error bounds; no freeze or hang was observed on either tree.
- What the numbers do **not** support: any claim of improved timing from this evidence. The two trees are indistinguishable here, and the supplementary probe that constructed one of the PR's named cost sources did not separate them either.
- Why, in the PR's own words: the transitions this PR fixes freeze only on a shared legacy connection, on busy profiles, or with foreign-skill folders present (section 6) — none of which the mandated fresh-empty-profile protocol allows. The measurement protocol mandated for this job is structurally blind to the fix.
- Therefore, per the person's standing instruction, **no "Fixes #1659" claim should rest on this timing evidence**. To support the claim with timing, re-measure the same two flows under one of the named conditions — most directly a **shared legacy connection** (the changelog's primary freeze condition), which was not constructed here; or a profile with a real session history. Until then the PR's own before/after screen recordings (`docs/media/1659/before-A.gif`, `before-B.gif`, `fixed-A.gif`, `fixed-B.gif`) are the evidence the PR carries for the freeze; those GIFs were not verified by this task.

## 8. Exact commands (as run, in order)

All work ran on the Spark via raw `ssh spark` (never `fleet`, never the Mac). Every ssh transport exited 0 (SSH_EXIT=0). Driver script bodies were supplied to each invocation as heredoc stdin; the measurement driver is verbatim in Appendix B. (This runner's guard forbids reproducing some git verbs in a command line — including in documentation text — so the two history-fetching steps are stated as prose with their exact arguments; the verbatim ssh transcripts remain in the run's task logs.)

1. **Fresh trees + SHAs + tool versions** (exit 0): over `ssh spark`, a shallow depth-1 working copy of branch `fix/1659-enter-responsiveness` of `https://github.com/Agent-Field/CodeAF` was materialized at `/home/santosh/src/codeaf-measure`, and of branch `dev` at `/home/santosh/src/codeaf-measure-dev`; then `git -C <each> rev-parse HEAD`, `go version`, `tmux -V`, `python3 --version`. SHAs in section 2.
2. **Builds** (exit 0 each):
   `ssh spark '( cd /home/santosh/src/codeaf-measure && make build ) & ( cd /home/santosh/src/codeaf-measure-dev && make build ) & wait'`
3. **Discovery runs** (exit 0): `ssh spark 'cat > /tmp/m1659-disc.sh && sh /tmp/m1659-disc.sh'`, then the same for `/tmp/m1659-disc2.sh`.
4. **Mandated measurement, runs 1-4** (exit 0): `ssh spark 'cat > /tmp/m1659-driver.sh && sh /tmp/m1659-driver.sh'` (script verbatim in Appendix B).
5. **Mandated measurement, runs 5-8** (exit 0): `ssh spark 'cat > /tmp/m1659-driver2.sh && sh /tmp/m1659-driver2.sh'` (same body; evidence dir 2, run index 5..8).
6. **Supplementary probe, round 1** (exit 0): `ssh spark 'cat > /tmp/m1659-driver3.sh && sh /tmp/m1659-driver3.sh'` (Appendix B body plus skill-folder generation; loop-var bug, section 5).
7. **Supplementary probe, rounds 1-3** (exit 0): `ssh spark 'cat > /tmp/m1659-driver4.sh && sh /tmp/m1659-driver4.sh'`.
8. **Read-only history** (exit 0): over `ssh spark`, a blobless no-checkout history-only copy of `https://github.com/Agent-Field/CodeAF` at `/home/santosh/src/codeaf-full` was materialized, then read-only `git -C /home/santosh/src/codeaf-full log --oneline 06a3283dd74385bf3c86ffc799c47b51b24fb0f7..e23a74f277b01d6d33d2639561946fc31aab1ce7` and `git -C /home/santosh/src/codeaf-full show --stat e23a74f277b01d6d33d2639561946fc31aab1ce7`.
9. **Landing**: the Spark report was pulled back into this task's copy at `docs/evidence/1659-enter-timing.md`; see section 10's delivery note for where it must finally live.

Two guard refusals are recorded for transparency, both before anything ran: one attempt included `git fetch` in the ssh line ("git fetch is not yours to run") — pivoted to fresh trees, which the colleague's note also recommended; one attempt wrote directly to the requester's checkout path — refused by the runtime's write rule (writes stay in the task's copy), which is why the landing note exists.

## 9. Exit codes

| step | command | exit |
|---|---|---|
| fix tree materialized | shallow depth-1 copy of `fix/1659-enter-responsiveness` | 0 |
| dev tree materialized | shallow depth-1 copy of `dev` | 0 |
| build fix tree | `make build` in `/home/santosh/src/codeaf-measure` | 0 (BUILD_FIX_EXIT=0) |
| build dev tree | `make build` in `/home/santosh/src/codeaf-measure-dev` | 0 (BUILD_DEV_EXIT=0) |
| discovery 1 | `sh /tmp/m1659-disc.sh` | 0 (DISC_EXIT=0) |
| discovery 2 | `sh /tmp/m1659-disc2.sh` | 0 |
| mandated driver, runs 1-4 | `sh /tmp/m1659-driver.sh` | 0 (DRIVER_EXIT=0) |
| mandated driver, runs 5-8 | `sh /tmp/m1659-driver2.sh` | 0 (DRIVER2_EXIT=0) |
| probe driver 3 | `sh /tmp/m1659-driver3.sh` | 0 (DRIVER3_EXIT=0; loop-var bug, section 5) |
| probe driver 4 | `sh /tmp/m1659-driver4.sh` | 0 (DRIVER4_EXIT=0) |
| history-only copy | read-only history at `/home/santosh/src/codeaf-full` | 0 (CLONE_FULL_EXIT=0) |
| every `ssh spark` transport | all invocations | 0 (SSH_EXIT=0) |
| all measurement rows | 32 mandated + 16 probe | OK, 0 timeouts, 0 NO_HOME |
## 10. Profile handling, secrets, and the delivery note

- **Fresh isolated profile for every single run** (32 mandated runs + 16 probe runs + 2 discovery runs): `PROFILE=$(mktemp -d ...)`, `HOME=PROFILE`, `CODEAF_HOME=PROFILE` (the profile root env var, confirmed from `cmd/codeaf` tests' use of it), workspace `PROFILE/work`, session destroyed (`rm -rf PROFILE`) at run end.
- **The user's real codeaf profile, keys and data were never touched** — never read, never written, never listed. The app's entire world lived under the run's own temporary PROFILE.
- **No secrets logged.** A fabricated, shape-valid non-credential `OPENROUTER_API_KEY=sk-or-v1-FAKEFAKEFAKEFAKEFAKEFAKEFAKE` is exported to the app so the home/conversation transition happens at all: with no key, Enter on home reopens the provider-connect gate and there is no conversation transition to measure (observed in discovery). The value is not a credential and appears only in the driver scripts; the setup screen is skipped when it is set, so it is never displayed, echoed, or captured. No real key was used anywhere. Model calls with it fail auth harmlessly; nothing was spent.
- Captures contain no key or token material (spot-checked: A-ready, B-ready and home captures on both trees).
- **Delivery note.** The work order's deliverable sentence names a full path in the requester's `af-1743-land` checkout (`docs/evidence/1659-enter-timing.md` relative). This task's runtime allows writes only inside its own copy ("what you write in your copy comes home on its own"), so this file is delivered at `docs/evidence/1659-enter-timing.md` **in the task's copy** and the caller must land it at the work order's named path. Content and relative path match exactly; nothing else is missing.

## 11. Standing orders and the conversation record

- **Nothing on GitHub was touched** — no comments, pushes, PR edits, or merges (hard rule of this job). PR #1743 is untouched by this task; nothing merged. Consequently the standing order about issue tags and milestones had no applicable surface (this job owns no issue or PR edits); it is honored by inaction, not violated. The production-readiness standing order is likewise not applicable: this job ships no code.
- **Attribution standing order:** this task made no commits (the deliverable is a file). The usual trailers close this document and apply to any future commit carrying this evidence.
- **Parallelism standing order:** builds and discovery ran in parallel where safe; the measurement runs were deliberately **serial and interleaved**, because concurrent load on the box would corrupt the timing numbers being measured. That is a property of the measurement, not a lack of parallelism.
- **Conversation record check:** the record's lines (fix commit `e23a74f27` applied on the PR's remote head and pushed to `fix/1659-enter-responsiveness`) match what was measured (branch head == `e23a74f277b01d6d33d2639561946fc31aab1ce7`). No statement in the record contradicts the work order. One environmental note arrived mid-task and was honored: the shared codeaf checkout on the Spark carries uncommitted changes and aborts checkouts, so all builds used this task's own fresh trees.

## 12. Artifact index (on the Spark)

- `/tmp/m1659-evidence/` — mandated runs 1-4: `results.csv` (Appendix A), per-run home and ready pane captures, `log.txt`.
- `/tmp/m1659-evidence2/` — mandated runs 5-8: same layout.
- `/tmp/m1659-evidence3/`, `/tmp/m1659-evidence4/` — probe runs.
- `/tmp/m1659-driver.sh`, `/tmp/m1659-driver2.sh`, `/tmp/m1659-driver3.sh`, `/tmp/m1659-driver4.sh` — driver scripts (1-2 canonical, 3-4 probe).
- `/tmp/m1659-disc.sh`, `/tmp/m1659-disc2.sh` — discovery scripts.
- Builds: `/home/santosh/src/codeaf-measure/bin/codeaf` (fix, buildinfo.rev e23a74f), `/home/santosh/src/codeaf-measure-dev/bin/codeaf` (dev, buildinfo.rev 06a3283).

---

Assisted-by: CodeAF (glm-5.3)

Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>

## Appendix A — raw measurement CSVs (verbatim from the Spark)

```csv
### /tmp/m1659-evidence/results.csv
tree,flow,run,t0_ns,tready_ns,ms,bound_ms,polls,status,flip_ms
fix,A,1,1791065508228629933,1791065508244304759,15.7,11.7,1,OK,15.7
fix,B,1,1791065509261962154,1791065509283319469,21.4,14.7,1,OK,NA
dev,A,1,1791065511130251905,1791065511133490198,3.2,3.2,0,OK,3.2
dev,B,1,1791065512149934329,1791065512171539916,21.6,6.7,3,OK,NA
dev,A,2,1791065514011843368,1791065514020844039,9.0,6.1,1,OK,9.0
dev,B,2,1791065515033266069,1791065515049041807,15.8,5.2,2,OK,NA
fix,A,2,1791065516873216324,1791065516882950020,9.7,6.6,1,OK,9.7
fix,B,2,1791065517895926116,1791065517921404558,25.5,17.1,2,OK,NA
fix,A,3,1791065519750287550,1791065519759203452,8.9,5.2,1,OK,8.9
fix,B,3,1791065520770654668,1791065520808063178,37.4,19.0,2,OK,NA
dev,A,3,1791065522719917589,1791065522742190138,22.3,6.4,3,OK,22.3
dev,B,3,1791065523756131280,1791065523770922072,14.8,5.9,2,OK,NA
dev,A,4,1791065525576195031,1791065525589419181,13.2,3.9,2,OK,13.2
dev,B,4,1791065526600576256,1791065526612516739,11.9,8.9,1,OK,NA
fix,A,4,1791065528439879546,1791065528448844952,9.0,5.3,1,OK,9.0
fix,B,4,1791065529459891869,1791065529480200190,20.3,5.2,3,OK,NA
### /tmp/m1659-evidence2/results.csv
tree,flow,run,t0_ns,tready_ns,ms,bound_ms,polls,status,flip_ms
fix,A,5,1791065875978508979,1791065875999653849,21.1,4.7,3,OK,21.1
fix,B,5,1791065877012560615,1791065877034536942,22.0,6.4,3,OK,NA
dev,A,5,1791065878830881741,1791065878834830580,3.9,3.9,0,OK,3.9
dev,B,5,1791065879848203235,1791065879870243530,22.0,7.2,2,OK,NA
dev,A,6,1791065881687913857,1791065881692305705,4.4,4.4,0,OK,4.4
dev,B,6,1791065882704824119,1791065882712892566,8.1,4.1,1,OK,NA
fix,A,6,1791065884513809359,1791065884517809991,4.0,4.0,0,OK,4.0
fix,B,6,1791065885529145683,1791065885538352756,9.2,5.6,1,OK,NA
fix,A,7,1791065887343408791,1791065887353426345,10.0,6.4,1,OK,10.0
fix,B,7,1791065888365673992,1791065888387620176,21.9,9.8,2,OK,NA
dev,A,7,1791065890260999119,1791065890269991983,9.0,5.3,1,OK,9.0
dev,B,7,1791065891283311298,1791065891299412159,16.1,5.3,2,OK,NA
dev,A,8,1791065893085615554,1791065893095196548,9.6,5.4,1,OK,9.6
dev,B,8,1791065894106865652,1791065894126425399,19.6,5.9,3,OK,NA
fix,A,8,1791065895917031332,1791065895925639172,8.6,5.4,1,OK,8.6
fix,B,8,1791065896939313096,1791065896959593997,20.3,6.2,3,OK,NA
### /tmp/m1659-evidence3/results.csv
kind,tree,flow,run,t0_ns,tready_ns,ms,bound_ms,polls,status
probe,fix,A,1,1791066146444055474,1791066146462982081,18.9,15.5,1,OK
probe,fix,B,1,1791066147473978433,1791066147497573594,23.6,4.3,4,OK
probe,dev,A,101,1791066150026670070,1791066150039966557,13.3,4.0,2,OK
probe,dev,B,101,1791066151050380164,1791066151103148665,52.8,49.0,1,OK
### /tmp/m1659-evidence4/results.csv
kind,tree,flow,run,t0_ns,tready_ns,ms,bound_ms,polls,status
probe,fix,A,1,1791066334340404560,1791066334348589488,8.2,4.4,1,OK
probe,fix,B,1,1791066335360678719,1791066335380868229,20.2,5.2,3,OK
probe,dev,A,1,1791066337871069219,1791066337878169257,7.1,4.0,1,OK
probe,dev,B,1,1791066338889193489,1791066338893296169,4.1,4.1,0,OK
probe,dev,A,2,1791066341372153628,1791066341375796495,3.6,3.6,0,OK
probe,dev,B,2,1791066342387262513,1791066342396425898,9.2,4.7,1,OK
probe,fix,A,2,1791066344870314564,1791066344878165312,7.9,4.2,1,OK
probe,fix,B,2,1791066345889344339,1791066345895790498,6.4,3.5,1,OK
probe,fix,A,3,1791066348343537654,1791066348351000126,7.5,4.0,1,OK
probe,fix,B,3,1791066349362690587,1791066349382587403,19.9,3.1,4,OK
probe,dev,A,3,1791066351856688902,1791066351865961279,9.3,5.1,1,OK
probe,dev,B,3,1791066352875382109,1791066352879059296,3.7,3.7,0,OK
```

## Appendix B — measurement driver script (verbatim: /tmp/m1659-driver.sh on the Spark)

```sh
#!/bin/sh
# 1659 measurement driver: real tmux keypress -> observable pane-capture boundary
FIXBIN=/home/santosh/src/codeaf-measure/bin/codeaf
DEVBIN=/home/santosh/src/codeaf-measure-dev/bin/codeaf
FAKE=sk-or-v1-FAKEFAKEFAKEFAKEFAKEFAKEFAKE
EV=/tmp/m1659-evidence
OUT=$EV/results.csv
rm -rf $EV; mkdir -p $EV
echo "tree,flow,run,t0_ns,tready_ns,ms,bound_ms,polls,status,flip_ms" > $OUT
echo "driver start $(date -u +%FT%TZ)" > $EV/log.txt
uptime >> $EV/log.txt

ms() { awk -v a="$1" -v b="$2" 'BEGIN{printf "%.1f",(b-a)/1e6}'; }

run_one() {
  tree=$1; bin=$2; idx=$3
  S=m1659_${tree}_${idx}
  D=$(mktemp -d /tmp/m1659-run.XXXXXX)
  mkdir -p "$D/work"
  tmux kill-session -t $S 2>/dev/null
  tmux new-session -d -s $S -x 100 -y 30 -c "$D/work" "env HOME=$D CODEAF_HOME=$D OPENROUTER_API_KEY=$FAKE $bin"
  t=0
  while [ $t -lt 300 ]; do
    tmux capture-pane -p -t $S > $EV/poll.txt
    grep -q "What would you like to work on?" $EV/poll.txt && break
    t=$((t+1))
  done
  if ! grep -q "What would you like to work on?" $EV/poll.txt; then
    echo "$tree,home-setup,${idx},0,0,NA,NA,NA,NO_HOME,NA" >> $OUT
    tmux kill-session -t $S 2>/dev/null; rm -rf "$D"; return 1
  fi
  cp $EV/poll.txt $EV/${tree}-run${idx}-home.txt
  tmux send-keys -t $S "hello timing test"
  sleep 0.5

  # FLOW A: Enter at home -> conversation view
  t0=$(date +%s%N)
  tmux send-keys -t $S Enter
  tlast=0; tready=0; tflip=0; n=0
  while [ $n -lt 2000 ]; do
    t=$(date +%s%N)
    tmux capture-pane -p -t $S > $EV/poll.txt
    if ! grep -q "What would you like to work on?" $EV/poll.txt; then
      [ "$tflip" = "0" ] && tflip=$t
      if grep -q "project:" $EV/poll.txt; then tready=$t; break; fi
    fi
    cp $EV/poll.txt $EV/prev.txt
    tlast=$t; n=$((n+1))
  done
  if [ "$tready" = "0" ]; then
    echo "$tree,A,${idx},${t0},0,NA,NA,${n},TIMEOUT,NA" >> $OUT
  else
    cp $EV/poll.txt $EV/${tree}-A-run${idx}-ready.txt
    [ "$tlast" = "0" ] && tlast=$t0
    echo "$tree,A,${idx},${t0},${tready},$(ms $t0 $tready),$(ms $tlast $tready),${n},OK,$(ms $t0 $tflip)" >> $OUT
  fi

  sleep 1.0
  # FLOW B: Ctrl+T inside conversation -> new-chat roster view
  t0=$(date +%s%N)
  tmux send-keys -t $S C-t
  tlast=0; tready=0; n=0
  while [ $n -lt 2000 ]; do
    t=$(date +%s%N)
    tmux capture-pane -p -t $S > $EV/poll.txt
    if grep -q "esc keeps the chat you were in" $EV/poll.txt && grep -q "recent sessions" $EV/poll.txt; then
      tready=$t; break
    fi
    cp $EV/poll.txt $EV/prev.txt
    tlast=$t; n=$((n+1))
  done
  if [ "$tready" = "0" ]; then
    echo "$tree,B,${idx},${t0},0,NA,NA,${n},TIMEOUT,NA" >> $OUT
  else
    cp $EV/poll.txt $EV/${tree}-B-run${idx}-ready.txt
    [ "$tlast" = "0" ] && tlast=$t0
    echo "$tree,B,${idx},${t0},${tready},$(ms $t0 $tready),$(ms $tlast $tready),${n},OK,NA" >> $OUT
  fi
  tmux kill-session -t $S 2>/dev/null; rm -rf "$D"
}

i=1
while [ $i -le 4 ]; do
  if [ $((i % 2)) -eq 1 ]; then order="fix dev"; else order="dev fix"; fi
  for tree in $order; do
    if [ "$tree" = "fix" ]; then bin=$FIXBIN; else bin=$DEVBIN; fi
    echo "round $i tree $tree $(date -u +%FT%TZ)" >> $EV/log.txt
    run_one "$tree" "$bin" "$i"
    sleep 1
  done
  i=$((i+1))
done
uptime >> $EV/log.txt
echo "driver end $(date -u +%FT%TZ)" >> $EV/log.txt
cat $OUT
echo "--- log ---"
cat $EV/log.txt
echo "--- evidence files ---"
ls $EV
echo DRIVER_EXIT=$?
```

Probe driver (verbatim: /tmp/m1659-driver4.sh; driver3 is identical except its make_skills reused the loop variable i)

```sh
FIXBIN=/home/santosh/src/codeaf-measure/bin/codeaf
DEVBIN=/home/santosh/src/codeaf-measure-dev/bin/codeaf
FAKE=sk-or-v1-FAKEFAKEFAKEFAKEFAKEFAKEFAKE
EV=/tmp/m1659-evidence4
OUT=$EV/results.csv
rm -rf $EV; mkdir -p $EV
echo "kind,tree,flow,run,t0_ns,tready_ns,ms,bound_ms,polls,status" > $OUT
echo "probe driver4 start $(date -u +%FT%TZ)" > $EV/log.txt
uptime >> $EV/log.txt
ms() { awk -v a="$1" -v b="$2" 'BEGIN{printf "%.1f",(b-a)/1e6}'; }
make_skills() {
  for root in "$1/.claude/skills" "$1/.codex/skills" "$1/work/.claude/skills"; do
    mkdir -p "$root"
    k=1
    while [ $k -le 100 ]; do
      mkdir -p "$root/s$k"
      awk -v n=$k 'BEGIN{print "name: s" n; print "---"; for(j=0;j<200;j++) print "padding padding padding padding padding padding padding."}' > "$root/s$k/SKILL.md"
      k=$((k+1))
    done
  done
}
run_one() {
  tree=$1; bin=$2; idx=$3
  S=m1659p2_${tree}_${idx}
  D=$(mktemp -d /tmp/m1659-probe.XXXXXX)
  mkdir -p "$D/work"
  make_skills "$D"
  echo "probe run $tree $idx skill files: $(find $D -name SKILL.md | wc -l)" >> $EV/log.txt
  tmux kill-session -t $S 2>/dev/null
  tmux new-session -d -s $S -x 100 -y 30 -c "$D/work" "env HOME=$D CODEAF_HOME=$D OPENROUTER_API_KEY=$FAKE $bin"
  t=0
  while [ $t -lt 300 ]; do
    tmux capture-pane -p -t $S > $EV/poll.txt
    grep -q "What would you like to work on?" $EV/poll.txt && break
    t=$((t+1))
  done
  if ! grep -q "What would you like to work on?" $EV/poll.txt; then
    echo "probe,${tree},home-setup,${idx},0,0,NA,NA,NA,NO_HOME" >> $OUT
    tmux kill-session -t $S 2>/dev/null; rm -rf "$D"; return 1
  fi
  tmux send-keys -t $S "hello timing test"
  sleep 0.5
  t0=$(date +%s%N)
  tmux send-keys -t $S Enter
  tlast=0; tready=0; n=0
  while [ $n -lt 2000 ]; do
    t=$(date +%s%N)
    tmux capture-pane -p -t $S > $EV/poll.txt
    if ! grep -q "What would you like to work on?" $EV/poll.txt; then
      if grep -q "project:" $EV/poll.txt; then tready=$t; break; fi
    fi
    tlast=$t; n=$((n+1))
  done
  if [ "$tready" = "0" ]; then
    echo "probe,${tree},A,${idx},${t0},0,NA,NA,${n},TIMEOUT" >> $OUT
  else
    cp $EV/poll.txt $EV/${tree}-A-run${idx}-ready.txt
    [ "$tlast" = "0" ] && tlast=$t0
    echo "probe,${tree},A,${idx},${t0},${tready},$(ms $t0 $tready),$(ms $tlast $tready),${n},OK" >> $OUT
  fi
  sleep 1.0
  t0=$(date +%s%N)
  tmux send-keys -t $S C-t
  tlast=0; tready=0; n=0
  while [ $n -lt 2000 ]; do
    t=$(date +%s%N)
    tmux capture-pane -p -t $S > $EV/poll.txt
    if grep -q "esc keeps the chat you were in" $EV/poll.txt && grep -q "recent sessions" $EV/poll.txt; then
      tready=$t; break
    fi
    tlast=$t; n=$((n+1))
  done
  if [ "$tready" = "0" ]; then
    echo "probe,${tree},B,${idx},${t0},0,NA,NA,${n},TIMEOUT" >> $OUT
  else
    cp $EV/poll.txt $EV/${tree}-B-run${idx}-ready.txt
    [ "$tlast" = "0" ] && tlast=$t0
    echo "probe,${tree},B,${idx},${t0},${tready},$(ms $t0 $tready),$(ms $tlast $tready),${n},OK" >> $OUT
  fi
  tmux kill-session -t $S 2>/dev/null; rm -rf "$D"
}
i=1
while [ $i -le 3 ]; do
  if [ $((i % 2)) -eq 1 ]; then order="fix dev"; else order="dev fix"; fi
  for tree in $order; do
    if [ "$tree" = "fix" ]; then bin=$FIXBIN; else bin=$DEVBIN; fi
    echo "probe round $i tree $tree $(date -u +%FT%TZ)" >> $EV/log.txt
    run_one "$tree" "$bin" "$i"
    sleep 1
  done
  i=$((i+1))
done
uptime >> $EV/log.txt
cat $OUT
echo "--- log ---"
cat $EV/log.txt
echo DRIVER4_EXIT=$?
```
