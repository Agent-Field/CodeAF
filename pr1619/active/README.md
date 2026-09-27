# Active working-copy restart: focused PR #1619

Revision25893534bfa9d9077e50b3d23c563f072b9a156c, binary SHA256f2446383a7a768420a12a7dc5b9ad5f0d1e16b8d50f94486c1521ceaf9e730f3. Real hosted tmux workflow on Spark; private fixture /tmp/caf-focused-active, explicit project PLANDB_DB, single-model mode configured through OpenRouter with deepseek/deepseek-v4.1-flash; every recorded request was independently checked below.

Goal: add an atomic due-list CSV export to the working maintenance CLI, preserving existing text/JSON behavior. At13:06:36 local time, about32seconds after task1 began, the task was running and its actual worker had made tool calls in trees/1. The harness sent SIGTERM only to its verified fixture frontend and resident engine. It reopened the exact same transcript, and task1 resumed in the same copy path. It then reached done with merge=merged on acceptance/active. The followup actually exported the sample and ran the regression suite.

Independent verification:36tests PASS in0.246seconds; CSV contains the expected two unfinished chores, including in-progress. The produced colleague.csv is included. Final task receipts show no running or queued ghost rows.

Model audit:42 request starts and42 ending receipts all name deepseek/deepseek-v4.1-flash. Three endings report stream errors despite HTTP200: the interrupted turn context was cancelled, one caption context was cancelled, and one worker stream reached its deadline. These are retained in model-receipts.json, not excluded to claim an error-free provider run. The task and practical outcome succeeded.

Media: workflow.cast is native terminal output recorded continuously across the restart; final-screen.png/gif are actual live terminal captures with readable geometry. This verifies one graceful active-run restart and successful continuation of this concrete task. It does not establish arbitrary crash recovery, exactly-once external side effects, or all possible interruption points.

maintenance-export-project.tar.gz contains only the finished example source, tests, input, README, ignore rules and two exported CSVs. No profile, key, database, runtime log or .git content is included.
