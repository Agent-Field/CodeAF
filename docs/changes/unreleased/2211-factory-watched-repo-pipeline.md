---
kind: fixed
title: A repository just turned on can start its first job
pr: 2211
surface: [factory]
invalidates: []
---

The queue now says what the pipeline of a watched repository with no jobs says,
its default budget included. Before, such a repository read a budget of 0, and
the app disabled Start now with "No budget is stated".
