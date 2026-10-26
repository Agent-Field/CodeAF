# Documentation map

Choose the kind of knowledge you need. Product instructions, developer design,
contributor rules and historical records serve different readers.

## Product knowledge

| Need | Read |
| --- | --- |
| Install and start using codeaf | [Guide](GUIDE.md) |
| Find the exact behavior codeaf explains about itself | [Shipped manual map](../internal/manual/README.md) — separate chat and resident corpora |
| Learn the current chat's features | [Chat manual](../internal/manual/chat/what-i-can-do.md), [getting started](../internal/manual/chat/getting-started.md) |
| Understand Memory and knowledge from saved chats | [Memory](../internal/manual/chat/what-i-remember.md), [knowledge from saved chats](../internal/manual/chat/places.md#knowledge-from-saved-chats-inside-memory) |

The manual is product knowledge compiled into the binary. Repository development
notes and users' learned chat knowledge are separate; see [documentation ownership](DOCUMENTATION.md).

## Developer knowledge

Start with the [current architecture map](ARCHITECTURE.md). It identifies the
chat and resident surfaces, entry points, implementation owners and storage roles.

| Category | References |
| --- | --- |
| Current interface contracts | [Headless](HEADLESS.md), [remote architecture](REMOTE.md), [subharness execution](SUBHARNESS-CONTRACT.md), [pairing](ux-pairing-contract.md) |
| Spending and reporting | [Limits](LIMITS.md), [model usage accounting](model-usage-accounting.md), [telemetry disclosure](TELEMETRY.md) |
| Operation and verification | [Run a relay](relay.md), [test moving chats](testing-anywhere.md), [remote session test notes](remote-access-testing.md) — check the historical warning |
| Design rationale | [Visual design language](DESIGN-LANGUAGE.md), [home layout](HOME-BRIDGE.md), [thread UX](THREAD-UX.md), [background jobs](JOBS.md), [services](SERVICES.md), [multimodal](MULTIMODAL.md), [stored subharness design](SUBHARNESS.md) |
| Performance | [Performance rules and size budget](../PERF.md) |

A contract describes an interface that other code relies on. Design rationale
explains a decision and may include proposals or earlier revisions. Check scope
and implementation before treating design prose as a current feature list.

## Contributing

| Need | Read |
| --- | --- |
| Make a first contribution | [Contributor guide](../CONTRIBUTING.md) |
| Give a coding agent repository instructions | [AGENTS.md](../AGENTS.md), then [architecture](ARCHITECTURE.md) and recent [changes](changes/README.md) |
| Choose where documentation belongs | [Documentation ownership](DOCUMENTATION.md), [manual authoring](../internal/manual/README.md) |
| Follow contribution and release policy | [Rules index](rules/README.md): branches, CI, change entries and promotion |

## History

[Historical designs](history/README.md) preserve superseded decisions without
presenting them as the current product. [Unreleased change entries](changes/README.md)
explain recent changes; [CHANGELOG.md](../CHANGELOG.md) records releases. Neither
replaces an up-to-date manual or interface reference.
