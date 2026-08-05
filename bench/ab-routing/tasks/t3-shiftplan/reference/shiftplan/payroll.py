"""Payroll. Every amount is whole cents and stays whole cents.

REFERENCE SOLUTION.
"""


def shift_pay_cents(rate_cents_per_hour, minutes):
    """rate * minutes / 60, rounded half-up to a whole cent, in integer math.

    (2x + 1) // 2 is half-up for a rational x = rate*minutes/60; multiplying
    numerator and denominator through gives (2*rate*minutes + 60) // 120 and
    never touches a float, so 2000 shifts sum exactly.
    """
    return (2 * rate_cents_per_hour * minutes + 60) // 120


def payroll(assignments, shifts, workers):
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
