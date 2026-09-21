---
kind: fixed
title: unread-config notice skips keys the product itself retired
pr: 1245
surface: [engine, chat]
invalidates:
  - "The unread-config-key notice named a top-level key that a shipped version once wrote as a settings row but that nothing reads at head, telling a person about an ignored key they never typed."
---
Seven retired keys (linear_mode, nerd_font, rail_state, work.workers,
memory.consolidation, practice_demand_pct, propose_new_skills) are skipped
silently by the unread check. A ledger of every top-level key the product has
ever written, plus a law test, keeps the set honest: adding a settings row
forces a ledger line, and removing one leaves its line behind and turns the law
red until the key is retired on purpose.
