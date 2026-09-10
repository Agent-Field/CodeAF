# Governing context and execution evidence

Accepted behavior: C23–C25. Maintained draft: #662. Baseline: `217da7951`.

## Contract

Existing collections retain navigation references. Explicit governing placements
are a separate typed relation in the same owner, allowing several placements.
Nothing in the migration promotes old membership into authority. Folder ancestry
uses only governing bindings and refuses cycles. A hold opts into descendants;
a direct binding alone does not imply subtree reach. These relations neither
move files nor grant tool permissions.

The existing standing owner retains governing instructions. A hold may explicitly
name folder scope instead of a legacy conversation/project/machine altitude.
The proposal must name the actual selected folders and descendant reach before
the person answers. Delegated approval cannot create this new scope. New answer
receipts distinguish person and delegated approval; older provenance stays unknown.
The receipt proves acceptance of a proposal, not verbatim extraction from a user
message. Automatic source-backed adoption without a card remains T12d.

Execution reads governing direction independently of mutation/timer capability.
Child workers receive a read-only resolver and an explicit owning-work reference.
An unattended occurrence belongs to its standing item; its temporary run folder
is not a new governing placement. Shared context remains information, never an
instruction or grant. The context used for execution is the context recorded.

## Cause and effects

Use the existing per-execution transcript journal as the durable owner. Record
the effective consumed inputs at the turn boundary and retain original tool-call
and result references. Effective standing content must be retained because the
standing owner rewrites its current revision; shared context already has immutable
revision history. Inspection must distinguish attempts, returned results and
unknown external effects. A successful shell/tool response alone is not proof of
a completed product goal. Do not store private reasoning as an explanation.

The first implementation identifies execution windows and selected inputs. It
must label absent parent-cause links and overlapping execution windows explicitly.
Ephemeral forked hands currently have no independent selection journal; their parent retains its existing fork call/result. Recording failures are disclosed for journaled turns. It does not promise a complete global event graph, external exactly-once effects,
or recording silent background decisions that never enter a session. Those gaps
remain visible in T12d/T13/T14 instead of being hidden by a new generic audit store.

## Functional assertions and delivery

- References never acquire governing scope, including after migration/reopen.
- Direct and opt-in descendant scopes, multiple paths, exception exclusion and
  alternate remaining placements resolve consistently.
- Paused rules do not govern, invalid folder scope cannot fall back to altitude,
  and background work cannot broaden bindings through the foreground tools.
- Normal chat, task, checker, fork and scheduled constructors preserve the correct
  owning scope while leaving mutation tools absent/read-only as appropriate.
- A receipt uses the exact selected input revisions/content, survives reopen,
  and links inspection to existing journal evidence without duplicating tool bodies.
- All compilation and test execution is on Spark. No tui3 or broad expensive
  acceptance runs. Passing these checks is not connector or live-model acceptance.

Schema boundary: standing schema 3 and organization schema 3. Stop/restart old
engines and tickers together before any deployment; mixed writers are unsupported.
This wave does not deploy or migrate the person's live installation.
