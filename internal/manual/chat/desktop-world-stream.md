# Desktop world stream

## How does the desktop keep track of other conversations?

The shared world client reads the engine's `/events?after=N` stream for conversation
rows, things waiting on you, background jobs, place changes and saved workspace
changes. Its readers share one connection per window. A disconnected client retries
with increasing delays from one second up to thirty seconds, resuming from its last
sequence and engine identity. A reset replaces the client's stored collections;
missing collections are cleared rather than kept as current information.

## Does the new world client already supply every desktop view?

No. The client and React hook are available as shared infrastructure, but existing
views still use their existing readers. The stream must first integrate the new
conversation-row and Inbox producers into its reset payload. The client does not
invent missing titles, jobs or questions, and a workspace notification only says
the saved document changed; it does not contain that document or a transcript.
