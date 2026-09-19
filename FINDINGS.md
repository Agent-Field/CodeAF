# c293 findings

c293 makes an approved hand-off under the belt run in one copy owned by that run, cut by the existing ground ladder from the ground resolved by the hand-off's stand. The conversation checkout must remain byte-for-byte untouched while workers run and after they finish. Children in one run share that run's copy, while separate runs never share a dirty tree. Landing must be a later, explicit action and its note must say in a person's words what moved where; a read-only run lands nothing and says so.

The current belt door resolves and retains the stand for the card and receipt, but `startKnownTaskRun` receives none of it. It sets `RunSpec.Workspace` to the conversation workspace, so work and the automatic `driveBeltRun` landing happen in the person's checkout rather than in `trees/<id>` cut from the selected ground. A second hand-off currently joins any live run and therefore the same dirty workspace without checking its ground.

The shipped task road already records a node's working copy and ground-ladder facts, and its landing returns the copy's committed work to that ground. The belt path should reuse those existing sources of truth: pass the resolved stand into run creation, carve and retain one run copy under the conversation's `trees`, give that path to every worker in the run, and leave bringing it home to the explicit landing step. A second hand-off may join a live run only when it stands on the same ground and the receipt says that it joined; a hand-off on different ground must not join or share that run and must be refused clearly until it can start its own isolated run.

## Belt run working-copy isolation

This task must make an approved hand-off run in a run-owned copy beneath the conversation's `trees`, cut by the existing ground ladder from the ground selected by the resolved stand, and leave the person's checkout unchanged. Today the hand-off door resolves and retains that stand, but `startKnownTaskRun` does not receive it and instead gives `RunSpec.Workspace` the conversation workspace; consequently side-by-side hand-offs can share one dirty tree. The intended boundary is one copy per run, shared only by that run's children, with cross-run hand-off occurring only through an explicitly landed commit.

## Focused tests

The next step fixes the contract at the run door with focused tests. Each fixture sets `CODEAF_TASK_BELT` itself, creates a real committed repository, captures the person's checkout bytes, and records the `RunSpec` handed to the engine. The tests distinguish the conversation ground from an explicitly selected alternate ground, require one run-owned path beneath the conversation's `trees`, require joined children to retain that exact path, and require a different-ground hand-off not to enter the live store.

## Run copy implementation

The focused tests now fail at the intended seam: `startKnownTaskRun` has no stand argument. The implementation step will pass the resolved stand through both task doors, prepare exactly one tree with `prepareTaskTreeOn`, store its ground and workspace on the live run, hand that workspace to every child, and reject a live hand-off whose canonical ground differs. The run engine remains responsible only for driving the shared copy; finishing workers will no longer alter the conversation checkout.

## Verification

The isolated-run tests pass after the door began retaining one prepared tree per run. The live run records the canonical ground and shared workspace; a same-ground hand-off joins and its receipt says why, while a different-ground hand-off is refused before it can enter the store. The next step is verification against the existing belt, stand, ground, receipt, and run tests, followed by the required build, vet, law, formatting, and forbidden-path/history checks.
