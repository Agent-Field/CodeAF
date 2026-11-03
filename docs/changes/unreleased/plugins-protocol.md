---
kind: added
title: Desktop plugin methods share terminal connections and capability settings
surface: [engine, app, docs]
invalidates:
  - "The desktop protocol had no plugin connection methods. Engines advertising plugins now expose the existing connect catalog, browser and key connections, capabilities and disconnect actions."
---

Seven plugin methods use the existing account and capability stores.
Connect questions emit plugin events with the same ask id used by chat and
Inbox. The desktop answers through asks.answer after connecting, without
starting a second sign-in. Provider plan-pause settings update existing source
rows through settings.set.

Featured copy is engine data. Short blurbs derive from catalog descriptions,
and connected tool-server lists retain the server's own titles and read flags.
