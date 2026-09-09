---
kind: changed
title: conversations retain their work, context and delivery across navigation
pr: 653
surface: [chat, engine, remote, docs]
invalidates:
  - "Opening another engine-backed conversation could end the previous one. Each conversation now owns its connection, allowing sibling chats to continue while their drafts, attention and reading state remain separate."
  - "Running and reopened conversations exposed intermediate activity as the main reading surface. Work is now compact by default, with meaningful step descriptions while running and a closed work disclosure beside the final answer after completion."
  - "A finished task with a retained branch could satisfy a request whose destination workspace was unchanged. Completion now distinguishes retained work from the delivery accepted for the original request."
  - "Earlier entries in this combined wave described temporary tab controls, shared-connection limits and a separate Working row. The entries now describe the final integrated behavior rather than those intermediate designs."
---

This is the reading guide for the combined conversation release. The linked
entries retain the detailed changes, compatibility behavior and regression scope.

## Progress and saved conversations

Recent captions occupy a three-row budget; whole captions may exceed it when
the newest description needs more room. Active text has a gentle two-second
shimmer and one still semantic icon. Between actions, the latest description
stays readable and only a separate dot animates. A running tool or known response
wait gains elapsed time after ten seconds, using its own start time. Suffixes
never move caption text; narrow displays omit the label or use the icon gutter
for the waiting dot. Screen-reader and lower-colour modes remain still.

Click or ctrl+e opens the outline, whose captions open individual tools. Default
completion and replay collapse that work; ui.work=open remains an explicit
preference. Saved captions and categories stay tied to their original tool calls.
Task pages have independent live and settled-phase disclosures. Questions,
failures, corrections and answers remain visible.

See [compact progress](653-compact-live-steps.md),
[inline waiting](653-inline-wait.md), [icons](653-step-action-icons.md),
[elapsed time](653-compact-step-time.md) and
[reopened work](653-reopened-work-fold.md).

## Navigation and context

Tabs provide new-chat, close and switcher controls, horizontal scrolling and up
to 32 remembered conversations. Closing active work offers keep running, stop
work or cancel. Stop work cancels that conversation's reply, tasks, adaptive runs
and jobs; ordinary switching preserves them. Engine-backed conversations keep
independent connections, and standalone --no-host conversations still depend on
the terminal's process lifetime. Naming begins with the first accepted message
and has bounded background retries. Task views preserve ancestry and reading
state without borrowing another conversation's controls or drafts.

/folder and bare /attach open one bounded context chooser. Browsing and previewing
do not attach anything; confirmation adds scoped folders or stages files for the
next message. The tray holds at most 24 choices. Folder scopes, applicable
instructions and removals reach subsequent model requests. The UI machine reads
the previews; pictures use terminal colour cells and PDFs provide text. A hung
filesystem read can finish late and be discarded, rather than being interrupted.

See [tabs](653-dedicated-chat-tabs.md),
[independent connections](657-every-tab-its-own-connection.md),
[early names](653-early-chat-title.md), [context](657-folder-context.md) and
[context modal](659-context-modal.md).

## Execution, delivery and recovery

Directions retain their author and effective request. Executable checks require
the explicit checks field; quoted prose alone grants no execution permission.
Full task results remain retrievable separately from compact cards. A retained
branch is not delivery to the requested workspace unless the original accepted
request explicitly allows branch or report delivery; report delivery also needs
an actual answer. Later acceptance can restore a released Git registration while
preserving the retained files. Integrating this conversation's own returned work
stays in that conversation under the existing execution limits.

Pre-send DNS and dial failures can wait for the configured endpoint to become
reachable. Recovery uses shared credential-free HEAD probes, stops on cancellation
or a shorter caller deadline, and is capped at two minutes. It says waiting for
connection; an ambiguous lost response to an accepted media job is not resubmitted.
Existing rate-limit and interrupted-stream recovery remain separate. Automatic
reasoning omits an override and preserves explicit settings; it does not disable
thinking. Routing learns usable completed workloads and keeps the person's pins.

See [execution detail](653-conversation-execution.md),
[explicit checks](653-inline-verification-contract.md),
[retained delivery](653-retained-delivery.md),
[later acceptance](653-reopen-retained-task-work.md),
[connection recovery](653-connection-recovery.md) and
[reasoning defaults](653-provider-default-reasoning.md).

Design prototypes and benchmark observations remain supporting material. They
do not establish that the proposed architectural consolidation is complete or
that one reasoning setting has a general performance advantage. This change
does not publish a release automatically.
