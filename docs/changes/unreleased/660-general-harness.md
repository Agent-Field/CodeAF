---
kind: changed
title: Work starts from the original request and checking keeps recoverable evidence
pr: 660
surface: [chat, engine]
invalidates:
  - "Unattended sessions asked another model to rewrite every request before starting. They now preserve the complete original request immediately and read a retained destination only when it affects completion."
  - "Long checker results could lose their continuation footer. The 8000-byte cap now keeps ordinary read offsets, and other long observations name the existing saved-output file."
  - "A command failing before and after work could hide a newly failing case. Named failures are now compared; unchanged or unreadable baseline failures still do not prove the requested result works."
  - "The conversation benchmark tried BSD stat syntax on Linux and could read filesystem text as a zero timestamp. It now selects the native timestamp syntax, so revision timing checks receive only the file modification time."
---

These shared harness changes apply across writing, research, data and code.
Deferred destination reading cannot add commands after the baseline was taken.
