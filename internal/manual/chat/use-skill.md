# use_skill — list or get a skill from the shelf

## What it does

Reads the shelf of active skills this project has saved after watching each one run. `use_skill` has two modes, the way `jobs` and `settings` do:

- **`list`** — shows every active skill by name with its one-line doc. No internal fields, no paths.
- **`get`** — resolves one name to its shelf path and full doc, which you then `read`.

## How to use it

```
use_skill mode=list
use_skill mode=get name=linter
```

The name is the directory name on the shelf, as `list` printed it. A name that
differs only in case still resolves — `release-notes` and `Release-Notes` are
the same skill — and the answer spells the name the shelf holds, so the name you
read back is the one that works next time.

## When a name misses

A name that matches nothing does not dead-end. The answer says how many skills
are active on the shelf and names the nearest handful, so you can re-ask with a
name the shelf actually holds. An empty shelf says so in one plain line.

## Why it exists

The distiller saves procedures it watched run as skills and promotes them to the active shelf. Until now nothing a worker held could reach one — the shelf was written to and promoted, and the only reader was a person with the CLI. This is your door onto it: mid-run discovery rather than a prompt fact.

## Where the shelf lives

Active skills are `store.Fact` entries of kind `"skill"`, pointed at a shelf directory their `Artifact` names. The shelf is curated — skills are promoted by a person through the resident.

## Tool name

The verb is `use_skill` on the belt. A belt that does not carry `propose_task` does not carry this verb either (it is gated on the same condition plus a non-nil store).