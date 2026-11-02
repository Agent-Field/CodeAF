---
kind: added
title: Factory jobs can close locally while retaining work and spending
pr: 1899
surface: [engine, app]
invalidates:
  - "Factory chat-list refreshes silently reset explicit conversation models to the job default or a finished step’s execution model. Reads now preserve saved Main/Fallback choices; legacy display-placeholder fallbacks migrate to DeepSeek Flash without touching valid selections or explicit None."
  - "Drop was the only way to move an unfinished job into Closed. Close now revokes its worker lease and blocks further paid work and PR maintenance without deleting its branch, folder, conversations or accounting. Reopen preserves those facts and starts no work. GitHub is unchanged."
  - "An unset or blank fallback setting chose a nearest catalog neighbour. The shared default is now deepseek/deepseek-v4.1-flash; explicit model pairs and explicit fallback chains remain authoritative."
---
