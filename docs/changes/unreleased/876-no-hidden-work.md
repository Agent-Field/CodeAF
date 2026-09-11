---
kind: fixed
title: nothing hidden happens on a turn — one file memo, one off-path reading, one deferred write, one door for a hand asking a model
pr: 876
surface: [chat, engine, docs]
invalidates:
  - "`read` on a picture, a recording or a video made an untagged, unannounced model call that could run for the ten-minute general completion bound. Every model call a HAND makes now goes through one door (`internal/session/toolask.go`): tagged `tool:<name>` in the call log, carrying the node inside a task, drawing a phase the person reads (`looking at error.png`, `listening to memo.m4a`, `watching run.mp4`), bounded by `lane.RoleTool.GiveUp()` and capped at what a tool result may show. A structural law fails the build on any `tools_*.go` that calls `completeWithModel` itself."
  - "`view_image` carried its own ten-minute window, `viewLookWindow = providerTimeout`. That var is DELETED. The bound is the role's — `lane.RoleTool` is a new row whose patience says a person is watching the tool row it holds open, so the give-up is ninety seconds and something is done about silence at ten. Its refusal still names the model and the file; the duration in it is now the role's."
  - "Nothing bounded how much an asked model could write, and the undirected look ran 588–1,256 completion tokens for an answer the belt pages to a cap anyway. The ask now carries `ai.WithMaxTokens` derived from `Agent.resultCaps().MaxBytes` — the same cap the answer is shown within. There is no factor and no chosen number."
  - "`git status --untracked-files=all` ran INLINE in every batch of ordinary chat that contained a shell command — ten to thirty milliseconds on a small repository, seconds on a large one, between the tools finishing and the next request being assembled. It is now taken with `offpath.Take` and settled at `lane.Hysteresis`, the quantity this build already calls the smallest difference in waiting anybody notices. A repository git can read inside that behaves exactly as before; one it cannot has the reading folded by the next batch instead of blocking the turn."
  - "`compactToolHistory` rebuilt the reduced form of the FROZEN prefix from scratch on every request of every tool round — a SHA-256 over the whole of every old tool result to find its pointer, a fresh copy of its text, a fresh view — work proportional to everything the conversation had ever read, paid again on each request. It is memoised per message on the agent (`Agent.toolCompact`), guarded per index by the result's call id and its byte weight, so a message the stubbing pass rewrote is recomputed and nothing else is. Measured: 1.34 ms → 13 µs at 100 rounds, 11.7 ms → 156 µs at 1600. `compactToolHistory` is now a method; loop.go's call site gained `a.`."
  - "The profile's `config.json` was read and parsed on EVERY call — `APIKeyConfigured` on every Enter, `FirstPrompt` on every turn, eight readers in `internal/config`. `readProfileConfig` now goes through `filememo.Stamped(SettingsGeneration, …)`: one stat per call, a parse only when the file's size or timestamp moved or this process persisted a write. The comment saying this package deliberately holds no cache is rewritten rather than left standing. `writeProfileValues` copies before adding its row, because the map a reader holds is the memo's."
  - "`renderSystemAt` re-read AGENTS.md and CLAUDE.md whenever the prompt's clock went stale — the came-back-to-the-terminal turn, and once per attached folder. `readInstructionFileWithin` now goes through the same `filememo` type, holding the bytes so a lean bound and a full bound share one read."
  - "`fixShelf.consult` — the READ path, asked after every failed tool call — persisted its two counters, four whole-file read-modify-write cycles per fail→fix→succeed iteration under a process-global lock. It writes nothing now. The counters are owed to `offpath.Write` and land behind the path or at `fixStore.settle()`."
  - "`stampUserLocked` did a whole `LoadMeta` + `SaveMeta` — read, MkdirAll, CreateTemp, write, rename — WITH THE AGENT LOCK HELD, in the moment between a person pressing Enter and their turn starting. The snapshot is still taken on the path; the disk is `offpath.Write`'s, coalesced so two messages a second apart owe one write, and `Agent.SettleMeta()` is the exit door."
  - "A conversation's working copy of a referred folder — a `git worktree add`, or a whole recursive copy — was cut INSIDE the first tool call that wrote a file, silently. It is now started when the folder is referred (`Agent.ReferPlace` → `Agent.startStandingTree`), so it runs while the person reads their own screen, and it says `preparing · a working copy of <folder>` for as long as it lasts wherever it runs. A cut that cannot be made is still not a refusal: the write goes to the real folder."
---

Three mechanisms, not eleven patches. `internal/filememo` is "a file read at most
once per change" (config.json, AGENTS.md, CLAUDE.md); `internal/offpath` is the
two shapes of work that must not happen on a person's path — `Take`/`Reading`
for a fact gathered beside the work and `Deferred`/`Write` for a write a read
path owes; `internal/session/toolask.go` is the one door a hand asks a model
through. Each carries its law in its doc comment and a test that fails the build
on the next copy of it.

Not memoised, deliberately: `fixStore.saveLocked` and `Agent.updateMeta` both
re-read their file inside a read-modify-write so that another window's
simultaneous change survives the merge. A memo there would be a memo in front of
a WRITE, which is a different thing with a different law.
