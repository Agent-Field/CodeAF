---
kind: fixed
title: the /crew panel's weak-checker warning counts a pinned checker
pr: 1501
surface: [chat, docs]
invalidates:
  - "The `/crew` panel's `no strong checker among the models you allow` warning was asked of the allowed models only. A strong checker pinned over a weak set left the warning on screen, and a weak checker pinned over a strong set had none. A pinned checker is now the one asked: a strong pin clears the warning, and a weak pin reads `checker pinned to <model> · open-ended work will be checked weakly`."
---
