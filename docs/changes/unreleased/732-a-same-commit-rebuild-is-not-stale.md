---
kind: fixed
title: a rebuild of the same commit is not an older aforge
pr: 732
surface: [build, engine, docs]
invalidates:
  - "`buildinfo.Identity()` was `revision/dirty/builtAt` — the second the linker ran was part of what named an engine, so two `make build` runs on ONE unmodified commit answered with two identities. It is now the revision alone whenever the build can name its source: a clean tree's rebuild of one commit is the same engine, and only a modified tree or an unstamped build still carries the moment (there is no ambiguity between the two shapes — a source never contains a slash and the fallback always carries two)."
  - "Every window opened while another window was open, after a rebuild of the same commit, read `the engine on <machine> is an older aforge and is still holding work — it picks up this build the moment it goes quiet`. That sentence still exists and still fires for an engine built from another source; it no longer fires for the commit you are running. Any open window makes an engine busy (`HostSelf.Busy` is a surface attached, a turn running, or a question waiting), so this was every second window on a machine after a rebuild."
  - "`clearStaleEngineHost` compared `buildinfo.Identity()` inside itself. The body is now `clearStaleEngineHostFor(workspace, thisBuild)` and the identity is passed in — a test needs two builds of one source, and a test binary is only ever linked once. Both call sites and the door's behaviour are unchanged."
  - "`internal/manual/chat/running-on-another-machine.md` said the check `includes the source/build stamp even when the wire protocol has not changed`, which left a person reading their own rebuild as a different aforge. It now says the check asks which SOURCE the engine was built from, so building one commit twice is the same build and the window attaches without a word."
---

The display stamp was right all along — both binaries printed `aforge c85e10a19
built 2026-09-09 17:09`, in the same minute — while the door that decides whether
to attach read thirteen seconds of difference and called one of them old. The
news channel in `internal/tui3/notice.go` had already written the rule down for
its own purpose: rebuilding unchanged source must not repeat old news. The engine
road now rests on the same fact.
