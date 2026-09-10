# Workspace model lab

2026-09-09. A new desktop-only representation study, requested after the logical
record-shape SVG. The user explicitly wants synthetic data for grooming, with the
underlying information retained while we explore different representations.

[Editable prototype](prototypes/workspace-model-lab.html) contains 283 synthetic
records. This complements the earlier 5,000-record overview; it does not replace
that study or read a real aforge home. The browser preview is
http://127.0.0.1:8769/workspace-model-lab-preview.html .

## One model, several representations

Browse renders direct folder memberships in their stored order, with explicitly
targeted context shown separately. Connections projects the selected record's
incoming and outgoing typed references, four relationships per page, with pan,
zoom and relation filtering. Records lists the current browsing/search scope.
Changing representations preserves the selected record; the inspector identifies
when that record is outside the displayed list. Details, Relations and Data expose
the same selected identity. Data is a simplified logical shape, not a byte-for-byte
copy of Go serialization. File preview payload is separate from artifact fields.

Folders can have multiple parents. Brand is reachable from Acme and Growth.
The breadcrumb is a navigation route, not a canonical parent property. A shared
readiness report remains one artifact in Production and September launch. Unfiled
chats are a derived view; no inbox folder or new primitive is introduced.

The fixture covers every logical shape in DATA-MODEL.md:

- Folder references, shared records, shared folders and unfiled chats.
- Ordered chat journals with messages and tool interactions; session state/location.
- Chat-owned tasks, brief, acceptance, current state, own journal and output refs.
  Dependency and delegation-parent edges remain distinct, including two different
  relations between the same pair of tasks.
- Context revisions, current pointer, withdrawal, revision-specific source and
  explicit targets. Source examples include chat, task and artifact. Target examples
  cover all permitted reference kinds. A historical selection follows the record
  when inspecting its neighbours and is labeled in the footer.
- Ongoing item's original request, origin, workspace, wake, action, grant, limits,
  state and retained activity/latest-run file. All six existing wake shapes have
  examples (at, every, file, idle, probe, hold); hold has no action.
- Available and unavailable artifact paths with synthetic file previews.

These are fictional contents with current backend review-branch semantics. No
automatic memory capture, accepted-decision authority, generic exact message anchor,
per-run context-consumption receipt, context inheritance or complete all-time audit
is invented. Selecting a historic revision does not mutate saved context or assert
that the entire workspace is reconstructed as of that historical moment.

## Engineering boundary and validation

The fixture objects, reference projection, navigation state and rendering functions
are separate within one self-contained fragment. Browser interactions change only
view state. The mock uses native HTML/SVG; it introduces no runtime dependency or
database migration. A future renderer or read adapter can replace the presentation
without changing the logical objects. No actual backend integration is claimed.

A local model check exercised identity uniqueness, member-kind restrictions,
acyclic folder membership with shared parents, task ownership, all six wake kinds,
unfiled selection, revision-specific source/target projections, distinct dependency
and delegation edges, and detail/data/relation rendering for every record. Browser
inspection exercised context r2 to r1 and its source chat, ongoing origin/details,
task dependency/delegation, shared artifact, unfiled chats and representation changes.
Native browser screenshots checked the desktop layout. This is prototype validation,
not product E2E acceptance or a database performance benchmark.

Next: use the five example entry points to compare navigation and inspector
representations. Record missing information separately from confusing presentation;
no backend change follows solely from a preferred graph layout.

## Follow-up critique

[Capability and run gaps](CAPABILITY-AND-RUN-GAPS.md) distinguishes store-level
shapes from chat authoring actions, and identifies existing standing scope,
exceptions and unattended session/child-task navigation missing from this mock.
It also records the next proposed issue-to-completion grooming case. The prototype
covers the simplified logical diagram, not every serialized runtime field.
