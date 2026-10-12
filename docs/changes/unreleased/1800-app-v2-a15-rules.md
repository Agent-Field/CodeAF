---
kind: changed
title: v2 engine rules and the TUI model seats command
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "Fresh task proposals waited 15 seconds and ordinary tasks defaulted to $5. They now wait 12 seconds and default to $2; explicit profile settings stay unchanged."
  - "A standing run without a chosen rail defaulted to $5. It now defaults to $0.40; existing orders keep their saved rails."
  - "The model-seat command was /crew. It is /seats, with /crew accepted as an alias for one release."
  - "The app's numbered rules had no shared binary policy. internal/appapi/rules.go now owns the $20 setup default, question/output/quick-task rules, standing scopes and deterministic idle selection. Resource lanes still own route enforcement."
---

Idle means no message for seven days and no running turn, task or process. It
never closes an agent automatically. The built-in day fallback remains $500;
the app setup proposes $20, and the owner's shared-home decision makes a saved
day cap common to both surfaces. API.md records policy availability separately from
resource enforcement so constants do not advertise unfinished routes.
