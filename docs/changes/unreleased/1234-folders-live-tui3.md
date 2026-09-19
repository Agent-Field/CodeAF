---
kind: fixed
title: Folders inside-Esc, nested rename, and launch composer survive the place
pr: 1234
surface: [chat]
invalidates:
  - "carryEditor on every raiseHome/dropHome concatenated home sends (`message 0message 1`). The copy is across the Folders place only."
  - "Esc from New chat inside a folder landed on Home new conversation. Drill-in now opens on New folder so Down+Enter is New chat, and Esc returns to Folders."
  - "After nest, `/folders rename Receipts Invoices` said `no folder called Receipts` because RootSnapshot is parentless-only. Rename walks nested snapshots. Visible `r` on a nested row still opens the name box."
  - "Official J43 typed on the launch conversation then Folders showed `› say what you want done`. The launch composer now copies onto Folders at 120 and 80 col. The `/home` then alt+5 retry is not the only proof."
---
