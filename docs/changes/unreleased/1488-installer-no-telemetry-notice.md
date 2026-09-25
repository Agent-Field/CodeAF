---
kind: removed
title: the installer no longer prints a telemetry notice
pr: 1488
surface: [build, docs]
invalidates:
  - "The installer printed a three-line telemetry notice after the `installed codeaf` receipt. It prints none now; the binary's full notice still arrives before the first session's events are sent."
  - "docs/TELEMETRY.md carried a second fenced block, the installer's three-line form, and test/installer-telemetry.sh compared the installer against it. That block is gone and the test asserts the installer prints no notice."
---
