# Late first token ordering regression

## Next step

Read the named provider test and the rescue and cancellation code it drives. Enumerate the winner and cancellation deciding events with file and line evidence, then force the suspect ordering in the smallest seam-level regression test.

## Established

- The reported failure is confined to `internal/provider`.
- The named test passes alone according to the task input.
- Reproduction must force an ordering with a hook, barrier, or delayed token.

## Ruled out

- Machine load, busy loops, and probability-shifting timing knobs are not valid reproduction methods.
- Changes under `internal/provider/pool` are outside this task.
