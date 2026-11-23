---
kind: internal
title: The GitHub sign-in's client-id and stand-in-address variables are registered as operator plumbing
pr: 2501
surface: [factory, build]
invalidates: []
---

TestRegistryCoversEveryUserFacingEnvironmentPin failed on factory/general-intake
from the day the browser sign-in landed (#2494), because CODEAF_GITHUB_CLIENT_ID
and CODEAF_GITHUB_LOGIN_URL were read but never registered.
