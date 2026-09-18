---
kind: fixed
title: the terminal audit names the gitignored build products a run left in the deliverable tree
pr: 1152
surface: [chat, engine]
invalidates:
  - "The terminal audit could not see a build product a subprocess left behind: it sorted only the created-file ledger (files the session's own tools wrote) and read the tree with `ls-files --others --exclude-standard`, which leaves out what .gitignore covers, so a latex run's leftover .aux/.log/.out sat in the deliverable tree for everybody's eyes but the audit's. The `reconciled` journal row now names them under `ignored` — build products, ignored by git, not in the landing — and only what appeared during the run, against the tree's own baseline photograph."
  - "An ignored build product is a report and never work for the sweep: it reaches no set the tidy acts on, no complete landing turns incomplete over one, and nothing is deleted for being on the list — an ignored target/ or node_modules/ a build made is removed, if ever, by a decision of its own."
---
