---
kind: fixed
title: A newer worker's failed report no longer makes an older factory refuse it
pr: 2502
surface: [factory, engine]
invalidates:
  - "Since #2499 a worker whose runner has a manager wrote a failed step's one-line summary onto the step's failure, and sent it as steps[].failure.summary (and on the reshaped pipeline's failure) to every factory. A factory older than #2499 decodes worker reports strictly, so it refused the whole report: a runner updated before its factory lost every report of a failed job. The factory now advertises failure_summaries on the job it hands a worker, beside round_cost_tables, and a worker leaves the summary off unless the pulled job admits it (api.ReportAdmits, api.FromRunReport). The runner's own failure keeps its summary either way."
  - "api.FromRunReport took a bare tables bool. It now takes api.ReportAdmits, read off the pulled job with Job.ReportAdmits; passing nothing still admits nothing."
