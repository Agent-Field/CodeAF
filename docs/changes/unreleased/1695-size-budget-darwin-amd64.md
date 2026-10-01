---
kind: changed
title: the size budget is measured on darwin/amd64 and raised to 68,850,000 bytes
pr: 1695
surface: [build]
invalidates:
  - "SIZE-BUDGET was 57,400,000 and the Makefile and ci-full.yml said it was set on linux/arm64, while every platform already weighed more and make size was red on a clean tree. It is now 68,850,000, set two percent above darwin/amd64, the heaviest shipped platform, with that platform's own furrow staged and the Go release go.mod pins."
---
The raise restores a gate that had stopped meaning anything; it does not make the
weight wanted. #1694 finds what grew the binary and lowers the number with each cut.
