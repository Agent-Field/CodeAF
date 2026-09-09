# What is designed, implemented, and still open

Updated 2026-09-09. This answers the continuation question: are the remaining
slices merely waiting to be coded? **No. The product direction is agreed more
widely than the detailed behavior or implementation.** This is a status record,
not a new architecture decision.

| Area | Agreed direction | Still needs decisions | Current implementation |
| --- | --- | --- | --- |
| Organization | Familiar collections and chats; one item can be reached from several places without copies. | Final navigation and how people inspect/correct relationships. | Stable memberships, nested collections, live owner resolution. |
| Shared information | Keep sources, revisions and explicit applicability. Membership grants no authority. | How accepted personal direction becomes durable governing state beyond informational records. | Sourced information, revisions, withdrawal, bounded chat/worker reads. |
| Accepted direction | Explicit personal decisions should not require a separate “save” request; local exceptions stay local. | Ambiguous scope, conflicting authority, and when changes should pause or redirect active work. | Transcript persistence and informational records; no complete accepted-direction mechanism. |
| Discovery | Find useful connections beyond existing links, retaining sources and allowing correction. | Retrieval signals, relevance thresholds, correction persistence and activation boundaries. | Explicitly linked context; automatic discovery beyond links is pending. |
| Consultation | Related efforts can exchange information without impersonating the person or granting new permission. | Addressing, offline delivery, retries, conflict handling and limits on repeated wakes. | Existing delivery foundations; collection-level consultation is pending. |
| Ongoing work | Responsibilities outlive a chat; reuse existing standing/task engines. | How organized context reaches checks/runs, deduplication, stop behavior and notification boundaries. | Standing items can be resolved; activation integration for this model is pending. |
| Learning | Useful knowledge and methods can improve future authorized work. | What becomes reusable, how it is corrected, and when proposed responsibilities need acceptance. | Existing memory/method foundations; this broader integration is pending. |
| Dashboard | Help a person manage, monitor and coordinate many efforts without reading a wall of text. | Layout, grouping, header continuity, navigation and interaction design. | Exploratory concepts; no finalized dashboard design in this branch. |

“Existing foundation” is not evidence that a new end-to-end behavior works.
Likewise, an unimplemented future capability is not a regression merely because
an exploratory test asks for it. False claims of having performed work remain
failures even when the requested capability is not supported.

The separate grooming records contain finer proposed/accepted distinctions.
Before implementing a pending slice, confirm its current decisions and state the
observable user outcome. Do not convert the entire remaining list directly into
parallel implementation tasks.

See [HANDOFF.md](HANDOFF.md) for branch ownership and continuation,
[BACKEND.md](BACKEND.md) for implemented guarantees, and
[CONSTRAINTS.md](CONSTRAINTS.md) for the product constraints.
