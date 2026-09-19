# Demand 3 findings

This step makes belt-run landing match the shipped task road: worker completion leaves the person's checkout alone, and a separate explicit landing action moves committed work through the existing `run.Land` and `session.LandRunTree` path. The landing report must name, in ordinary words, the source run copy and destination checkout. A run that made no changes moves nothing and reports that fact.

The implementation and focused tests will be committed separately after a failing test demonstrates the missing behavior.
