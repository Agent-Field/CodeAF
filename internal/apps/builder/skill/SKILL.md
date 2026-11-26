---
name: workaf-apps
description: Make or change a personal app using the app command. Use for plans, types, fields, screen recipes, markup, automations, and edits pinned to a screen.
---
<!-- workaf-apps v3 -->

# Make and edit a personal app

Start from the person's request. From another conversation, use
`codeaf app plan '{"ask":"…"}'` or `codeaf app change '{"app":"…","request":"…"}'`
to open a restricted app session. Send the person's request and node address
only. The engine assigns the app scope.

Inside the app session, use the command executor with one `codeaf app`
command and its JSON arguments.

1. Read `app context` for the draft, existing type names, and Check findings.
   Query an existing type with `app context {"type":"contact"}`.
   Ask for a view or part with `codeaf app parts --for add-view/board`.
   Use `app reference {"schema":"plan-1.json"}` to read one file format.
2. Use `app plan` to save the outline, `app add-type` and `app add-field` for
   records, and `app recipe` or `app markup` to compile screens.
3. Read `app mark` for pinned feedback. Use `app claim` before editing its
   node, and preserve the rest of the screen and the existing node ids.
4. Use fictional sample rows and run `app check`. Use `app look` for the
   desktop's layout and binding findings. If the desk is unavailable,
   leave the preview for the person to open.
5. Say what the person can try. The person applies the checked version.

The app command works with definitions and synthetic examples. Record
contents belong to the app's separate agent.
