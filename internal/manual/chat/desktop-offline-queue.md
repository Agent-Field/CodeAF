# Sending while the desktop is offline

## What happens to a message I send while the engine is offline

The composer stays editable. A plain-text send is held in this pane and drawn as your message at 60% opacity until the engine records it. It is not left sitting in the composer. You can type the next message while those are waiting.

## Do messages I send offline go out when the engine reconnects

Yes. They go out in the order you sent them, once the engine answers again. A draft you did not send stays in the composer. Retry on the "Can't reach the engine" line checks the connection. It does not send that draft.

## What if I attach a file and send while offline

That send is not held. The draft and the attachments stay in the composer, the same as when a send is refused. Nothing is posted for them when the engine comes back until you send again.

## What if the engine refuses a message after it reconnects

That message comes back into the composer, with the engine's reason and Retry, the same as a refused send while the engine is reachable. Messages you sent after it stay held, and this one goes out ahead of them when you retry.
