---
kind: added
title: Task models — Auto or one model, per-provider allowed models, kept pins, no silent swaps
pr: 0
surface: [chat, engine]
invalidates:
  - "A rule that left a pinned seat outside the allowed models was refused (`your worker is pinned to …, which that rule leaves out`). It is now written and the pin is kept; that seat is routed by Auto with a pin_not_allowed warning on the decision and a `gap` line on /crew until the model is allowed again or the pin removed. Pinning a seat to a disallowed model is still refused."
  - "The allowed rule was by model, or by whole provider. It now also takes `+model@provider` / `-model@provider`, one model on one provider, which is how the app's per-provider ticks are stored. An older build ignores such a word."
  - "A seat nothing allowed could sit was rescued at admission onto the last crew's model or the model you are talking to. config.RouteCrew now answers *crewroute.NoEligibleError{Seat, Mode, Rejected, Action} and starts nothing; free pools of allowed models are still a rescue, and mid-task ladder rungs (last crew, chat model) are taken only where the allowed rule admits them. errors.As(err, &config.ErrCrewUnreachable{}) still matches."
  - "An allowed saved pin whose provider was gone, off, signed out, out of credit or resting still ran (and was probed). It is now that role's NoEligibleError at admission; it is never moved to another model or provider."
  - "A direct metered connection's routes were weighed at the catalog's (OpenRouter's) price. They are now Auto candidates only when that provider's own cached listing published a price (catalog.OwnPrices; a listing seeded from ids alone knows none), and are weighed at it (crewroute.Route.Price). Unpriced direct routes are pinnable, not picked."
  - "One model for every task existed only as the --one-model launch flag. A profile row (models.tasks.mode / models.tasks.one) now makes config.RouteCrew answer a decision with Mode \"one\" and the one model in every seat, read at admission; the flag is unchanged."
  - "crewroute.Decision had no mode or warnings. It now carries Mode (auto|one; empty on old checkpoints reads auto) and Warnings []SeatWarning."
  - "The app had only settings.checks_model / planner_model for task models. New settings.models.get/list/set (internal/appwire/taskmodels.go) and an optional task_models on settings.changed, sent only when a task-models row changed; the caps stay settings.task_cap_usd / day_cap_usd."
---
