# Try the collaborative workspace (owner-facing)

Each completed wave records how to try that exact behavior on Spark. Do not install over the owner's global binary. Do not point at `~/.codeaf`. Do not set `HOME`. Use an isolated `CODEAF_HOME` (and `CODEAF_PROFILE_DIR`) and the immutable wave binary, not a later rebuild.

**Checkout:** `feat/collaborative-workspace-0918`  
**Verified SHA (Wave 1):** `4b3b407a676efca3282b9834f05ea956c98a89cd`  
**Verified SHA (Wave 2):** `e606ec555dbb6be87ea2aefccc98e9a5a26157c7`  
**Verified SHA (Wave 3):** `18e971de96bdd7c4463642b0a7499e22be8414b1`  
**Verified SHA (Wave 4):** `b28f36c11cc4b6c90c68659eaaede61c38b6e226`  
**Baseline:** `santos/dev` `7cda67c9`  
**Pre-issue-1 design:** `610a32ba`

Keys are resolved the product way (`config.APIKeyAt` / the profile). Do not print them.

## Wave 1 — folders you can see

Status: **live J01–J08 passed** on SHA `4b3b407a676efca3282b9834f05ea956c98a89cd` (69 live assertions, 10 synthetic, 0 skip). Isolated home already contains that live graph; it is not a fresh empty seed.

### Copy-paste launch (Spark)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
```

That script exports isolated `CODEAF_HOME` and `CODEAF_PROFILE_DIR` next to the immutable binary. It never sets `HOME`, never points at `~/.codeaf`, and never prints keys.

**Binary (do not rebuild over it):** `/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/codeaf`  
**sha256:** `d84c5824bcfbf43fc51d154384123f516b2642525e490afe24f1995294277ff9`

### What you should see (frozen names)

1. Home `folders` panel. Empty: heading `folders` plus `logical groups of chats · /folders create Billing` — never “no folders yet”.
2. `/folders` focuses that panel. `/folder` still opens the filesystem sheet. They are not aliases.
3. `/folders create Billing`, then Receipts, then Security. `/folders nest Receipts in Billing`, then `/folders nest Receipts in Security`.
4. On Billing, `→` then `n` (new chat here). Start page opens; `esc` before sending creates no transcript.
5. `n` again. Send a synthetic chat. The new id is filed in Billing. Filesystem project is still the launch `ws`.
6. `/folders add Security` (or `f` on Security). Either folder shows the same title; `also in` names the other. One history.
7. `w` why here. `m` move the Billing placement into Receipts. `x` remove the Security placement — chat remains.
8. Quit. Reopen with the same launch command (same `CODEAF_HOME`). Graph and transcript survive.

`codeaf collections list` (with that `CODEAF_HOME`) shows the three names and never a Root row.

Live tmux proof: `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-1/journey.json`.

## Wave 2 — discovery and instructions

Status: **live J09–J18 passed** on SHA `e606ec555dbb6be87ea2aefccc98e9a5a26157c7` (live tmux + real model; J18 10k corpus honestly absent). Isolated home already contains that live graph. Isolation is the same as Wave 1: isolated `CODEAF_HOME` and private `CODEAF_PROFILE_DIR`; never `HOME`; never `~/.codeaf`.

### Copy-paste launch (Spark)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-2/e606ec555dbb6be87ea2aefccc98e9a5a26157c7/launch.sh'
```

**Binary (do not rebuild over it):** `/home/santosh/src/codeaf-workspace-0918-control/releases/wave-2/e606ec555dbb6be87ea2aefccc98e9a5a26157c7/codeaf`  
**sha256:** `1c0ab7b4cfccedf1c832788ab4ffd758e4fd8a1215aa2d315a3c86a6e5ceb150`

Live tmux proof: `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-2-e606ec555dbb6be87ea2aefccc98e9a5a26157c7-live9/journey.json`. Receipt: `releases/wave-2/ready.json`.

