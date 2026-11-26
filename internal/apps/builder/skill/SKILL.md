---
name: workaf-apps
description: Make or change a personal app using the app command. Use for plans, types, fields, screen recipes, markup, automations, document outputs, and edits pinned to a screen.
---
<!-- workaf-apps v4 -->

# Make and edit a personal app

From another conversation, open an app session with
`codeaf app plan '{"ask":"…"}'` or `codeaf app change '{"app":"…","request":"…"}'`
and send only the request and node address. The engine supplies the app scope.

Run one `codeaf app` command with JSON arguments at a time.

1. Read `app context` for the draft, existing type names, and Check findings.
   Query an existing type with `app context {"type":"contact"}`.
   Ask for a view or part with `codeaf app parts --for add-view/board`.
   For outputs, use `app reference {"schema":"output-template-2.json"}` and
   `app configure {"outputs":{...}}`. Schedule `render` with `inbox:true` for a query digest.
2. Use `app plan` to save the outline, `app add-type` and `app add-field` for
   records, and `app recipe` or `app markup` to compile screens.
3. Read `app mark` for pinned feedback. Use `app claim` before editing its
   node, and preserve the rest of the screen and the existing node ids.
4. Run `app check` with fictional sample rows. Use `app look` for layout and
   binding findings when the desktop is available.
5. Say what the person can try. The person applies the checked version.

The app command works with definitions and synthetic examples. Record
contents belong to the app's separate agent.
