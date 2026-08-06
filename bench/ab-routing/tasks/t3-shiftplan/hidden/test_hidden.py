"""The suite the submission is actually graded against. Never shown to the agent.

Every test carries a `GROUP_<n>_` name so the grader can score defect families
rather than raw assertion counts, and the grader runs **each group in its own
pytest process**. That is not tidiness. `shiftplan.assign` holds a module-level
`_CACHE`, so in one process an earlier test can answer a later one from cache
and a real defect passes. The first draft of this file did exactly that: group 3
scored 3/3 against the *buggy* package because group 3's second call hit the
cache the first call had filled. Per-group processes plus per-test id
namespacing are what make a group score mean what it says.

Groups:
  1  interval    half-open [start, end) semantics
  2  payroll     exact whole cents, half-up, no float drift
  3  defaults    no shared mutable state between calls
  4  cache       a cache key that covers every argument that changes the answer
  5  dates       bookings are scoped to a date
  6  loader      malformed input raises, never returns an empty roster
  7  strategy    the required pluggable-strategy refactor
"""

import json
import os
import tempfile

import pytest

from shiftplan import (
    Assignment, Interval, Shift, ShiftPlanError, Worker, any_overlap,
    assign_shifts, load_roster, payroll, shift_pay_cents, unstaffed,
)


def W(i, rate, *skills):
    return Worker(id=i, name=i.upper(), rate_cents_per_hour=rate,
                  skills=frozenset(skills))


def S(i, date, start, end, skill="till"):
    return Shift(id=i, date=date, window=Interval(start, end),
                 required_skill=skill)


# ---- group 1: half-open intervals ------------------------------------------

def test_GROUP_1_touching_does_not_overlap():
    assert not Interval(540, 720).overlaps(Interval(720, 900))
    assert not Interval(720, 900).overlaps(Interval(540, 720))


def test_GROUP_1_shared_minute_does_overlap():
    assert Interval(540, 721).overlaps(Interval(720, 900))
    assert Interval(720, 900).overlaps(Interval(540, 721))


def test_GROUP_1_contains_excludes_end():
    i = Interval(540, 720)
    assert i.contains(540)
    assert i.contains(719)
    assert not i.contains(720)
    assert not i.contains(539)


def test_GROUP_1_empty_interval_overlaps_nothing():
    assert not Interval(600, 600).overlaps(Interval(540, 720))
    assert not Interval(540, 720).overlaps(Interval(600, 600))


def test_GROUP_1_any_overlap_back_to_back_day():
    day = [Interval(540, 720), Interval(720, 900), Interval(900, 1080)]
    assert not any_overlap(day)
    assert any_overlap(day + [Interval(899, 1000)])


# ---- group 2: money is whole cents -----------------------------------------

def test_GROUP_2_pay_is_an_int():
    assert isinstance(shift_pay_cents(1795, 50), int)
    assert isinstance(shift_pay_cents(2000, 60), int)


def test_GROUP_2_half_up_rounding():
    # 1795 c/h * 50 min / 60 = 1495.8333... -> 1496
    assert shift_pay_cents(1795, 50) == 1496
    # 1200 c/h * 1 min / 60 = 20 exactly
    assert shift_pay_cents(1200, 1) == 20
    # exact halves go up, not to even: 1.5 -> 2 and 2.5 -> 3
    assert shift_pay_cents(90, 1) == 2
    assert shift_pay_cents(150, 1) == 3
    assert shift_pay_cents(2000, 60) == 2000


def test_GROUP_2_no_float_drift_over_many_shifts():
    # payroll only, no assignment: this group is about arithmetic, so the
    # assignment list is built directly rather than earned through assign_shifts.
    workers = [W("g2w", 1795, "till")]
    shifts = [S(f"g2s{n}", "2026-03-04", 540, 590) for n in range(2000)]
    assignments = [Assignment(shift_id=s.id, worker_id="g2w") for s in shifts]
    totals = payroll(assignments, shifts, workers)
    assert isinstance(totals["g2w"], int)
    assert totals["g2w"] == 1496 * 2000


