# Native web tools for PlanDB task workers

## Decision

Keep filesystem work and PlanDB coordination in bash. Expose web search and
page fetching as native `web_search` and `web_fetch` tools on the same worker
belt, with exactly one tool call per response. Retain the existing shell
commands as an alternate interface to the same implementations.

## What was missing

The session task belt already appends `searchTools`, and older graph workers
inherit the parent's provider and fetcher. The active PlanDB runner constructs
standalone workers through `CrewFactory`, `BashWorker.Run` and `NewBeltWorker`.
It passed neither web dependency, so conditional tool composition removed both.
The worker page also claimed only bash existed, despite the belt supporting
other native capabilities. The envelope accepts any carried tool, but its
retry diagnostic incorrectly required a bash call.

## Ownership and execution

`CrewFactory` binds `search.Live` to its explicit run profile. Every task role,
new child, retry and wake passes through that factory. `BashWorker` carries the
pair into `session.Config`; the existing tool registry owns schemas, argument
validation, bounds, provider errors and execution. The live resolver rereads
settings for each operation without performing a network availability probe.
The worker cannot edit its settings through this addition. Both policy pages
allow one available tool call, rather than requiring bash for every action.
A leaf reads the full ask for its owned requirements, but parent coordination
instructions are not reassigned to it; it follows its own work order.

A search returns titles, URLs and snippets; the worker chooses a URL and calls
fetch. Each operation receives the task's context, so run cancellation and wall
limits reach the network implementation. Provider failures return tool errors
which the model can act on. No new credentials, subprocess bridge, vendor
implementation or independent retry loop is introduced.

A bash wrapper alone would also work, but its subprocess must rediscover the
profile and its record is a bash command. Native calls preserve the direct
context and tool events, and make web use inspectable without parsing shell
strings. PlanDB stays in bash because it already has the run-bound shim and
lifecycle ownership checks.

## Records and compatibility

Every native action is recorded using the existing step recorder. The additive
optional `tool` field identifies the action beside its original argument object
and observation. Old trajectories still decode with an empty tool name. Native
web actions have no shell exit code, so a fetch cannot satisfy a declared shell
check merely by succeeding. Failed observations remain visible; no synthetic
successful search or fetch is recorded.

## Acceptance

Deterministic coverage executes search and fetch through the actual worker,
asserts the backend was called, and checks successful and failed observations,
tool identities and absence of shell exit codes. Factory coverage asserts an
explicit profile pin and a settings change after worker construction.

Live acceptance uses OpenRouter `deepseek/deepseek-v4.1-flash` for work,
planning, checking and auxiliary calls. Run a research task which searches,
fetches a discovered URL, writes an answer with citations, and finishes through
PlanDB. Check actual task trajectories and recorded model requests/responses;
a prose claim that it searched, a skipped run, or a substituted model fails
acceptance. Evidence remains outside the source tree.
