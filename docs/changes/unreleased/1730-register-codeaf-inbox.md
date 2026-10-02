---
kind: internal
title: CODEAF_INBOX is registered as operator plumbing, so release builds pass their tests again
pr: 1730
surface: [build]
invalidates:
  - "After #1689 added CODEAF_INBOX, every staging, rc and stable build failed TestRegistryCoversEveryUserFacingEnvironmentPin in its release test job; the pull-request gate never ran that test because #1689 did not touch internal/config. The variable is now on OperatorEnvPins and those builds publish again."
---
