# Unread config key notice findings

## Task

Carry the unread top-level `config.json` keys computed by `config.Load` into the existing one-time conversation notice in both hosted and `--no-host` chat. The notice must name the keys, use the existing `notices.json` mechanism, stay silent for a correct flat config, and show again only for a previously unseen changed key set. Production code is intentionally out of scope for this findings commit.

## Single-source transport path

The only key classification remains the computation currently inside `warnUnreadProfileKeys` in `internal/config/config.go:361-388`. Refactor it to return its sorted `[]string`, and make the existing log format that exact returned value. No downstream code may inspect config keys or reconstruct the set.

Retain that value from the existing `config.Load` call in `openV3ProcessWith` (`cmd/codeaf/chatv3_process.go:128-154`) on the process/launch data. From there the two paths are:

- `--no-host`: `openV3ProcessWith` -> `v3Process` -> `openV3Launch` / `v3Seam.bundle` -> the existing launch notice input -> `tui3.Options.Notice` -> the existing dim conversation note consumed by `internal/tui3/app.go:2870-2917`.
- Hosted: `openV3ProcessWith` during `bootEngine` (`cmd/codeaf/engine.go:563-613`) -> `remote.Engine` -> a dedicated unread-key field on `remote.Welcome` (`internal/remote/wire.go`) populated by `welcomeLocked` (`internal/remote/server.go:1149-1162`) -> `hostEntryNotice` / `hostOptions` (`cmd/codeaf/chatv3_host.go`) -> `tui3.Options.Notice` -> the same existing dim conversation note.

The hosted wire field is required because the engine and surface are separate processes. Neither stderr, the per-profile chat log, nor `host.log` reaches the conversation.

The displayed sentence should use the existing conversation-note surface and plain wording such as: `config.json keys are not read: models; models fell back to defaults.` The names come directly from the transported sorted list.

## Notice gate

Use the existing `noticeBoard` and `noticeLedger` persistence in `internal/tui3/notice.go` and `internal/tui3/notice_ledger.go`, with `maxShown=1`. Keep a stable display row id, `unread-profile-config-keys`, and use this dynamic ledger gate id:

`unread-profile-config-keys-<sha256hex>`

Compute the full lowercase SHA-256 hex digest from `encoding/json.Marshal` of the already sorted shared list. This is deterministic and unambiguous for arbitrary key strings, matches the notice id shape, and does not introduce a second unread-key computation. Gate eligibility, per-session shown state, show, and retire by the dynamic gate id while rendering by the stable row id. An empty list arms no notice. The same set remains retired across launches; a new set gets a new gate and shows once; returning to an already retired set stays silent.

The ledger must be opened for the profile whose config was loaded. In hosted mode, do not silently gate an engine-profile warning against an unrelated client profile.

## Focused verification plan

1. Extend the focused config test around `TestWarnUnreadProfileKeys` to prove the shared function returns a sorted list and the log formats that same result: `go test ./internal/config -run TestWarnUnreadProfileKeys -count=1`.
2. Add focused remote wire/server coverage proving the sorted unread list survives `Engine -> Welcome` serialization and receipt.
3. Add focused notice-board tests proving: empty input is unarmed; the first display is persisted; restart with the same gate is silent; a changed list displays once; returning to an old list is silent; and the rendered conversation note names the keys.
4. Add focused command-path tests for both `--no-host` launch transport and hosted `hostOptions` transport.
5. Exercise the acceptance case with a built real binary and a real engine plus client, never `quietAgent` or another fake agent: nested `{"models":{"tiers":{"reflex":"..."}}}` shows a notice naming `models` once; a correct flat config shows nothing; a second launch is silent; changing the unread set shows once again in hosted and `--no-host` modes.
6. Run only focused `go test -run` commands while implementing, then leave the coordinator's prescribed pre-PR gates to the coordinator.

## Implementation step

Implement the shared return value first, then transport it on `v3Process` and `remote.Welcome`, and finally arm the existing notice board with the hashed dynamic gate. Focused tests will cover config classification, wire transport, local and hosted option assembly, and ledger behavior. No downstream layer will read or classify `config.json`.
