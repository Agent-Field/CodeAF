---
kind: fixed
title: codeaf keeps Factory page charges when a model call fails
pr: 0
surface: [engine]
invalidates: ["Failed AI read and Look closer calls could disappear from Spending"]
---

AI read and Look closer now keep each reported provider charge on Spending,
including when a generation fails. Successful calls still count once.
