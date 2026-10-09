# Mock engine for browser tests

`installMockEngine(page, scenario)` intercepts `**/api/engine/**` with
`page.route`, so specs never need a model or a bridge. Call it before
`page.goto`.

```ts
import { installMockEngine } from './support/mock-engine';
import { withTasks } from './support/scenarios';

const engine = await installMockEngine(page, withTasks());
await page.goto('/');
// ...drive the UI, then:
expect(engine.calls.some(c => c.path.endsWith('/turn'))).toBe(true);
```

## Scenarios (`scenarios.ts`)

`plainReply`, `toolsReply`, `withTasks`, `pendingQuestion`, `streaming`, and in
`scenarios-v2.ts` `richReply` (update, narrated steps, edit, bash, fetch, a
generated image, workspace files) plus tray questions (`trayQuestions`,
`timedProposal`, `waitingConsent`). Each
returns a `Scenario`: `initial` snapshot fields (merged over a valid snapshot:
model `deepseek/deepseek-v4.1-flash`, persistent, non-empty `sessionFile`),
`turns` (one scripted reply per accepted turn, the last repeats), `taskPages`,
`tools`, optional `manual` and `fail`. Spread to customise, for example
`{ ...plainReply(), initial: { entries: [] } }` for a fresh conversation.

## Timing

`route.fulfill` sends one finite body. The events stream therefore delivers
every record newer than `after` and then closes (the client reports a closed
connection); with nothing new it stays open until a record exists. The client
re-reads `GET /sessions/{id}` after each turn, stop and answer, so state is
reliable through snapshots. For timing, use `manual: true`: a turn records the
user message and `running: true`, and the reply lands when the spec calls
`engine.advance()`. `engine.update(patch)` changes the snapshot at any point.
`fail: { turn: 409 }` forces an HTTP error on one endpoint.

## Handle

`calls` (method, path, body of every request), `snapshot()`, `advance()`,
`update(patch)`. Task pages are served from `taskPages[taskId]`, tool output
from `tools[callId]`, workspace files from `files[path]` (GET /files and
POST /files/stat; anything else stats as missing). POST /questions/hold and
POST /tasks/{id}/{note|amend|pause|resume|cancel} are accepted and logged (a
note is added to the task page). Answering removes that question and records
its decision in `recentOutcomes`. While running, `mode: 'queue'` holds the
message until the reply lands and `mode: 'steer'` records a steer entry.
Terminals: `scenario.terminals` seeds terminals and jobs (`id`, optional
`command`, `state`, `exitCode`, `output` as raw terminal text). The mock serves
`/terminals` list, start, state, `stream` (finite body from `?after`), `output`
(plain text), `input` (appended to the log), `resize`, `close` and `remove`.
`terminal-client.spec.ts` shows them through the typed client.

`mock-engine.spec.ts` shows each endpoint through `fetch` only.
