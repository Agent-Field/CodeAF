# PR1625: actual retry-accounting acceptance

Two bounded, natural tmux workflows on Spark; no injected failures and no third trial. Every role used OpenRouter `deepseek/deepseek-v4.1-flash`. Both useful inventory tasks generated the expected supplier rows (Bolts12/need3; Nuts7/need8), merged the two output files, and passed24 independent tests.

Live binary: clean `961c98c0d`, the reviewed focused stack `25893534b` plus production fix `1f4f3c51f`. Public PR1625 final `6105c01e8` only renames its change entry; the live result is explicitly a composed-stack result, not an isolated PR-head run.

Both trials naturally reproduced the 320-token reasoning-only first response. The fix retained each discarded first charge exactly once in the ledger: trial1 `$0.0005985`; trial2 `$0.000446472`. Both second attempts then hit their summary stream deadline. Their transport records omit cost; existing asynchronous reconciliation later records `$0.0014391` and `$0.0013488` respectively. Therefore neither trial claims full raw-transport/ledger cost parity. Known transport costs plus those reconciled charges equal the final ledger sums; raw missing cost is not treated as zero.

Final settled totals: trial1 14 transport completions/14 ledger rows, ledger `$0.011427792`; trial2 14/14, ledger `$0.010908780`. Internal day/task/seat guard behavior is verified by deterministic regression tests, not measured from the UI screenshots. Earlier interim snapshots are not final evidence.

`trial1.png` and `trial2.png` are actual final frames extracted from the finite native tmux recordings; videos are43.36s and43.68s. They show completed useful tasks, not accounting internals. Receipts contain the settled accounting proof. All owned fixture engines and tmux servers are stopped.

Capture scope: these short videos and casts were attached after task execution and show the real completed terminal state; they are not full workflow recordings. The settled receipts and real output artifacts establish execution/accounting, while the earlier focused1619 recordings show full queued/restart workflows.
