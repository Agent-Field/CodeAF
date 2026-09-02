---
kind: fixed
title: the tagged e2e lane runs on any machine, and the folder landing test is green on a Mac
pr: 356
surface: [build]
invalidates:
  - "`internal/e2e`'s harness located the person's credentials at a fixed path, `/home/santosh/.aforge/config.json`, so on any other machine the families, standing and phase-clock lanes SKIPPED with \"no provider credentials\" even with `OPENROUTER_API_KEY` set. The profile is now `home.Dir()/config.json`, read before the throwaway `AFORGE_HOME` is put in front of it — so `go test -tags e2e ./internal/e2e/` needs a key, tmux, `bin/aforge` and the person's own `~/.aforge/config.json`, and honours `AFORGE_HOME` if they set it."
  - "`TestAFolderLandingRefusesToWriteOverThePersonsOwnEdit` failed on every Mac and was not on the known-red ledger: it compared the landing sentence against the raw `t.TempDir()` path, and the door canonicalizes the ground (`/private/var/…` for `/var/…`). The test now asks for the canonical spelling. Nothing about the sentence a person reads changed."
---

Both were found while verifying #333 on a Mac: the real-model lane had never run here, and the one red test in the engine suite was the harness's spelling of a temp path.
