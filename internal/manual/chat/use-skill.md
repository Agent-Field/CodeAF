# use_skill — list or get a skill from the shelf

## What it does

Reads the shelf of active, execution-verified skills this project has saved. `use_skill` has two modes, the way `jobs` and `settings` do:

- **`list`** — shows every active skill by name with its one-line doc. No internal fields, no paths.
- **`get`** — resolves one name to its shelf path and full doc, which you then `read`.

## How to use it

```
use_skill mode=list
use_skill mode=get name=linter
```

The name is the directory name on the shelf, exactly as `list` printed it.

## Why it exists

The distiller saves verified procedures as skills and promotes them to the active shelf. Until now nothing a worker held could reach one — the shelf was written to and promoted, and the only reader was a person with the CLI. This is your door onto it: mid-run discovery rather than a prompt fact.

## Where the shelf lives

Active skills are `store.Fact` entries of kind `"skill"`, pointed at a shelf directory their `Artifact` names. The shelf is curated — skills are promoted by a person through the resident.

## Tool name

The verb is `use_skill` on the belt. A belt that does not carry `propose_task` does not carry this verb either (it is gated on the same condition plus a non-nil store).