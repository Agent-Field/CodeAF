# Try the collaborative workspace (owner-facing)

Each completed wave records how to try that exact behavior on Spark. Do not install over the owner's global binary. Do not point at `~/.codeaf`. Do not set `HOME`. Use an isolated `CODEAF_HOME` (and `CODEAF_PROFILE_DIR`) and the immutable wave binary, not a later rebuild.

**Checkout:** `feat/collaborative-workspace-0918`  
**Verified SHA:** `4b3b407a676efca3282b9834f05ea956c98a89cd`  
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

Status: **names frozen** on this branch. Live J09–J18 is t-w2-live, not this lane. Isolation is the same as Wave 1: isolated `CODEAF_HOME` and private `CODEAF_PROFILE_DIR`; never `HOME`; never `~/.codeaf`. Use the wave-2 launch script once t-w2-ready cuts the immutable binary next to `releases/wave-2/`.

### What you should see (frozen names)

1. **Instruct.** Open Security. Folder-detail heading `instructions`. Empty: `standing guidance for chats in this folder` — never “no instructions yet”. `/folders instruct Security customers must authenticate receipt links` (or `i` instruct this folder). Not `/folder`. Guidance is standing, not retrieved maybe-relevant text.
2. **Discovery.** In Security, a chat that customers must authenticate and that mailing raw URLs is refused. Billing, new chat about emailed download links **without** the old title. Reply cites the older Security passage. New chat id stays new. Not a keyword-only fixture.
3. **Why.** Wait for background organize (`codeaf tick` if needed). `w` on a new Security placement: origin `organizer` plus evidence. No approval card.
4. **Correct.** `x` that automatic Security placement. Quit. Reopen. Tick with no new messages. It does not return. New evidence may reconsider with a new reason.
5. **Memory off.** `/settings` memory off. Search still finds the old passage. `remember` stays absent. Automatic organization still files.
6. **Delayed.** Embedder or organizer down: `discovery delayed`, never `checked`. Expansion-only is `degraded`. Manual `/folders create` / add still work. Indexing is software counters, never a fake `100%` while work remains.

`workspace.organize` off pauses automatic placements; it does not refuse manual folders or foreground chat.

## Wave 3 — ordinary chats coordinate

Not yet. After issue 3: selected coordination, a direct message, and visible participants. Not only a group-chat demo.

## Wave 4 — launch-or-join

Not yet. After issue 4: launch-or-join and pause vs stop.
