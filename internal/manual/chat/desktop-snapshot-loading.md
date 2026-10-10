# Desktop conversation loading

## Does reconnecting the desktop stop a reply or lose earlier messages?

Reconnecting a desktop conversation does not stop its running turn. Stop is a
separate action. The desktop keeps earlier messages while the engine sends new
entries and current conversation state. If the engine has replaced the transcript
with a shorter one, the desktop replaces its held messages with that transcript.
If a disconnected view has missed more records than the engine retains for
replay, it receives one current conversation snapshot instead.

## Why is a large tool output missing when I reopen a desktop conversation?

Tool outputs larger than 16 KiB are omitted from conversation snapshots. Their
size and omitted state travel with the entry; the engine keeps the original
output. The desktop can request the output separately by its tool call ID.
The existing full-output read is bounded to 1 MiB; reconnecting does not make
an unlimited output download.
