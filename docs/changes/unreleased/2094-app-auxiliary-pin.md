---
kind: added
title: codeaf app reads and sets the small-jobs model as the auxiliary role pin
pr: 2094
surface: [engine]
invalidates:
  - "Was true: the model the small jobs beside a conversation run on (titles, route questions, captions, fold-ups; the small-work tier row) could not be read or set by an app client: settings.models.get/set carried only worker, planner and checker. Is true now: an engine whose hello says `settings.models.auxiliary` answers `roles.auxiliary` as `{pin, warning, eligible_count}` and takes `pins.auxiliary` as `{id, provider?}` or null, checked like the other pins (a model the provider serves, admitted by the allowed models), with settings.changed carrying the result. A null pin gives back the build's own small-work model."
---