# ---- group 3: no shared mutable default ------------------------------------

def test_GROUP_3_repeated_default_calls_all_staff():
    # Same worker, a fresh shift each time, `blocked` never passed. A mutable
    # default that the body appends to makes call 2 onwards see call 1's
    # bookings and leave the shift unstaffed.
    workers = [W("g3a", 1800, "till")]
    outs = [assign_shifts([S(f"g3a{n}", "2026-03-04", 540, 720)], workers)
            for n in range(3)]
    assert [len(o) for o in outs] == [1, 1, 1]
    assert all(o[0].worker_id == "g3a" for o in outs)


def test_GROUP_3_caller_list_is_not_mutated():
    workers = [W("g3b", 1800, "till")]
    shifts = [S("g3b1", "2026-03-04", 540, 720)]
    blocked = []
    assign_shifts(shifts, workers, blocked=blocked)
    assert blocked == []


def test_GROUP_3_default_and_explicit_empty_agree():
    workers = [W("g3c", 1800, "till")]
    implicit = assign_shifts([S("g3c1", "2026-03-04", 540, 720)], workers)
    explicit = assign_shifts([S("g3c2", "2026-03-04", 540, 720)], workers,
                             blocked=[])
    assert [x.worker_id for x in implicit] == ["g3c"]
    assert [x.worker_id for x in explicit] == ["g3c"]


# ---- group 4: the cache key covers everything that changes the answer ------

def test_GROUP_4_blocked_changes_the_answer():
    workers = [W("g4a", 1800, "till"), W("g4b", 2500, "till")]
    shifts = [S("g4s1", "2026-03-04", 540, 720)]
    free = assign_shifts(shifts, workers, blocked=[])
    assert [x.worker_id for x in free] == ["g4a"]
    busy = assign_shifts(shifts, workers, blocked=[("g4a", Interval(500, 800))])
    assert [x.worker_id for x in busy] == ["g4b"]


def test_GROUP_4_equal_inputs_still_give_equal_results():
    workers = [W("g4c", 1800, "till"), W("g4d", 2500, "till")]
    shifts = [S("g4s2", "2026-03-04", 540, 720)]
    assert (assign_shifts(shifts, workers, blocked=[])
            == assign_shifts(shifts, workers, blocked=[]))


def test_GROUP_4_returned_list_cannot_be_corrupted_by_the_caller():
    workers = [W("g4e", 1800, "till")]
    shifts = [S("g4s3", "2026-03-04", 540, 720)]
    out = assign_shifts(shifts, workers, blocked=[])
    out.append("junk")
    again = assign_shifts(shifts, workers, blocked=[])
    assert "junk" not in again


# ---- group 5: bookings are per date ----------------------------------------

def test_GROUP_5_same_window_next_day_is_free():
    workers = [W("g5a", 1800, "till")]
    shifts = [S("g5s1", "2026-03-04", 540, 1020),
              S("g5s2", "2026-03-05", 540, 1020)]
    out = assign_shifts(shifts, workers, blocked=[])
    assert sorted((x.shift_id, x.worker_id) for x in out) == \
        [("g5s1", "g5a"), ("g5s2", "g5a")]
    assert unstaffed(shifts, out) == []


def test_GROUP_5_same_day_overlap_is_still_blocked():
    workers = [W("g5b", 1800, "till")]
    shifts = [S("g5s3", "2026-03-04", 540, 1020),
              S("g5s4", "2026-03-04", 600, 900)]
    out = assign_shifts(shifts, workers, blocked=[])
    assert len(out) == 1
    assert unstaffed(shifts, out) == ["g5s4"]


def test_GROUP_5_two_disjoint_same_day_shifts_both_staff():
    # Deliberately *not* back to back. Touching windows would make this test
    # depend on group 1's half-open semantics, and a group that fails for a
    # defect belonging to another group cannot be scored as its own family --
    # selfcheck.py catches exactly that by reverting one module at a time.
    workers = [W("g5c", 1800, "till")]
    shifts = [S("g5s5", "2026-03-04", 540, 720),
              S("g5s6", "2026-03-04", 780, 900)]
    out = assign_shifts(shifts, workers, blocked=[])
    assert len(out) == 2


