---
kind: added
title: A factory keeps rules of its own, for every repository, and every step is told them
pr: 1997
surface: [engine]
invalidates:
  - "Rules lived only in each repository's `.codeaf/factory.yaml` (and a personal `~/.codeaf/factory.yaml` on one machine). A factory now keeps its OWN rules (`rules`, `ask_first`, `never`) in its store beside its runner settings, served at `GET`/`PUT /v1/rules` (PUT is operator-only and replaces all three lists; every refusal before the save carries the phase marker). They are merged into every repository's read the way the personal file is: they only ever add."
  - "A step's brief never carried the repository's `rules` sentences; only the item manager's brief did. Every chat step's brief now has a \"rules for this work\" paragraph with the repository's rules and then the factory's."
  - "A job carried no rules of its own. Queueing it now stamps the factory's rules on it (`house_rules` on the wire), so a runner on another machine follows the rules of the factory that handed it the work; a factory rules file that cannot be read refuses the start (a repository file that cannot be read does not: the start takes the defaults with the factory's rules)."
  - "A pipeline answer that the factory's own rules added to did not say which lines were whose. It now carries `house_rules` beside the merged `policy` and `start` lists."
  - "The app wire had no way to read or change a factory's rules. `factory.rules {factory}` and `factory.rules_set {factory, rules, ask_first, never}` (feature `factory.rules`, capability `rules`) now do; all three lists are required on a write, and only the operator may make one. A write may send `expect`, the rules as last read: when the factory keeps anything else it changes nothing and answers `409` (`PUT /v1/rules`, with the phase marker) or `stale` (`factory.rules_set`). The adapt word `adapt` and none compare as the same read."
---

The factory's rules (`factory.HouseRules`) are three bounded lists of one-line
sentences. Code that recognises a rule applies it whichever of the repository
or the factory said it; the rest are for the steps to follow.
