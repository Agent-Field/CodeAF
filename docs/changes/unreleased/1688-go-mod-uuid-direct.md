---
kind: internal
title: go.mod lists github.com/google/uuid as the direct requirement it is
pr: 1688
surface: [build]
invalidates:
  - "Every `go build`, `go test` or `make build` on a clean `dev` checkout rewrote go.mod and stamped the binary `dirty=true`, because `internal/codexauth` imports `github.com/google/uuid` directly (since #1336) while go.mod still marked it indirect. go.mod now says direct, and a build leaves the tree clean."
---