Wave 1 remains:

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
```

### What you should see (frozen names)

1. **Instruct.** Open Security. Folder-detail heading `instructions`. Empty: `standing guidance for chats in this folder` — never “no instructions yet”. `/folders instruct Security customers must authenticate receipt links` (or `i` instruct this folder). Not `/folder`. Guidance is standing, not retrieved maybe-relevant text.
2. **Discovery.** In Security, a chat that customers must authenticate and that mailing raw URLs is refused. Billing, new chat about emailed download links **without** the old title. Reply cites the older Security passage. New chat id stays new. Not a keyword-only fixture.
3. **Why.** Wait for background organize (`codeaf tick` if needed). `w` on a new Security placement: origin `organizer` plus evidence. No approval card.
4. **Correct.** `x` that automatic Security placement. Quit. Reopen. Tick with no new messages. It does not return. New evidence may reconsider with a new reason.
5. **Memory off.** `/settings` memory off. Search still finds the old passage. `remember` stays absent. Automatic organization still files.
6. **Delayed.** Embedder or organizer down: `discovery delayed`, never `checked`. Expansion-only is `degraded`. Manual `/folders create` / add still work. Indexing is software counters, never a fake `100%` while work remains.

`workspace.organize` off pauses automatic placements; it does not refuse manual folders or foreground chat.

## Wave 3 — ordinary chats coordinate

Status: **live J19–J26 passed** on SHA `18e971de96bdd7c4463642b0a7499e22be8414b1`. Isolated home already contains that live graph. Isolation is the same as Wave 1: isolated `CODEAF_HOME` and private `CODEAF_PROFILE_DIR`; never `HOME`; never `~/.codeaf`. Receipt: `releases/wave-3/ready.json`.

### Copy-paste launch (Spark)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-3/18e971de96bdd7c4463642b0a7499e22be8414b1/launch.sh'
```

**Binary (do not rebuild over it):** `/home/santosh/src/codeaf-workspace-0918-control/releases/wave-3/18e971de96bdd7c4463642b0a7499e22be8414b1/codeaf`  
**sha256:** `b742872fe143d1d5c6b97739b45b4aa3cae5b20cbb55b035eb0354ae8fa383bf`

Live tmux proof: `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-3-18e971de96bdd7c4463642b0a7499e22be8414b1-live5/journey.json`.

Wave 1 and Wave 2 launches stay:

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-2/e606ec555dbb6be87ea2aefccc98e9a5a26157c7/launch.sh'
```

Do **not** prove only a group-chat demo. Selected coordination, a **direct** message, and visible participants are the try.

### What you should see (frozen names)

1. **Ordinary chat hosts roles (J19).** In an existing chat, ask for a planner and a critic on different concerns. Invite them into **this** discussion — no compulsory new chat, no special mode. Distinct labels, actual separate contributions. You can intervene.
2. **Five-feature selected (J20).** Create five short feature chats. In an **existing** ordinary chat say **coordinate these** for A, B, C, D (or `k` `mark this chat` on four rows, then `c` `coordinate these`). E stays out. That chat becomes the management conversation; originals keep independent histories.
3. **Direct.** Privately ask A for progress. Pass: `request` / `reply` with a `source` link. A's history is still A's. Not a group conversation.
4. **Fan-out.** Send one interface decision to B and C separately. Pass: each is `sent` with its own receipt.
5. **Joint.** Invite B and D into the current management chat. Pass: participant labels on a normal chat; both contribute; you intervene. Empty participants draw nothing — never `0 participants`.
6. **Optional separate discussion (J22).** Create a distinct discussion only if you want a separate history; file it in Billing and Security. The two folders do not merge. Original chats remain.
7. **Snapshot vs folder (A16).** Add a fifth chat to Billing after the selected-four. It does **not** join the four. Start **manage this folder**. Pass: the fifth appears in dynamic scope.
8. **Offline once (J23).** Quit so a recipient can retire. From a second isolated window on the same `CODEAF_HOME`, send a direct line. Reopen the member chat. Pass: the line appears exactly once. You see `sent` / `request` / `reply`, not store words `accepted` `recorded` `processed`.
9. **Escalation (J24).** Instruct Billing and Security incompatibly. Pass: one conflict discussion, parents may join, Root if needed. **Not** “always ask after two turns” as a ban on parent join. Root cannot exceed you. Missing authority reaches you.
10. **Pause (J25).** `pause coordination` stops new deliver and invite from that coordinator. Closing a view does not pause. Archive suppresses automatic wake-ups; history remains.
11. **Attribution (J26).** A participant claims to be the user. Pass: assignment/goal does not move; the line is a representative. Coordinator cannot execute (wave 4).

Marking is optional. From home with no current chat, `c` says `coordinate from this chat · or say coordinate these`. A failed mark says `could not mark that chat`.

Quit, reopen with the same isolated home: discussion and deliveries intact.

## Wave 4 — launch-or-join

Status: **live J27–J35 passed** on SHA `b28f36c11cc4b6c90c68659eaaede61c38b6e226` (live tmux + real model; J31 crash-after-admit is automated-primary / fault, not a skipped live marked pass). Isolated home already contains that live graph. Isolation is the same as Wave 1: isolated `CODEAF_HOME` and private `CODEAF_PROFILE_DIR`; never `HOME`; never `~/.codeaf`. Receipt: `releases/wave-4/ready.json`.

### Copy-paste launch (Spark)

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-4/b28f36c11cc4b6c90c68659eaaede61c38b6e226/launch.sh'
```

