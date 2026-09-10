# One year with aforge: a startup CTO's accumulated workspace

Superseded explanatory study, 2026-09-09. The person found the large boxes
unhelpful; continue with [INFORMATION-GRAPH.md](INFORMATION-GRAPH.md). [YEAR-ONE.svg](YEAR-ONE.svg) answers the
person's request to see their whole personal aforge after a year, rather than
only a request-to-execution flow. Fictional data and statuses; this is a proposed
visualization, not an implemented interface or an accepted new product contract.
[PRODUCTION-WORK.svg](PRODUCTION-WORK.svg) retains the narrower execution-flow
study that led to this question.

## What the picture means

The workspace accumulates familiar folders, conversations, work, outputs and
sourced context. A startup CTO might return to Product, App & production,
Customers, Launch & growth, Company & finance and Hiring, alongside Travel,
Learning and Home. These names illustrate one person's organization; nobody must
create these departments or configure a permanent agent for each folder. Starting
in ordinary chat remains possible before the right placement is known.

Folders group records. Meaningful links connect records across those groups:
a release plan guides a release, that release supports a customer pilot, and a
budget decision constrains a hire. The drawing summarizes those links at folder
boundaries; the detailed source and target must be inspectable. A reference is
not a copy, an authorization, or an automatic instruction to start work. The
illustration uses a convenient nesting; it does not impose a canonical parent
on collections that can be reached from more than one place.

Ongoing work retains a purpose, scope, permitted actions and history. Its runs
come and go. A folder can therefore show work running now, waiting for its next
check, or requiring the person's call. A responsibility lasting a year does not
mean an agent executes continuously for a year. Monitoring also requires real
tool access and an available aforge runtime, separately from the deployed app.

## Proposed ways to look at the same records

- A daily overview emphasizes current work, meaningful changes and the person's
  pending calls, with a route back to sources and controls.
- Folder navigation gives predictable places to return, with older chats and
  outcomes available without crowding today's view.
- An optional map reveals selected relationships and expands a folder into its
  contents. At year-long scale, aggregate before zooming and reveal connections
  on selection rather than drawing every historic message and link at once.

This can be understood as a graph with nested groupings, without requiring a
new graph database, a global organizational chart, or a graph-first TUI. The
view reads existing owners and references; it must not maintain competing state.
These are design recommendations, not a decision that the home screen will be a
clickable graph. Exact filtering, summaries, navigation and controls remain to
groom. Memory and decisions are inspectable context with source and applicability,
not a folder badge that silently grants blanket authority.

## What exists versus what the illustration combines

The current engine already has standing work, scheduled firings, run history,
setup approval and pause/stop controls. Collections can reference chats, work,
artifacts and other collections; the draft backend adds explicitly sourced,
revisable shared context for chats and ordinary workers. Current collections do
not supply this new home view, and the richer context is not yet consumed by
scheduled firings. Automatic decision capture, global relevant discovery and
live cross-chat consultation are also not established by the diagram.

Implementation sources inspected in the backend review worktree:
`internal/manual/chat/keeping-an-eye.md`,
`internal/manual/chat/collections.md`, `internal/workspace/workspace.go`,
`internal/session/organization.go`, `internal/session/standing_contract.go`,
`internal/session/standing_run.go`, `internal/standing/standing.go`, and
`internal/tui3/standingpage.go`. Implementation evidence is tracked separately in
[DISCUSSION.md](DISCUSSION.md).

Next discussion: test this accumulated-work picture with the person, then settle
what they need to see and do at the overview, folder and individual-work levels.
Keep organization distinct from relevance, activation and permission throughout.
