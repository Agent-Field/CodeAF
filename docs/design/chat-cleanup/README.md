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
| `TestCleanChat` | A live bash call folds; its details remain accessible through work and step disclosures. A real task finishes, its notification is compact, `/dismiss` hides it, and `/dismiss undo` restores it. |
| `TestCleanManagerReplay` | A deterministic historical manager journal contains a team send, a team delivery, an internal wake note, and a Markdown answer. The real TUI resumes it without exposing traffic or raw Markdown, then takes a live model follow-up. This is **replay plus live follow-up**, not a claim that a live teammate sent the fixture. |
| `TestCleanChatFailureRecovery` | A live model makes an intentionally failing bash call and a separate recovery call. Polling checks that the failure output never auto-expands; the real journal confirms that the failure actually happened. |
| `TestCleanChatSteering` | Real keyboard input is sent while a command runs. The user correction, an explicit `[update]` assistant progress message, and the final answer remain visible after two tool calls. The assistant marker is consumed by the renderer. Closing and reopening the real journal preserves all three messages. |

The narrow unit regressions additionally cover the three-row activity bound
across conversation, task, and nested views, plus interrupted intermediate
updates and Markdown rendering.

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
