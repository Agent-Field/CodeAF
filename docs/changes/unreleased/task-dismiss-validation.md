# Keep dismissal validation within one graph transaction

Task dismissal validates graph nodes and run records through separate producer
rules before checking live children and dependencies. Every check and the
checkpoint write still share the same graph lock; exact refusals, reversible
visibility, stopped work and unsupported external-plan behavior are unchanged.
