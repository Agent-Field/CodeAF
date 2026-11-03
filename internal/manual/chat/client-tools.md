# Tools an app adds to a conversation

## Tools an app adds to a conversation: connection, limits and waiting

An app connected through `codeaf app` can attach its own tools and instructions
to one conversation. The model sees each tool's name, description and input
schema, and codeaf sends its calls to that app for an answer. The app checks the
arguments against its schema. Child tasks, workers and clarification
conversations do not receive these tools or instructions.

Tools appear only while the app is connected. Attaching them does not open the
conversation or start a turn. Changes take effect at the next model step.
Each conversation can hold 4 sets, with 32 tools per set and 64 tools total.
Replacing a set replaces all its tools and instructions; an empty set removes
only that set. Tool names must be unique within the conversation and cannot
reuse an engine tool's name.

A conversation held by a collection is kept even before its first message;
it can be reopened by id after a restart.

Set ids start with a lowercase letter, followed by lowercase letters, digits,
underscores, dots or hyphens, up to 64 characters. Tool names follow the same
rule without dots or hyphens. Descriptions take 1–2048 bytes; each input schema
is a JSON object with `type: "object"`, up to 16 KiB. Instructions take up to
4096 bytes per set. Each tool declares `read`, `edit` or `create`; reading-only
contexts refuse edits and creations. The usual tool approval rules still apply.

The default wait is 30 seconds. An app can choose 1–120 seconds per set;
120 seconds is the maximum. Stopping the turn ends the wait immediately.
A timeout or a disconnect during a call leaves its outcome unknown. Check what
the app did before trying again: codeaf does not retry that call automatically.
Results are compact JSON, cut at 64 KiB with the dropped-byte count. Error
answers take 1–4096 bytes.
