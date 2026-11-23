---
kind: fixed
title: Factory priority comes from explicit marks rather than issue type
pr: 1858
surface: [engine]
invalidates: []
---

A bug label no longer invents Medium priority. The factory shows a priority only from a saved person-set mark or an explicit priority label, with saved marks taking precedence. Legacy p0–p3 labels remain supported, and model triage does not rank jobs or change execution order.
