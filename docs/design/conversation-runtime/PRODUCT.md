# Aforge product brief

This is a proposed product direction, not a description of shipped behavior. It derives from the owner's request on 2026-09-04; the existing branch is evidence about implementation problems, not product authority.

## Confirmed intent

- The main conversation supports ideation, decisions, high-level steering and continuity.
- Capable models should accept substantial work and carry it through with little supervision.
- Work happens in task conversations; tasks can delegate further as necessary.
- The person can enter a task or subtask conversation to inspect and steer it, then return to the main discussion.
- Tasks continue when the terminal closes; the owner explicitly confirmed this.
- Quality, completion time and cost matter together. The product should handle work beyond coding.
- This deliverable is a product/design plan; application implementation is not requested in this wave.

## Proposed product promise

Keep thinking here. Work you ask for gets done alongside the conversation. Change direction wherever you are looking, and come back to results you can use.

## Primary experience

One assistant, several simultaneous threads of work. The user commissions outcomes and makes meaningful decisions; the product handles decomposition, coordination, context transfer and integration. Agent management is optional diagnostic detail.

## Platform and operating context

The immediate surface is Aforge's terminal chat. The interaction model should also transfer to a graphical client. Closing a client detaches from ongoing work. Running while the machine is powered off requires another execution host and is not promised by local persistence.

## Proposed principles

1. Attention is the scarce resource: completion with less supervision beats visible agent activity.
2. Every material assignment has one owner responsible for the usable result.
3. Discussion, committed direction, observations and proposals remain distinguishable.
4. Steering changes the current assignment; it does not require restarting the conversation.
5. Durable work and ordinary conversation remain connected without copying every transcript everywhere.

## Open choices for implementation

Exact visual styling, keybindings, budget defaults, model allocation and deployment policies need implementation-stage decisions. They are not prerequisites to judging the product concept. Initial local-service hosting and bounded parallelism are recommendations in the design plan.
