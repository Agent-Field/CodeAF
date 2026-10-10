# Desktop queued message delivery

## What happens when a queued message is sent now in the desktop?

The desktop's queued-message client asks the engine to send the identified message
now. The engine removes it from the queue and steers it into the running turn, or
starts the next turn if nothing is running. The client reads the resulting
conversation; it does not send a second copy of the message through the composer.
The queue menu is a separate desktop control; client support alone does not make
that control available.

## Why does the desktop say that message has already been sent?

If a queued message has already left the queue when the request reaches the engine,
the engine refuses the change. The desktop drops that row and shows the muted
error "that message has already been sent" without a toast or Retry for that
message. The row leaves even if refreshing the conversation fails. Other failures
keep the queued message and show the engine's error.
