---
kind: fixed
title: A steered watched chat --once run says its answer once
pr: 2999
surface: [chat]
invalidates:
  - "A `codeaf chat --once` run that took a note through `--steer` while its turn was running printed the answer twice, and `--json` returned it doubled in `answer` (\"the answer.the answer.\"), so an AgentField step that was steered from WorkAF handed back a doubled result. The answer is now printed once; a note that arrives after the turn has ended still starts its own turn and prints that turn's reply."
---
