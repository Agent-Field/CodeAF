WIP review needed: skills tranche 1 (store, attachment, shelf, use_skill)

## Summary

- The five-part skill record lives in internal/store and internal/resident: trust, cost-card and digest fields, and serving that marks consumption.
- Ordered per-node skill attachment on briefs: every briefed plan leaf now carries an ordered Skills list on plan.Node and on the journaled store.NodeBrief, composed at brief time by plan.ComposeSkills with pinned skills (names the person's proposal text says outright) first and deterministic cue/scope retrieval behind them. Function words never count as cues, one shared doc word is coincidence, three candidates at most.
- The worker's brief renders the attachment through plan.RenderSkillsBlock beside the working method; a leaf with nothing attached renders byte for byte what it rendered before.
- A windowed chat skill catalog (eight lines, scored, overflow line) and the depth-gated use_skill tool (list and get) are on the session belt wherever a store exists.
- Live wiring of NodeBrief.Skills: the chat engine reads the active shelf once per build, hands it to plan.Build frozen at both brief-journaling sites (the main plan and the remainder replan), and each dispatched leaf carries its plan node's attachment onto the task that runs it.

## Design

- https://github.com/Agent-Field/CodeAF/issues/1277#issuecomment-5753795826

## Test state

- `go build ./...` - clean.
- `go test ./internal/plan/... ./internal/orchestrate/... ./internal/session/... ./internal/store/... ./internal/resident/... -count=1 -timeout 30m` - all ok: plan 1.208s, orchestrate 0.598s, session 272.439s, store 14.302s, resident 17.970s.
- `go test ./internal/exec/ -count=1 -timeout 20m` - ok, 259.328s (added beyond the list above because internal/exec/linear.go, the render site, changed).
- New tests cover: a proposal naming a skill attaches it pinned and first on every briefed leaf (checked against the journaled store event and the graph node); a proposal naming nothing attaches nothing; retrieved candidates follow pinned entries; retrieval ignores function words and caps at three; the worker brief renders the block in composed order and renders nothing new when nothing resolves.

## Notes

- Later tranches: checks-on-attach, cost-card learning, and the forge.
- Unrelated commits: origin/santos/dev..skills/tranche-1 carries 17 commits from the feat/1089-custom-connections wave that are not this tranche - the named/multiple custom connections work (#1089) with its task and merge commits, the fzf v2 fuzzy matcher wave, a settings-row inventory, the settings search box cursor fix, docs/test chores, and two poem-chapter commits. They were on the checkout's trunk history before the skills work started.
- RenderSkillsBlock and SkillEntry moved from internal/orchestrate to internal/plan, beside ComposeSkills: the executor cannot import orchestrate (orchestrate imports the subharness, which imports the executor), and the render had no live caller.
- The change entry in docs/changes/unreleased is added once the PR number exists: `make changelog-new PR=<n> KIND=changed SLUG=skills-tranche-1`.
- origin/skills/tranche-1 already exists and points at 3bec5413f, an older lineage this branch does not carry (4 commits behind, including "session: fix ActivateSkill test callers after the skills merge"); a push will need reconciliation, which is the author's call.

—
Drafted with [CodeAF](https://agentfield.ai/github?utm_source=github&utm_medium=pull_request&utm_campaign=drafted_with) · reviewed and owned by the author