---
kind: fixed
title: the factory's stage_pr_base_sync tool refuses bad arguments in the same words as every other tool
pr: 2662
surface: [factory, engine]
invalidates:
  - "Was true: TestNoToolDecodesItsOwnArgumentsWithEncodingJSON failed on app/v3 itself, because stage_pr_base_sync called json.Unmarshal on its arguments, and that failure counted toward every pull request's touched-session failure cap. Is true now: the tool decodes through decodeToolArguments and the test passes on the base."
---
