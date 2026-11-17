---
kind: fixed
title: With Memory off, nothing reads saved chats until the Memory page opens
pr: 2358
surface: [engine]
invalidates:
  - "With Memory off, a pass every two seconds listed saved chats and re-read them whenever one changed, to keep a count. Now the Knowledge offer asks for the count when it opens, and the last count stays. The offer itself appears as before."
---
