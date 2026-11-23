---
kind: fixed
title: An HTTP route in a task's contract is not read as a folder
pr: 2877
surface: [chat, docs]
invalidates:
  - A task whose deliverable or acceptance named an HTTP route like /top or /links was refused before it started; a route is now admitted, because no folder along it exists on this machine.
---

`groundLint` read every slash-bearing token in a task's contract as an absolute path and refused any token outside the ground ("this task names a folder it does not stand in: /top"). A route names what the work delivers, not where it stands; the regression test `TestARouteInTheContractIsNotAFolder` pins that, and the manual says a route is not read as a folder.
