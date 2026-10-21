// Package cellbudget keeps the device's disk a cache of the cells' stores
// (docs/ARCHITECTURE.md 10). A cell that is finished, idle and fully sealed
// may have the working files of its sealed workspace removed, but only when
// that workspace is the harness's own, a folder inside the cell's. A person's
// project folder is never evicted. The engine's attach markers stay, plus a
// pointer naming the snapshot that holds everything else. Opening the cell
// writes the files back byte for byte.
//
// WHERE SEALED CONTENT LIVES is the [Vault] interface. Stage 0 backs it with
// the local engine store (cellstore.Engine); stage 1 backs it with a bucket,
// and the policy below does not change.
//
// THE POLICY, ONE SENTENCE PER PART:
//
//   - The budget is one setting, [BudgetEnv], in GiB; 0 means no limit.
//   - Every cell has a device-local ledger (ledger.go): last opened, measured
//     size, and the pointer when evicted. Sizes are cached; a tree is walked
//     again only when the cell's transcript moved after the last measure.
//   - Over budget, cells are evicted least recently opened first, and a cell
//     is passed over unless every [Rule] accepts it: idle, not just opened,
//     its workspace the harness's own, and sealed with nothing pending in the call log.
//   - Eviction captures the tree first, writes the pointer, then removes. A
//     crash in between leaves a pointer and a partial tree, and opening
//     restores over it.
package cellbudget
