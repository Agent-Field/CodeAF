# Try the collaborative workspace (owner-facing)

Each completed wave records how to try that exact behavior on Spark. Do not install over the owner's global binary. Do not point at `~/.codeaf`. Do not set `HOME`. Use an isolated `CODEAF_HOME` (and `CODEAF_PROFILE_DIR`) and the local `bin/codeaf` from the product checkout.

**Checkout:** `feat/collaborative-workspace-0918`  
**Baseline:** `santos/dev` `7cda67c9`  
**Pre-issue-1 design:** `610a32ba`

Keys are resolved the product way (`config.APIKeyAt` / the profile). Do not print them.

## Wave 1 — folders you can see

Status: **shape recorded; live commands pending SHA from `t-w1-live`.** This is not a live-chat pass.

**Binary, after `make build` on the product checkout:**

`/home/santosh/src/codeaf-collaborative-workspace-0918/bin/codeaf`

### Isolate (never `HOME`)

```bash
# from /home/santosh/src/codeaf-collaborative-workspace-0918
git rev-parse HEAD          # record as WAVE1_SHA once t-w1-live has run
make build
CODEAF_HOME=$(mktemp -d /tmp/codeaf-cw-i1-XXXXXX)
CODEAF_PROFILE_DIR=$(mktemp -d /tmp/codeaf-cw-i1-profile-XXXXXX)
WS=$(mktemp -d /tmp/codeaf-cw-i1-ws-XXXXXX)
chmod 0700 "$CODEAF_HOME" "$CODEAF_PROFILE_DIR"
git -C "$WS" init
# Copy credentials the product way. Never print the key.
# Pin model.talk=deepseek/deepseek-v4-flash and icons=plain.
```

Launch only that binary, with `CODEAF_HOME` and `CODEAF_PROFILE_DIR` set, from `$WS`. Pending live SHA: do not treat an unrecorded `git rev-parse` as the accepted journey.

### What you should see (frozen names)

1. Home `folders` panel. Empty: heading `folders` plus `logical groups of chats · /folders create Billing` — never “no folders yet”.
2. `/folders` focuses that panel. `/folder` still opens the filesystem sheet. They are not aliases.
3. `/folders create Billing`, then Receipts, then Security. `/folders nest Receipts in Billing`, then `/folders nest Receipts in Security`.
4. On Billing, `→` then `n` (new chat here). Start page opens; `esc` before sending creates no transcript.
5. `n` again. Send a synthetic chat. The new id is filed in Billing. Filesystem project is still `$WS`.
6. `/folders add Security` (or `f` on Security). Either folder shows the same title; `also in` names the other. One history.
7. `w` why here. `m` move the Billing placement into Receipts. `x` remove the Security placement — chat remains.
8. Quit. Reopen with the same `CODEAF_HOME`. Graph and transcript survive.

`codeaf collections list` (with `CODEAF_HOME`) shows the three names and never a Root row.

Live tmux proof, receipts, and the SHA that actually ran are `t-w1-live`.

## Wave 2 — discovery and instructions

Not yet. After issue 2: discovery / why / correct / instruct.

## Wave 3 — ordinary chats coordinate

Not yet. After issue 3: selected coordination, a direct message, and visible participants. Not only a group-chat demo.

## Wave 4 — launch-or-join

Not yet. After issue 4: launch-or-join and pause vs stop.