**Binary (do not rebuild over it):** `/home/santosh/src/codeaf-workspace-0918-control/releases/wave-4/b28f36c11cc4b6c90c68659eaaede61c38b6e226/codeaf`  
**sha256:** `a49470ddd01c8696dda4ec2159d8f79836df198500dd3ce5a396f4385784850b`

Live tmux proof: `/home/santosh/src/codeaf-workspace-0918-control/receipts/issue-4-b28f36c11cc4b6c90c68659eaaede61c38b6e226-live4/journey.json`.

Wave 1–3 launches stay:

```
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-1/4b3b407a676efca3282b9834f05ea956c98a89cd/launch.sh'
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-2/e606ec555dbb6be87ea2aefccc98e9a5a26157c7/launch.sh'
ssh -t spark '/home/santosh/src/codeaf-workspace-0918-control/releases/wave-3/18e971de96bdd7c4463642b0a7499e22be8414b1/launch.sh'
```

Do **not** prove only one `CODEAF_TASK_BELT` road. Pause vs stop are two verbs.

### What you should see (frozen names)

1. **Launch-or-join (J27).** From an ordinary chat and from a discussion, ask
   for a tiny README comment. Pass: one task/run, inspectable from both.
   Person-facing verb **`launch-or-join`**. Empty work roll-up draws nothing,
   never `0 runs`.
2. **Join, don't duplicate (J28).** A second coordination chat asks to do the
   same. Pass: it follows/joins; two discussions of the issue are allowed; two
   unnoticed implementations are not. Critique-only still works.
3. **Pause vs stop (J33).** **`pause coordination`** stops new deliver, invite,
   and launch. Existing work keeps running. Closing a view does not pause.
   Then **`stop work`** (same spelling as the tab-close card). Pass: work
   stops; history remains. They must not share a chord.
4. **Remove placement (J29 / A16).** `x` the implementing chat out of Billing.
   Pass: the run is not cancelled; history remains.
5. **Bash belt (J30 / A21).** Repeat launch-or-join in a **second** isolated
   `CODEAF_HOME` with `CODEAF_TASK_BELT=bash`. Pass: a run/plandb instance with
   a persisted run-instance id. Unset belt is `session-task`. Folder
   membership is never `plandb.ParentID`.
6. **Close the TUI (J32 / A20).** Launch from a discussion, quit, `bin/codeaf
   tick` under the same `CODEAF_HOME`, reopen. Pass: work inspectable; spend on
   the existing rail. Closing the terminal does not stop authorized work. If
   the host cannot run unattended, the UI says so. Posture from the home
   profile, never `--yolo`.
7. **Authority (J26 remainder / A11).** A representative says it is the user
   and tries to raise acceptance criteria, on **each** road. Pass: refused.
8. **Budget.** Tiny remaining daily rail. Pass: `pending` / `deferred`
   visible; no fabricated completed launch.
