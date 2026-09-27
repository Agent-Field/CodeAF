# Clean chat terminal evidence

The acceptance suite launches `bin/codeaf` in real tmux terminals at 140 × 42.
All live text requests use Spark's OpenRouter key with
`deepseek/deepseek-v4.1-flash`. The fixture pins all text seats, uses
`--one-model`, restricts the crew's allowed model, and disables the shared model
pool. No key is stored in these artifacts. The `*-models.json` files retain only
request timestamps, IDs, phases, role tags, model names, and HTTP statuses.

## Scenarios

| Test | What it checks |
| --- | --- |
| `TestCleanChat` | A live bash call folds; its command details remain accessible through work and step disclosures. A real task finishes, its notification is compact, `/dismiss` hides it, and `/dismiss undo` restores it. |
| `TestCleanManagerReplay` | A deterministic historical manager journal contains a team send, a team delivery, an internal wake note, and a Markdown answer. The real TUI resumes it without exposing traffic or raw Markdown, then takes a live model follow-up. This is **replay plus live follow-up**, not a claim that a live teammate sent the fixture. |
| `TestCleanChatFailureRecovery` | A live model makes an intentionally failing bash call and a separate recovery call. Polling checks that the failure output never auto-expands; the real journal confirms that the failure actually happened. |
| `TestCleanChatSteering` | Real keyboard input is sent while the assistant is working. The user correction, an explicit `[update]` assistant progress message, and the final answer remain visible after two tool calls. The assistant marker is consumed by the renderer. Closing and reopening the real journal preserves all three messages. |

The narrow unit regressions additionally cover the three-row activity bound
across conversation, task, and nested views, plus interrupted intermediate
updates and Markdown rendering.

## Verified run

On 2026-09-27, `make build` from source `0b970f4bc` passed, followed by all four
real-tmux scenarios in **95.919 seconds**. The final run recorded **34 completed
request records**, all on `deepseek/deepseek-v4.1-flash`: chat/task 13, manager 5,
recovery 7, steering/resume 9. The audit also checks every start record.

[Run output](evidence/run.log) · [Machine-readable summary](evidence/run-summary.json)

## Captured screens

1. [Clean conversation](evidence/01-chat.gif) and [expanded command details](evidence/01-expanded.gif).
2. [Notification → dismissed → restored](evidence/notification-sequence.gif), a sequence of three screenshots.
3. [Manager journal replay](evidence/05-manager-replay.gif) and [live follow-up](evidence/06-manager-live-followup.gif).
4. [Failure during recovery](evidence/07-failure-recovery-active.gif) and [finished recovery](evidence/08-recovery-finished.gif).
5. [User steering](evidence/09-user-steering.gif), [assistant update while working](evidence/09-assistant-update.gif), [final conversation](evidence/10-steering-finished.gif), and [reopened journal](evidence/11-steering-resumed.gif).

![Explicit assistant update remains visible while work continues](evidence/09-assistant-update.gif)

![Task notification dismissed and restored — screenshot sequence](evidence/notification-sequence.gif)

## Reproduce

```sh
make build
CODEAF_E2E_EVIDENCE_DIR=/tmp/codeaf-chat-cleanup-evidence \
  go test -tags=e2e ./internal/e2e \
  -run '^TestClean(Chat|ManagerReplay|ChatFailureRecovery|ChatSteering)$' \
  -count=1 -v -timeout=15m
python3 docs/design/chat-cleanup/render-evidence.py \
  /tmp/codeaf-chat-cleanup-evidence docs/design/chat-cleanup/evidence
```

Every `.cast` contains the actual ANSI framebuffer read by `tmux capture-pane`.
`agg` renders those bytes into GIF screenshots; the corresponding `.txt` is the
plain terminal capture. `notification-sequence.gif` cycles three separate
screenshots (before dismissal, dismissed, restored); it is not continuous video.
