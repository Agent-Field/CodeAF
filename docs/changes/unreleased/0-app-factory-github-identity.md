---
kind: fixed
title: Factory surfaces carry verified GitHub identities
pr: 0
surface: [engine]
invalidates: []
---

Runner owners, activity participants and join previews can carry names and
photos recorded at GitHub sign-in. Responses include only referenced people.
Setup reports the account verified by its connected credential separately
from the ambient GitHub CLI account. Returning from repository selection
continues that connection without asking for another token.
