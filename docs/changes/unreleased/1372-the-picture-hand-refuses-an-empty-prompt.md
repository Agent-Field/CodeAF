---
kind: fixed
title: the picture hand refuses a prompt that is not there
pr: 1372
surface: [engine, chat]
invalidates: []
---
generate_video and generate_music each refuse an empty prompt on their own
first line. generate_image did too, until the block around the check moved into
the shared function and the check did not come with it. Nothing went red,
because nothing was asserting it, so the one paid door of the three spent money
on every call that asked for nothing and then reported the provider's complaint
about it. The guard is back at the head of GenerateImage, which is where both
the tool and the command line's picture door go through, and the request now
carries the same trimmed prompt the guard read rather than trimming it a second
time at the call.
