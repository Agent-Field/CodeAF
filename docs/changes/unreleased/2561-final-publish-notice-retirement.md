---
kind: fixed
title: Closing chats retain publish notices after their last live listener retires
pr: 2561
surface: [chat, engine]
invalidates:
  - "A closing drive began retaining notices only when its own Close started. Agent close had already removed its last seal watch, so a background publish in between could announce displacement to no listener and leave the restored terminal without its notice. Notices now remain available to the closing receipt as soon as the last registered watch retires."
---
The closing receipt claims each retained notice once. Announcements before any
watch subscribed keep their previous behavior, and a remaining live watch still
receives its own notices without adding them to the closing receipt.

The final-publish acceptance fixture now waits for the actual writer close before
forcing displacement. A long retry interval never held successful background
publishes back, so the old fixture could demand a closing receipt for a notice
already shown in the live window.
