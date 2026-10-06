# Usage counts and privacy

## What anonymous usage counts collect

codeaf sends anonymous session counts and provider-reported input/output token counts when telemetry is enabled. Usage receipts include a bounded routing-service category and public model family, never an exact model name, endpoint address, prompt, code, path or API key. Missing provider receipts have a missing status and no invented token total. Completed local provider receipts are appended before accounting returns, then sent every 30 seconds and at shutdown. Hosted chat currently sends turn aggregates with unknown provider and family; work lost before a turn ends is not guaranteed to appear.

## Turning anonymous counts off

Set `CODEAF_TELEMETRY=off`, `DO_NOT_TRACK=1`, or the telemetry switch in `/settings`. Project settings can also turn telemetry off. Disabled telemetry sends no usage receipts. The detailed public contract is in `docs/TELEMETRY.md`.
