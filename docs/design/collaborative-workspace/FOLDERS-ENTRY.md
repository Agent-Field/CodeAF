# Folders-entry: dedicated logical Folders place

**Tracking:** branch-local product refinement; [no GitHub issue or PR before verification](PUBLICATION-POLICY.md). Not a fifth GitHub issue. Internal release ordinal is 5 only for `ready.json` later. Do not overwrite `releases/wave-{1,2,3,4}/`.

**Contracts:** [`CONTRACTS.md`](CONTRACTS.md) Folders-entry section is the freeze. This page is the short amendment later lanes and TRY point at.
**Plan:** [`serial-plan.md`](serial-plan.md) §2.1 (superseded).
**Journeys:** [`USER-JOURNEYS.md`](USER-JOURNEYS.md) **J36–J43** plus affected J01–J35.
**Owner:** Folders is a dedicated registered place on the Home tab bar. The old no-eighth-tab-bar-place law is withdrawn.

---

## User-visible outcome

A person reaches logical Folders from the Home tab bar without treating it as a filesystem directory and without waiting for the organizer to invent a graph.

- Tab-bar word `folders`. Fifth bar place, after `settings`. `alt+5` is Folders.
- `/folders` **enters this logical Folders place**. It is not an alias of `/folder`. `/folder` `/place` `/dir` stay physical.
- Virtual Root, never a filesystem/project mirror.
- Fresh workspace: **no generated folders** until **New folder** or **Organize existing chats**. Unfiled chats stay at Root.
- Upgrade does not delete or reorganize persisted placements.
- Visible keyboard actions, not slash-only: **New folder**, **New chat**, **Organize existing chats**. New chat may start at Root or the selected folder.
- Empty folder *list* and Root unfiled chats are **two truths**. Never draw “no folders yet”; never hide unfiled chats by pretending the tab is empty of work.
- Background organize reuses `observe_and_organize` / wsdiscover / standing tick. Honest `queued` `running` `delayed` `done` `cancel`. No second scheduler, no model on paint.
- 80-column sequential; stable selection by object id + path; composer retained.
- Home `folders` panel **stays as enter-from** (heading `folders`; enter opens the place).

This is a usable Folders *entry*, not a schema bump. Wave 1–4 interfaces stay.

---

## What the lanes must not do

- Do not alias `/folders` to `/folder`.
- Do not silently auto-populate a fresh Folders tab.
- Do not add a second job type, vector DB, or daemon.
- Do not ship a dummy production adapter that claims `done`.
- Do not overwrite wave-1…4 immutable binaries.
- Do not set `HOME` or touch `~/.codeaf`.
- Do not invent a GitHub issue/PR for this refinement.

Exact signatures, person-facing strings, and file owners: CONTRACTS.md Folders-entry.
