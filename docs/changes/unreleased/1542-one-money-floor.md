---
kind: fixed
title: a spend under a hundredth of a cent reads <$0.0001 on every receipt, never $0.0000
pr: 1542
surface: [chat, docs]
invalidates:
  - "The receipts beside the Spending tab's limits (`… today`, `this one …`), the headless run's footer, `codeaf doctor` and the notebook wrote a spend under $0.00005 as `$0.0000`, while the tab's `today` row wrote the same day as `<$0.0001`. Every one of them now writes `<$0.0001`. The rule lives once, in `config.SubCent`."
---
