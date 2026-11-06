# Client tools: tools an app adds to a conversation

## Client tools: connection, remote holders, limits and waiting

An app connected through `codeaf app` can attach tools and instructions to one
conversation. The model sees their names, descriptions and schemas; the app
checks arguments and answers calls. Child tasks, workers and clarification
conversations do not receive them.

Attaching tools does not open the conversation or start a turn. Changes apply
at the next model step. Each conversation allows 4 sets, 32 tools per set and
64 tools total. Replacing a set replaces its tools and instructions; an empty set
removes it. Tool names must be unique and cannot reuse engine tool names.

A collection keeps a conversation before its first message; reopen it by id
after a restart.

Set ids start with a lowercase letter, followed by lowercase letters, digits,
underscores, dots or hyphens, up to 64 characters. Tool names exclude dots and
hyphens. Descriptions take 1–2048 bytes; schemas are JSON objects with
`type: "object"`, up to 16 KiB. Instructions allow 4096 bytes per set. Tools
declare `read`, `edit` or `create`; reading-only contexts refuse changes.
The usual approval rules apply.

The wait defaults to 30 seconds; apps can choose 1–120 seconds per set.
120 seconds is the maximum. Stopping the turn ends the wait. Timeout or
disconnect leaves the outcome unknown: check before trying again.
codeaf does not retry calls automatically.
Results are compact JSON, cut at 64 KiB with the dropped-byte count. Error
answers take 1–4096 bytes.

Joined holders forward tools when supported; older holders refuse attachment.
Reconnect restores sets only to the same conversation. A chat held on another
computer stores sets until "Continue here" or take. Each connection owns its
tools and calls. Link loss ends calls when observed or at their wait limit;
later answers are refused.
