---
kind: fixed
title: a resident dispatch pass does not run again the leaf it just released
pr: 1225
surface: [engine]
---

A background resident pass could reclaim a leaf that the same pass had just
released to a worker, so one pass ran the leaf twice and spent its budget twice
before the pass ended. A leaf a pass dispatches is now held out of that pass's
own claim set and picked up by a later pass.
