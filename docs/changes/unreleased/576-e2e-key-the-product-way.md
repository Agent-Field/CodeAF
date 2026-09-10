---
kind: fixed
title: the end-to-end lanes ask for a key the way the product does, so a machine with one runs them
pr: 576
surface: [build]
invalidates:
  - "internal/e2e's lanes skipped unless OPENROUTER_API_KEY was exported. The product resolves a key THREE ways (config.APIKeyAt: OPENROUTER_API_KEY, then OPENAI_API_KEY, then the profile's `api_key` row), so a machine whose key was pasted into the first-run setup or typed into /settings — a machine that launches aforge and talks to a model all day — skipped the whole tmux suite and printed `ok`. Every gate now goes through one door, `liveKey`, which reads those same three roads in the same order, and the rigs export the key it found."
  - "TestStandingE2E's world skipped with `no provider credentials at <path>` when the person's own ~/.aforge/config.json did not exist. A missing profile file is no longer a skip: the key is resolved first, the profile is copied when it is there, and the throwaway AFORGE_HOME is given the resolved key through config.WriteAPIKey whichever road it came down."
  - "There is now an untagged structural gate beside the suite, TestEveryLaneAsksForItsKeyTheWayTheProductDoes in internal/e2e/livekeygate_test.go: it parses every lane's source and fails the pull request on an os.Getenv or os.LookupEnv of OPENROUTER_API_KEY or OPENAI_API_KEY outside the door. It needs no key, no tmux and no model, and it rides `make test-laws` like tuiwords_test.go's gate does."
---

A suite that skips is a suite claiming it could not be honest. This one was
claiming that on machines that had a key the entire time — the same shape as
#184, where the tmux lane was green for a week without asserting anything, and
the reason a skip has to be as hard to reach as a pass.
