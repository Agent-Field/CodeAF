# Historical worker compatibility fixture

These packages preserve the historical runner and worker from compatibility
fixture commit `c7d846f98cbeb911ed60ecc6e06216f7de7220da`. Only the internal
import prefix changes. The historical runner intentionally compares the cap
against current-run spending.

`serve/legacy_worker_test.go` runs this worker against the current server.
Keep the snapshot unchanged so the test continues to prove that server budget
gates protect workers built before lifetime budget enforcement.
