# Historical designs

Preserve reasoning that is useful for understanding older code and migrations.
These records are not the current feature catalog or instructions for a new
implementation. Start with the [current architecture](../ARCHITECTURE.md) or
[shipped product manuals](../../internal/manual/README.md).

| Record | Scope and replacement |
| --- | --- |
| [Resident architecture](resident-architecture.md) | Former root architecture document: resident graph, lease and design decisions. The current architecture entry point now maps both surfaces. |
| [Standing goals](../STANDING.md) | Removed v1 machinery. Current chat behavior is documented under [automations](../../internal/manual/chat/automations.md). |
| [Feature and hint catalog](../FEATURES.md) | Mixed-surface design inventory; not a verified catalog of current chat features. Use [chat capabilities](../../internal/manual/chat/what-i-can-do.md). |
| [Remote session acceptance notes](../remote-access-testing.md) | Historical acceptance snapshots and limitations. Use [remote architecture](../REMOTE.md) and the [maintained test guide](../testing-anywhere.md); revalidate any old command before use. |

Keep [change entries](../changes/README.md) and [release history](../../CHANGELOG.md)
in their existing locations. They explain when behavior changed, while current
manuals and references explain the resulting behavior.
