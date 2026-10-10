# Desktop notifications and dock badge

## Why does the desktop app send system notifications

When codeaf is in the background, a new blocking question, approval or failed
item can send a system notification. Done and running work never notify.
Questions already seen while the app was focused stay quiet after you switch
away. Repeated records do not send another notification for the same item.

The notification title names the chat's place, or `codeaf` for an unplaced
chat. Its body names the conversation and includes the question's head when
the engine supplies one. No missing question text is invented.

## What happens when I click a desktop notification

Clicking brings the codeaf window forward and opens the conversation. The
notification also names its attention item so Next up can start there. A
question that has already been answered is not brought back.

## What does the desktop dock badge count

The dock badge counts questions and approvals waiting on you across all
conversations, including conversations with no open tab. Failed, done and
running work are excluded. Zero clears the badge. On Linux, whether the
launcher draws a badge depends on your desktop; Windows does not support
this badge count.

## When does the desktop ask for notification permission

Permission is checked on the first new background item that needs a system
notification. If needed, it is requested once per app window launch. A denial
does not prompt again for every question. Browser previews send no system
notifications and request no notification permission. Your operating system
may silence notifications even when the desktop bridge reports permission
as granted.