def test_GROUP_5_standing_block_applies_on_every_date():
    workers = [W("g5d", 1800, "till"), W("g5e", 2500, "till")]
    shifts = [S("g5s7", "2026-03-04", 540, 720),
              S("g5s8", "2026-03-09", 540, 720)]
    out = assign_shifts(shifts, workers,
                        blocked=[("g5d", Interval(500, 800))])
    assert {x.worker_id for x in out} == {"g5e"}
    assert len(out) == 2


# ---- group 6: a malformed roster is an error, not an empty roster ----------

def test_GROUP_6_bad_json_raises():
    with tempfile.TemporaryDirectory() as d:
        p = os.path.join(d, "roster.json")
        with open(p, "w") as f:
            f.write("{not json")
        with pytest.raises(ShiftPlanError):
            load_roster(p)


def test_GROUP_6_missing_key_raises():
    with tempfile.TemporaryDirectory() as d:
        p = os.path.join(d, "roster.json")
        with open(p, "w") as f:
            json.dump({"workers": []}, f)
        with pytest.raises(ShiftPlanError):
            load_roster(p)


def test_GROUP_6_missing_file_raises():
    with pytest.raises(ShiftPlanError):
        load_roster("/nonexistent/roster/path.json")


def test_GROUP_6_good_file_still_loads():
    with tempfile.TemporaryDirectory() as d:
        p = os.path.join(d, "roster.json")
        with open(p, "w") as f:
            json.dump({
                "workers": [{"id": "g6a", "name": "A",
                             "rate_cents_per_hour": 1800, "skills": ["till"]}],
                "shifts": [{"id": "g6s1", "date": "2026-03-04",
                            "start_minute": 540, "end_minute": 720,
                            "required_skill": "till"}]}, f)
        shifts, workers = load_roster(p)
        assert len(shifts) == 1 and len(workers) == 1
        assert workers[0].id == "g6a"


# ---- group 7: the pluggable-strategy refactor ------------------------------

def test_GROUP_7_strategy_module_exists():
    from shiftplan.strategy import GreedyCheapest, Strategy  # noqa: F401


def test_GROUP_7_default_strategy_is_greedy_cheapest():
    from shiftplan.strategy import GreedyCheapest
    workers = [W("g7a", 2200, "till"), W("g7b", 1800, "till")]
    default = assign_shifts([S("g7s1", "2026-03-04", 540, 720)], workers,
                            blocked=[])
    explicit = assign_shifts([S("g7s2", "2026-03-04", 540, 720)], workers,
                             blocked=[], strategy=GreedyCheapest())
    assert [x.worker_id for x in default] == ["g7b"]
    assert [x.worker_id for x in explicit] == ["g7b"]


def test_GROUP_7_a_stub_strategy_is_honoured():
    from shiftplan.strategy import Strategy

    class MostExpensive(Strategy):
        def rank(self, shift, candidates):
            return sorted(candidates,
                          key=lambda w: (-w.rate_cents_per_hour, w.id))

    workers = [W("g7c", 2200, "till"), W("g7d", 1800, "till")]
    out = assign_shifts([S("g7s3", "2026-03-04", 540, 720)], workers,
                        blocked=[], strategy=MostExpensive())
    assert [x.worker_id for x in out] == ["g7c"]


def test_GROUP_7_strategy_only_sees_qualified_candidates():
    from shiftplan.strategy import Strategy

    seen = []

    class Recording(Strategy):
        def rank(self, shift, candidates):
            seen.append({w.id for w in candidates})
            return sorted(candidates, key=lambda w: w.id)

    workers = [W("g7e", 1800, "till"), W("g7f", 1800, "forklift")]
    assign_shifts([S("g7s4", "2026-03-04", 540, 720, "till")], workers,
                  blocked=[], strategy=Recording())
    assert seen == [{"g7e"}]
