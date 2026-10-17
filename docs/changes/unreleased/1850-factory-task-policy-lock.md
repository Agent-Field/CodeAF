---
kind: fixed
title: Factory task model policy follows deferred mutex release
pr: 1850
surface: [engine]
invalidates: []
---

The saved model policy now releases its mutex with defer, satisfying the enforced lock law without changing model selection.
