---
kind: fixed
title: a folder reached through a symlink is one project and one engine, not two
pr: 1798
surface: [engine, chat]
invalidates:
  - "A folder reached through a symlink became two projects, two conversation histories and two engines. `codeaf engine --status --workspace <link>` missed the engine holding `<dir>`, and home's projects panel listed the folder once per spelling. The key-minting doors stopped at filepath.Clean, so each spelling hashed to its own key. Every door that turns a path into a key now resolves symlinks first (filepath.EvalSymlinks, with the original spelling kept when the path does not resolve): engineWorkspace (the engine commands and the host's workspace name, including the default home), enginehost.where (the host directory every lookup, dial, spawn, lock and status is keyed by), v3Workspace (the session bucket and tools root), gitidentity's path fallback (the memory project key), and the typed /project pin. Both spellings of one folder reach one project, one conversation history and one engine."
---

Found in the v0.7.1 happy-path pass on macOS, where `/tmp` is a link to
`/private/tmp`, and reproducible on any system with `mkdir d && ln -s d l`.
`engineWorkspace` — the door every engine command (`--daemon`, `--status`,
`--stop`) and the host's workspace name go through — expanded and cleaned the
path but never resolved its links, so two spellings of one folder keyed two
host directories in `internal/enginehost.where` (sha256 of the cleaned string),
and the person saw one folder while codeaf kept two of everything.

The resolution was first added at `engineWorkspace` alone, and the review of
this pull request caught what that left split: the local surface still hashed
the alias its terminal carried while the daemon listened under the resolved
key, and the default home skipped the door entirely. The identity now resolves
at every layer that mints a key — the host directory inside `where` itself, so
lookup, dial, spawn, lock and status cannot disagree; the default home in
`engineWorkspace`; the session bucket and memory project key in `cmd/codeaf`;
the typed `/project` pin in `internal/tui3`. A regression test drives a symlink
through each layer and asserts both spellings land on one key.
