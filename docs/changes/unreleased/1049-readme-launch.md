---
kind: changed
title: The README is the launch story and the reference it used to be is docs/GUIDE.md
pr: 1049
surface: [docs]
invalidates:
  - "README.md was the reference: every flag, key, slash command, exit code and folder rule, with no pictures. That file is now docs/GUIDE.md, unchanged apart from its title. README.md is the public story for the launch: install, a real first turn, the benchmark, why a factory rather than a copilot, the seven places, tasks and review, specialist tasks, standing orders, ssh and phone, the open-model crew, the one binary, switching from other harnesses, and the roadmap."
  - "The repository had no LICENSE file. It has one, Apache 2.0, the same text agentfield ships."
  - "assets/readme/ is new and holds the hero, the diagrams, the social preview and six product screens (home, conversation, tasks-tree, question, models, phone) plus placeholder frames for the slots still to fill. The screens are real terminal captures of a seeded demo machine on open models, staged and lit in the brand package; no image model drew their text. The hero, diagrams and screens are generated from the brand package outside this repository (~/Documents/agentfield/codeaf: tools/make_visual.py, templates/readme/diagrams.html, templates/readme/stage.html and brand/prompts/ui/README.md); regenerate there, do not hand-edit the images here."
---

The README is what a stranger reads in the first minute, and the old one asked
them to read four hundred lines of reference before it said what the program
was for. The new one says it, shows it, and sends them to the guide for the
rest. Benchmark figures, the hero recording and the screenshots are marked
`TODO` in HTML comments and land in follow-up pull requests.
