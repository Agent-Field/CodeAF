# Developer architecture

Current entry point for contributors and coding agents. This map separates the
chat surface from the resident surface and points to implementation owners.
Read [contributing](../CONTRIBUTING.md) and recent [change entries](changes/README.md)
before relying on an older design. This page describes code organization, not
user-facing promises; those belong in the [shipped manual](../internal/manual/README.md).

## Start with the surface

| Surface | Entry and implementation | Product knowledge |
| --- | --- | --- |
| Interactive chat | [chat launcher](../cmd/codeaf/chatv3.go), [terminal UI](../internal/tui3/), [session engine](../internal/session/) | [Chat manual](../internal/manual/chat/) |
| Scripted work | [commands](../cmd/codeaf/), [headless contract](HEADLESS.md) | [Running from the terminal](../internal/manual/chat/running-from-the-terminal.md) |
| Resident | [head](../internal/head/), [resident](../internal/resident/) | [Resident manual](../internal/manual/pages/) |

Chat and resident share parts of the repository, but their lifecycles and manual
corpora differ. Do not use the [historical resident architecture](history/resident-architecture.md)
as a description of the chat's storage or execution model.

## Follow a chat change

The launcher wires configuration, the workspace and the session to the terminal
surface. The surface presents state and user actions; the session owns the turn
loop, tools and task coordination. Start with these files, then follow the
specific action rather than reading either large package from beginning to end.

| Responsibility | Start here | Related reference |
| --- | --- | --- |
| Launch, resume and configuration | [chatv3.go](../cmd/codeaf/chatv3.go), [config](../internal/config/) | [Starting codeaf](../internal/manual/chat/starting-codeaf.md) |
| Screen, navigation and interaction | [tui3/app.go](../internal/tui3/app.go), [shared tui2 components](../internal/tui2/) | [Design language](DESIGN-LANGUAGE.md) |
| Turns, tools and task execution | [session/session.go](../internal/session/session.go), [session/task_run.go](../internal/session/task_run.go), [exec](../internal/exec/) | [Subharness contract](SUBHARNESS-CONTRACT.md) |
| Provider calls and model configuration | [provider](../internal/provider/), [config](../internal/config/) | [Usage accounting](model-usage-accounting.md), [limits](LIMITS.md) |
| Remote sessions and movable chats | [remote](../internal/remote/), [enginehost](../internal/enginehost/), [cell](../internal/cell/) | [Remote architecture](REMOTE.md), [remote testing](testing-anywhere.md) |
| Relay coordination across computers and workers | [relay server](../internal/relayserve/), [committed directory](../internal/raftdirectory/), [consensus core](../internal/raftcore/), [hosted relay](../relay/hosted/), [cluster command](../cmd/codeaf/cluster.go) | [Run a relay](relay.md), [coordination and workers](../internal/manual/chat/coordination-and-workers.md) |

## Find the right storage and knowledge owner

There is no single Markdown file that acts as every kind of memory. Keep these
concepts separate when implementing a feature or writing an agent's brief.

| Kind of knowledge | Owner | How it is maintained |
| --- | --- | --- |
| What codeaf knows about its product | [manual](../internal/manual/) | Maintained Markdown embedded in the binary, with separate chat and resident corpora |
| Profile knowledge learned from saved chats | [knowledge](../internal/knowledge/), [session integration](../internal/session/knowledge.go) | Native SQLite vault, background compiler, and human edits; see [knowledge architecture](knowledge.md) and [Memory behavior](../internal/manual/chat/places.md#knowledge-from-saved-chats-inside-memory) |
| Graph and memory storage mechanisms | [store](../internal/store/) | Read the relevant caller and tests before changing schema or replay behavior |
| What contributors know about this codebase | This page and [engineering references](README.md#developer-knowledge) | Reviewed documentation committed beside the code |

Repository documentation is not automatically part of the shipped manual, and a
user's project knowledge vault is not the repository's developer documentation.
A project-knowledge feature needs both developer guidance here when structure
changes and product guidance in the manual when behavior changes.

## Change profile knowledge through its owner

The knowledge vault owns source admission, background compilation, publication,
search, and human overrides. The session history reader decides which saved records
can enter the vault. Memory renders published pages and does not run compilation
on the UI loop. Agent recall reads the same effective publication.

Start with the [knowledge architecture](knowledge.md) for entry points and module
contracts. Preserve immutable sources, stable page identities, cancellation,
revision-checked edits, and deletion intent when changing this path. Keep provider
requests outside database write transactions and publish pages and search entries
as one revision.

## Contracts and design rationale

Current interface references include [headless behavior](HEADLESS.md),
[remote protocol](REMOTE.md), [subharness execution](SUBHARNESS-CONTRACT.md),
[pairing](ux-pairing-contract.md), [spend limits](LIMITS.md), and
[usage accounting](model-usage-accounting.md). Check their stated scope before
applying them to another surface.

Design rationale explains why a mechanism was chosen. Start with the relevant
page in the [documentation map](README.md#developer-knowledge); a design proposal
is not evidence that the behavior shipped. Superseded material belongs in
[history](history/README.md), linked from its replacement.

## Make a change safely

Use tests at the observable interface affected by the change. Update the matching
manual and reference in the same PR when commands, defaults, tools, limits or
refusals change. Follow [CI guidance](rules/ci.md) for focused validation and
[PERF.md](../PERF.md) for measured performance changes. Keep general repository
instructions in [AGENTS.md](../AGENTS.md), not in the product manual.
