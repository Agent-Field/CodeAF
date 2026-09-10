---
kind: fixed
title: a stalled check under load keeps its stall account, person manual labels match ##, and the e2e suite has honest timeouts
pr: 820
surface: [chat, engine, docs]
invalidates:
  - "A second checking attempt that could not be made (window already under the floor after building the checker) was folded into `asked twice` over `nobody could check it in …`, which claimed two calls when only one ran and erased the stall sentence. `secondAuditOutcome` keeps the first attempt's account and adds the window-closed clause (#803)."
  - "`aforge manual \"…\"` with no key printed labels the test looked for as `[permissions ·`; the person's door is `## permissions ·`. Both openers are one exported spelling now (`PersonSectionOpen` / `ModelSectionOpen`)."
  - "`go test -tags e2e -timeout 40m ./internal/e2e/` was the documented door for the whole tagged package. The package does not fit in forty minutes; `make test-e2e-tui` is TestTUIE2E alone under 40m, and `make test-e2e` is the full package under 120m."
---

Workstream D of the reliability wave: the #803 load race on an unattended
stalled check, the person's manual chrome labels, and an honest e2e budget split.
Live hold-out MISSes on `TestManualOnTheWire` remain model-appetite residual when
the tool is not opened; floors already sit under the lowest measured pass.
