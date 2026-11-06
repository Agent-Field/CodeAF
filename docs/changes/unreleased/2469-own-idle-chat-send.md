---
kind: fixed
title: Typing into your own idle chat no longer says it's available to continue here
pr: 2469
surface: [app, remote]
invalidates: []
---

Sending a message into your own idle background chat opens it again when its
hold is still free. Previously, the old ownership watcher could mistake the
chat's own release for losing ownership. Send could then refuse with
`This chat is available to continue here.`

Closing now stops that watcher before releasing the hold, while still waiting
for the final upload to finish. Checks for another computer taking the chat
and recovery after a failed continuation keep their existing rules.
