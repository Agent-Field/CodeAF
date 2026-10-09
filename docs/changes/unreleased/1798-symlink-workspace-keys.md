---
kind: fixed
title: a folder reached through a symlink is one project and one engine, not two
pr: 1798
surface: [engine, chat]
invalidates:
  - "A folder reached through a symlink became two projects, two conversation histories and two engines. `codeaf engine --status --workspace <link>` missed the engine holding `<dir>`, and home's projects panel listed the folder once per spelling. engineWorkspace stopped at filepath.Clean, so each spelling hashed to its own enginehost host directory. engineWorkspace now resolves symlinks (filepath.EvalSymlinks after the absolute path is settled), so both spellings of one folder reach one project, one conversation history and one engine."
---

Found in the v0.7.1 happy-path pass on macOS, where `/tmp` is a link to
`/private/tmp`, and reproducible on any system with `mkdir d && ln -s d l`.
The workspace key-minting door in `cmd/codeaf` — the one every engine command
(`--daemon`, `--status`, `--stop`) and the host's workspace name go through —
expanded and cleaned the path but never resolved its links. Two spellings of
one folder then keyed two host directories in `internal/enginehost.where`
(sha256 of the cleaned string), so the person saw one folder while codeaf kept
two of everything. The fix names a workspace by its resolved path at that door;
a regression test drives a symlink through `engineWorkspace` and asserts both
spellings land on the same directory.