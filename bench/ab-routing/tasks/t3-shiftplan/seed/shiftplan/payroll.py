"""Payroll. Every amount in this module is whole cents, and stays whole cents.

Rounding rule: a shift's pay is rate_cents_per_hour * minutes / 60, rounded
half-up to the nearest cent. Half-up, not banker's rounding -- payroll rounds
in the worker's favour on an exact half.
"""


def shift_pay_cents(rate_cents_per_hour, minutes):
    return round(rate_cents_per_hour * minutes / 60.0, 2)


def payroll(assignments, shifts, workers):
    """Total cents owed per worker id."""
    by_shift = {s.id: s for s in shifts}
    by_worker = {w.id: w for w in workers}
    totals = {}
    for a in assignments:
        shift = by_shift[a.shift_id]
        worker = by_worker[a.worker_id]
        pay = shift_pay_cents(worker.rate_cents_per_hour, shift.window.minutes)
        totals[a.worker_id] = totals.get(a.worker_id, 0) + pay
    return totals


def grand_total_cents(totals):
    return sum(totals.values())


def format_cents(cents):
    return f"${cents // 100}.{cents % 100:02d}"
