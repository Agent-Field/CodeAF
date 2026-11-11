---
kind: added
title: codeaf app holds keys in memory, keeps setup, and asks before a budget or day cap stops it
pr: 0
surface: [engine]
invalidates:
  - "codeaf app read keys only from the profile and the shell. The app now hands keys over with keys.set and the process holds them in memory, ahead of every other source; while they are installed no profile write puts a key on disk, and keys.remove disconnects a provider for the launch without the profile's key taking its place."
  - "settings.get's day_cap_usd was the terminal's daily limit. It is now the app's own day cap, $20.00 unless setup or settings.set chose another, kept in app/state.json under codeaf's home; the terminal's daily limit and its default are unchanged."
  - "An agent created with no workspace always worked in the home folder. It now works in settings' work_folder, which defaults to the home folder."
  - "An agent's budget_usd lasted only while its conversation stayed open. The app now keeps it and binds it again at every open and every turn."
---

New methods: `providers.list`, `keys.check` (one request to the provider, ten
seconds, the provider's own status and words), `keys.set`, `keys.remove`,
`providers.sign_in` (OpenRouter's browser sign-in; its key arrives once on
`provider.changed`), `setup.get` / `setup.set`, `settings.set` with
`settings.changed`, and `caps.raise`. An agent near or at its budget, or a
message sent at the day cap, opens an approval ask of subkind `budget`; a raise
runs again the turn the limit refused.
