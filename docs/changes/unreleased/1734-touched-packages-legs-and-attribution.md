---
kind: changed
title: touched packages run concurrently and attribute flaky or inherited failures
pr: 1734
surface: [build, docs]
invalidates:
  - "The touched job ran every changed package in sequence and charged its first failure to the PR. It now starts the needed tui3, session, unsharded codeaf and rest legs together, retries each named failure once, then compares persistent failures with the exact base before deciding."
  - "The PR gate restored a cache keyed only on go.sum, so weeks of compiled changes were never saved. The first dev push missing each build namespace's UTC-day key now saves it, and light saves modules by go.sum hash; PRs restore the newest compatible entries without saving, and GitHub evicts old days."
  - "Local touched proof had its own package walk and no failure attribution. It now uses CI's shared selector, partitions and classifier; tooling acceptance remains conditional on the changed scripts, Makefile or covered benchmark paths."
---

A flaky test remains a bug with an owner, but its first failure is not evidence
that this PR introduced it. Unnamed failures and more than five failed tests per
leg stay red without retries; a new persistent head-only failure still blocks
check. The classifier prints human test output, skips no test by name and
records warnings in the lowest-numbered open exact-title standing issue. The
shared ledger guard, suite lock, light-gate prerequisites and full workflow
remain unchanged.
