# Containment, references and behavior

2026-09-09. The person supplied a sketch of Folder A expanded into contents and
properties, with connections to other folders. They then asked for a critical,
minimal explanation and a clean SVG. This is an explanation of the current
backend model, not a confirmation of new product semantics.

[CONTAINMENT-AND-LINKS.svg](CONTAINMENT-AND-LINKS.svg) separates membership from
ownership, sources and explicit targets. The repeated Chat X is one record with
two memberships; its dashed identity line is explanatory drawing notation, not
a new stored chat-to-chat relation. Folder D's arrow to A denotes containment.
The drawing uses a conversation as the context source; the current source kinds
also permit tasks and artifacts. Context revisions retain title, text, source,
explicit targets and withdrawal state. A target does not prove acceptance,
authority, actual consumption or triggering.

## Critical assessment

- “Hierarchical graph” is a useful description of the view, but incomplete as a
  storage model. Collection containment is acyclic and permits multiple parents.
  Source, target, owner and origin references have their own meanings.
- A folder is an existing record, yet the UI can depict it as a collapsed node
  or an expanded boundary. No new domain object follows from a zoom level.
  Reading individual transcript messages does not require making every message
  a globally addressable knowledge node. Exact source granularity remains a
  property of the retained references, not of the viewer.
- Conversations exist without collections. An unfiled chat may have tasks and
  context connections. A query for unfiled conversations does not require a new
  “unfiled” entity or automatic creation of one folder per chat.
- Keep goals, wake conditions, permitted actions and run state with existing work
  and ongoing items. Adding independent folder goals/triggers would create a
  second place to define or contradict the same responsibility.
- Shared context is separate from the five allowed collection member kinds. It
  can be displayed alongside folder contents through explicit targets. No
  generic folder-to-folder “related to” relation is currently stored.

## The remaining behavioral question

A chat referenced by A and B is one chat, and current context selection considers
both direct collection memberships. The present shared context is information,
not accepted authority. For the wider agreed scope model, multiple applicable
memberships and conflicting guidance still need decisions; a graph layout does
not answer which direction governs the work. Ancestor inheritance and automatic
cross-folder activation must not be inferred from the drawing.

Continue grooming that consequence before adding relationship kinds or new folder
properties. Automatic retention of explicit decisions and reversible discovery
remain agreed product directions with incomplete implementation; universal memory
provenance, generic dependency links and causal execution traces are not established.
See DECISIONS.md D01–D03/D08 and the existing CURRENT-MODEL-EXPLORER.md.
