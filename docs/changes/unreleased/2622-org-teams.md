---
kind: added
title: An organization server keeps teams, and /v1/me names the teams you are in
pr: 2622
surface: [factory]
invalidates: []
---

The operator creates, renames and deletes teams and adds or removes members through `/v1/teams`. Only people already in the organization can be added. Every member can read every team. Removing a person from the organization also removes them from every team. A team that owns a workstream cannot be deleted. Each change is sent on the event stream as `team.changed`.
