---
kind: fixed
title: The factory on this Mac opens at once, whatever old jobs and GitHub do
pr: 2101
surface: [engine]
invalidates:
  - "Was true: opening the app cleaned up old pull-request jobs before the factory on this Mac served, and one job it could not clean stopped it at every launch; the saved address then pointed at the last launch's port and every page said the factory isn't answering. Is true now: the factory serves first, the clean-up runs behind it, a job it cannot clean is kept for the next launch, and the saved address is this launch's."
  - "Was true: a read of the factory asked gh for the token, GitHub for the person and the forge for each repository's pipeline, waiting on each, so a hung gh or a dead network held the queue for its whole budget. Is true now: those reads answer from what was last heard (the person's login on the floor, gh's last token, the last pipeline file or the checkout's) within a moment, and are asked again behind the reads with backoff."
---
