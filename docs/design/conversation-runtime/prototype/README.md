# Conversation and tasks prototype

`conversation-tasks.html` is a self-contained interactive fragment. Open it in a
browser, or render it with the Codex inline visualization host. It has no model,
server, storage, external assets or network dependencies. Refresh starts a fresh
simulation. The optional host design control switches task navigation between a
right rail and a compact strip.

Try this sequence:

1. Type a draft in the main conversation, then open the authentication task.
2. Type a task draft and press Escape. The main draft is still there. Choose the
   authentication task in the recipient menu to see and send its draft.
3. Refresh, then use **Advance demo task**. The task needs a compatibility decision.
   Open it, stop and resume it: the unanswered decision remains.
4. Answer, then advance again. Review the branch result without merging it.
5. Back in the conversation, describe another assignment and choose **Run as task**.
   The task appears while the conversation stays open.

All changes, checks and results shown are fixtures. The walkthrough does not
interpret natural language or perform actual work. The architecture and omitted
runtime behavior are documented in `../TASK-EXPERIENCE-PROTOTYPE.md`.

Validation: headless browser interactions cover recipient routing, independent
drafts, stop/resume, retained questions, completion, task creation, escaped input
and mobile navigation. Layout checks cover 1024, 736 and 360 pixels in light and
dark appearances. Prototype JavaScript runs without browser errors. The design
detector reports no findings. This does not establish production integration.
