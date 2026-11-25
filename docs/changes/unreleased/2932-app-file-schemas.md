---
kind: added
title: App files get JSON Schemas, a kit v0 catalog and a validator that says what is wrong
pr: 2932
surface: [engine]
invalidates:
  - "Was true: a codeaf app's files (app.json, plan.json, screens/*.json, runs/*.yaml) existed only as examples in a design document, so nothing could say whether a definition was valid and no two lanes agreed on the field names. Is true now: internal/apps/schema embeds JSON Schemas for app/1, plan/1, screen/1 and run/1 and the kit-v0.json part catalog (20 parts, a props schema each, also served to the desk verbatim), and Validate(def, existing) reports {file, path, code, message} for every schema break and every v0 rule: refs and used types resolve against the person's existing types, a second copy of an existing type is refused, screens name only kit parts and label every interactive node, every action and binding is covered by a capability, loops have max, outbound tools wait for approval, and shell or workspace steps are refused."
---

The fixtures in internal/apps/schema/testdata (a full CRM and a hiring app that uses
its contacts) are the shared example every other fluid-apps lane builds against.
