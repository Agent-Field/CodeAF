# Agent Harness Strategy — Differentiation vs Claude Code / Codex / opencode

*Date: 2026-08-30. Status: ideation. Companion to `deep-terminal-capabilities.md`.*

## The strategic read

Claude Code, Codex, opencode all compete on **model quality and UX polish**.
None of them compete on **the runtime** — they're all "a loop that calls
tools in your shell." The runtime is where a small team can build a moat,
because it requires deep systems knowledge and it compounds: every feature
below is impossible to copy without rebuilding their core.

**The product: the first agent harness that is an operating system for
agents, not a chat loop.**

## Three flagship capabilities

### 1. "Run 10 agents on my laptop without fear"
*Combines: netns + snapshots + blackboard + scheduler + cgroups*

The indie dev's reality: one machine, paying per token, can't afford a
mistake on their only codebase. Today every harness makes parallelism scary
(agents collide, leak, hog the machine). We make it the default:

```
aforge run --parallel 5 "try five approaches to the pagination bug"
```

Five isolated workspaces (snapshots), five network sandboxes (netns), one
shared blackboard (they dedupe work and share discoveries), scheduler keeps
the laptop responsive, and at the end: five diffs, five test results, five
true costs (tokens + CPU + network), pick the winner, the other four vanish
costing ~zero disk.

**No competitor can do this.** They can do "5 terminals, 5 git worktrees,
good luck." The blackboard alone — agents that see each other's claims in
microseconds — is a category difference.

### 2. "The agent is sandboxed by the kernel, not by the prompt"
*Combines: netns + seccomp + fd-passing + syscall tracing*

Every other harness's security model is "the model was told not to do bad
things" plus a permission prompt the user learns to reflexively approve.
Ours: the kernel enforces it. The pitch to an indie dev doing client work:
*"I can run an agent on your codebase and prove it never touched the network
except npm, never read your .env, and here's the syscall trace as evidence."*

### 3. "Every run is deterministic, resumable, and cheap"
*Combines: network recording proxy + CAS + snapshots + io_uring transactions*

Indie devs feel token and time costs directly. The features: replay any past
run offline (recorded network + snapshot = hermetic replay), resume any
session after a reboot, never re-run a build whose inputs didn't change
(syscall-traced input hashing → Bazel-grade caching with zero config), and
every mutating op is atomic so a crashed run never costs you cleanup time.
"Cheaper" stops being about model pricing — it's about never paying for the
same work twice.

## Sequencing

1. **Snapshots + io_uring transactions** first — the foundation everything
   else stands on; "undo anything the agent did" is immediately demo-able.
2. **Network proxy + netns** second — the security story and the replay
   story in one, self-contained.
3. **Blackboard + scheduler** third — they only matter once parallelism
   (enabled by 1) is real, and they're the deepest engineering.

## The demo that wins opencode users

A side-by-side video. Their harness: one agent, one task, user watches a
spinner, machine fans spin up, agent half-applies a refactor and the user
spends 20 minutes cleaning up. Ours: five agents race the same task in
sandboxes, laptop stays cool, winner picked by test results, losers rolled
back in 200ms, total cost printed at the end. That's not a UX difference —
it's a capability difference.

## The wow demo (all three clusters)

You're on a laptop at a café. You say "fix the pagination bug, try three
approaches." The work executes on your desktop at home (mesh), three
approaches race with semantic-merge patches (senses/hands), the winning
approach's workflow gets compiled into your personal trace-JIT so the next
similar bug is near-free (learning), and the whole session is a git object
you can push to a teammate. Total marginal cost: a fraction of anyone else's,
on hardware you already own, getting cheaper every week.

Everyone else is building a better chat client for a model API; this is an
operating system for agent labor that lives on your machines.
