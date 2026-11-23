---
kind: fixed
title: Solo factory jobs can be approved by their signed-in caller
pr: 1851
surface: [engine]
invalidates: []
---

Solo jobs no longer require an assignment to approve a plan or proof. The caller still signs in with GitHub and gets the five-second Undo window; team jobs still require their assignee. Solo reads retain the caller identity for personal Dismiss and Stop Undo while hiding assignment details.
