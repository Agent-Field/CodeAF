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

Not yet. After issue 2: discovery / why / correct / instruct.

## Wave 3 — ordinary chats coordinate

Not yet. After issue 3: selected coordination, a direct message, and visible participants. Not only a group-chat demo.

## Wave 4 — launch-or-join

Not yet. After issue 4: launch-or-join and pause vs stop.
