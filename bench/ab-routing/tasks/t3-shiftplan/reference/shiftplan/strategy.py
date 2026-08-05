"""Pluggable candidate-ranking strategies. REFERENCE SOLUTION."""


class Strategy:
    """Ranks the workers qualified for a shift, best first.

    rank is given only the workers holding the shift's required skill. It must
    not filter for availability -- the assigner walks the ranked list and skips
    anyone already booked, so a strategy that dropped a busy worker would be
    making a decision that is not its to make.
    """

    def rank(self, shift, candidates):
        raise NotImplementedError


class GreedyCheapest(Strategy):
    """The behaviour shiftplan has always had: cheapest first, ties on id."""

    def rank(self, shift, candidates):
        return sorted(candidates, key=lambda w: (w.rate_cents_per_hour, w.id))
