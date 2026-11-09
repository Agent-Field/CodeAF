---
kind: fixed
title: Session tests no longer leave a read-only Go toolchain behind in a temp home
pr: 2543
surface: [build]
invalidates:
  - "Each run of the internal/session tests could leave a codeaf-session-test-home-* folder holding a downloaded Go toolchain of about 500 MB that rm could not delete; the suite now keeps Go's module cache where the developer has it and removes read-only trees from its throwaway home."
---
This happened only where the system Go was older than the module's toolchain
line, as on one Mac, where two runs left 14.5 GB. Nothing in the product changed.
