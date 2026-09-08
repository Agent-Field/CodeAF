# Canary baseline chat-door rows on dev 35c1a79e (from aforge-v2-14, #407 "BASELINE COMPLETE"), no pin
| brief | door / tests | wall s | cost $ | ttft s | note |
|---|---|---|---|---|---|
| reef-145 | ok / green | 483 | 0.0163 | 1.9 | |
| click-3740 | ok / green | 248 | 0.0157 | 2.5 | |
| attrs-1416 | wall / green | 902 | 0.0269 | | 2 turns, 37 calls; task landed at 12m35s, conversation kept working to the wall |
| tox-4031 | wall / green | 904 | 0.0376 | | 3 turns, 42 calls; pre-TMPDIR rig, turns spent on /tmp/pytest-of-santosh garbage |
Walls at 900 s are product behaviour (the chat does not stop after its task lands), not the driver's rule.
